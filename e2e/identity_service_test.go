//go:build e2e

// The identity service, running for real against a real database.
//
// The tests in identity_test.go prove the schema and its policies. These prove
// the service on top of them: that a password buys a session, that the session
// resolves to a tenant, and that signing out ends it on the next request rather
// than at expiry — which is the whole reason sessions are rows.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ppusapati/gavya/libs/integrity/credential"
)

// identityAt starts the identity service against the isolated database, seeded
// with people who have real password hashes.
//
// It connects as gavya_app, not as the owner, because connecting as the owner is
// what would make every policy under it decorative — and this is the service
// whose whole job is to be trusted.
func identityAt(t *testing.T) (base string, owner *pgx.Conn) {
	t.Helper()
	owner, _ = isolated(t)
	seedIdentity(t, owner)
	seedPasswords(t, owner)

	root := workspaceRoot(t)
	bin := filepath.Join(t.TempDir(), "identity-service")
	build := exec.Command("go", "build", "-o", bin, "./cmd/server")
	build.Dir = filepath.Join(root, "services", "identity-service")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build identity-service: %v\n%s", err, out)
	}

	addr := fmt.Sprintf("127.0.0.1:%d", freePort(t))
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(),
		"SERVER_ADDR="+addr,
		"DATABASE_URL="+asRole(dsn(t, "e2e_isolation"), "gavya_app"),
	)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start identity-service: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	// Registered after the kill so it runs before it: cleanups are last in,
	// first out. identity-service is the one service the shared harness does not
	// start, so it is the one whose counters have to be read while it is alive —
	// TestMain's sweep would find nothing here. See coverage_test.go.
	t.Cleanup(func() { recordServedProcedures("http://" + addr) })

	base = "http://" + addr
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
			_ = c.Close()
			resp, err := http.Get(base + "/healthz")
			if err == nil {
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return base, owner
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("identity-service did not become ready on %s", addr)
	return "", nil
}

const (
	alphaPassword = "an alpha password"
	bothPassword  = "a shared password"
)

// seedPasswords replaces the placeholder hashes with real ones, at a cost that
// keeps the suite quick without changing which code paths run.
func seedPasswords(t *testing.T, owner *pgx.Conn) {
	t.Helper()
	cheap := credential.Params{Memory: 8 * 1024, Time: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	for _, u := range []struct{ id, password string }{
		{"US_ALPHA_ONLY_000000000000", alphaPassword},
		{"US_BOTH_TENANTS_0000000000", bothPassword},
	} {
		h, err := credential.HashWith(u.password, cheap)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := owner.Exec(context.Background(),
			"UPDATE users SET password_hash = $2 WHERE id = $1", u.id, h); err != nil {
			t.Fatal(err)
		}
	}
}

// call posts one Connect unary request and returns the status and the decoded body.
func call(t *testing.T, base, method string, in any) (int, map[string]any) {
	t.Helper()
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(base+"/gavya.identity.v1.IdentityService/"+method,
		"application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestSigningInOverTheWireGivesASessionForTheRightTenant(t *testing.T) {
	base, _ := identityAt(t)

	code, body := call(t, base, "SignIn", map[string]string{
		"email": "alpha.only@example.test", "password": alphaPassword})
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, body)
	}
	if body["tenant_id"] != alpha {
		t.Errorf("tenant = %v, want alpha", body["tenant_id"])
	}
	if body["session_id"] == "" || body["session_id"] == nil {
		t.Error("no session id")
	}
	if body["role_name"] != "manager" {
		t.Errorf("role = %v", body["role_name"])
	}
}

func TestAWrongPasswordOverTheWireIsRefused(t *testing.T) {
	base, _ := identityAt(t)

	code, body := call(t, base, "SignIn", map[string]string{
		"email": "alpha.only@example.test", "password": "not the password"})
	if code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401: %v", code, body)
	}
	// And the message must not say which half was wrong.
	msg, _ := body["message"].(string)
	for _, leak := range []string{"no such", "does not exist", "wrong password", "suspended"} {
		if strings.Contains(strings.ToLower(msg), leak) {
			t.Errorf("the refusal says %q, which distinguishes causes: %q", leak, msg)
		}
	}
}

// An unknown address and a wrong password must be the same reply, byte for byte.
func TestAnUnknownAddressAndAWrongPasswordGiveTheSameReply(t *testing.T) {
	base, _ := identityAt(t)

	codeA, bodyA := call(t, base, "SignIn", map[string]string{
		"email": "alpha.only@example.test", "password": "wrong"})
	codeB, bodyB := call(t, base, "SignIn", map[string]string{
		"email": "nobody@example.test", "password": "wrong"})

	if codeA != codeB {
		t.Errorf("statuses %d and %d differ", codeA, codeB)
	}
	if fmt.Sprint(bodyA) != fmt.Sprint(bodyB) {
		t.Errorf("replies differ:\n  known:   %v\n  unknown: %v", bodyA, bodyB)
	}
}

// A person in two tenants who did not say which is refused, and told the
// options — because picking one would be a silent decision about whose data they
// are about to edit.
func TestSomebodyInTwoTenantsIsAskedWhichOverTheWire(t *testing.T) {
	base, _ := identityAt(t)

	code, body := call(t, base, "SignIn", map[string]string{
		"email": "both@example.test", "password": bothPassword})
	if code == http.StatusOK {
		t.Fatalf("signed in without saying which tenant: %v", body)
	}
	msg, _ := body["message"].(string)
	if !strings.Contains(msg, alpha) || !strings.Contains(msg, beta) {
		t.Errorf("the refusal does not name both tenants: %q", msg)
	}

	// Naming one works, and gives that one.
	code, body = call(t, base, "SignIn", map[string]string{
		"email": "both@example.test", "password": bothPassword, "tenant_id": beta})
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, body)
	}
	if body["tenant_id"] != beta {
		t.Errorf("tenant = %v, want beta", body["tenant_id"])
	}
}

// Asking for a tenant you do not belong to is refused even with the right
// password — which is the case the whole membership model exists to decide.
func TestAskingForSomebodyElsesTenantIsRefused(t *testing.T) {
	base, _ := identityAt(t)

	code, body := call(t, base, "SignIn", map[string]string{
		"email": "alpha.only@example.test", "password": alphaPassword, "tenant_id": beta})
	if code == http.StatusOK {
		t.Fatalf("signed in to a tenant they do not belong to: %v", body)
	}
}

// The step that turns the tenant from something the caller asserted into
// something that was checked.
func TestASessionVerifiesToItsTenant(t *testing.T) {
	base, _ := identityAt(t)

	_, in := call(t, base, "SignIn", map[string]string{
		"email": "alpha.only@example.test", "password": alphaPassword})
	session, _ := in["session_id"].(string)

	code, body := call(t, base, "VerifySession", map[string]string{"session_id": session})
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, body)
	}
	if body["tenant_id"] != alpha {
		t.Errorf("verified to tenant %v, want alpha", body["tenant_id"])
	}
	if body["user_id"] != "US_ALPHA_ONLY_000000000000" {
		t.Errorf("verified as %v", body["user_id"])
	}
}

// The reason sessions are rows rather than only signatures. A signed token
// checked only by its signature works until it expires, which makes dismissal, a
// stolen credential and a lost laptop unhandleable for the length of the
// lifetime.
func TestSigningOutEndsTheSessionOnTheNextRequest(t *testing.T) {
	base, _ := identityAt(t)

	_, in := call(t, base, "SignIn", map[string]string{
		"email": "alpha.only@example.test", "password": alphaPassword})
	session, _ := in["session_id"].(string)

	if code, _ := call(t, base, "VerifySession", map[string]string{"session_id": session}); code != http.StatusOK {
		t.Fatalf("the session did not verify before signing out")
	}
	if code, body := call(t, base, "SignOut", map[string]string{"session_id": session}); code != http.StatusOK {
		t.Fatalf("sign out: %d %v", code, body)
	}
	code, body := call(t, base, "VerifySession", map[string]string{"session_id": session})
	if code != http.StatusUnauthorized {
		t.Fatalf("a session verified after being signed out: %d %v", code, body)
	}
}

// A client whose reply was lost retries. Told it failed, it retries again.
func TestSigningOutTwiceOverTheWireIsNotAnError(t *testing.T) {
	base, _ := identityAt(t)

	_, in := call(t, base, "SignIn", map[string]string{
		"email": "alpha.only@example.test", "password": alphaPassword})
	session, _ := in["session_id"].(string)

	for i := 0; i < 2; i++ {
		if code, body := call(t, base, "SignOut", map[string]string{"session_id": session}); code != http.StatusOK {
			t.Fatalf("sign-out %d gave %d: %v", i+1, code, body)
		}
	}
}

// Expired, revoked and never-issued are one answer to whoever is asking.
func TestAnInventedSessionIdIsRefusedLikeAnyOther(t *testing.T) {
	base, _ := identityAt(t)

	code, body := call(t, base, "VerifySession", map[string]string{
		"session_id": "SE_NEVER_ISSUED_0000000000"})
	if code != http.StatusUnauthorized {
		t.Fatalf("status %d: %v", code, body)
	}
}

// What a dismissal needs, and the thing a signature alone cannot do.
func TestSigningOutEverywhereEndsEverySession(t *testing.T) {
	base, _ := identityAt(t)

	var sessions []string
	for i := 0; i < 3; i++ {
		_, in := call(t, base, "SignIn", map[string]string{
			"email": "alpha.only@example.test", "password": alphaPassword})
		s, _ := in["session_id"].(string)
		if s == "" {
			t.Fatalf("sign-in %d gave no session: %v", i, in)
		}
		sessions = append(sessions, s)
	}

	code, body := call(t, base, "SignOutEverywhere", map[string]string{
		"tenant_id": alpha, "user_id": "US_ALPHA_ONLY_000000000000", "reason": "left the company"})
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, body)
	}
	if n, _ := body["sessions_ended"].(float64); int(n) != 3 {
		t.Errorf("ended %v sessions, want 3", body["sessions_ended"])
	}

	for i, s := range sessions {
		if code, _ := call(t, base, "VerifySession", map[string]string{"session_id": s}); code != http.StatusUnauthorized {
			t.Errorf("session %d still verifies after everything was signed out", i)
		}
	}
}

// The log is what makes a credential-stuffing run visible, and the attempts
// worth keeping are the ones against addresses that do not exist — which have no
// tenant and would be refused by the policies if the service wrote them directly.
func TestFailuresAgainstUnknownAddressesReachTheLog(t *testing.T) {
	base, owner := identityAt(t)

	for _, addr := range []string{"nobody1@example.test", "nobody2@example.test", "nobody3@example.test"} {
		call(t, base, "SignIn", map[string]string{"email": addr, "password": "guess"})
	}

	var n int
	if err := owner.QueryRow(context.Background(),
		`SELECT count(*) FROM authentication_attempts
		 WHERE NOT succeeded AND tenant_id IS NULL AND email_normalised LIKE 'nobody%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("%d attempts against unknown addresses reached the log, want 3", n)
	}
}

// Repeated failures have to become slower, or a stolen table is not needed at
// all — the login endpoint is enough.
func TestRepeatedFailuresEventuallyLockTheAccountOverTheWire(t *testing.T) {
	base, owner := identityAt(t)

	for i := 0; i < 5; i++ {
		call(t, base, "SignIn", map[string]string{
			"email": "alpha.only@example.test", "password": "wrong"})
	}

	var locked *time.Time
	var failed int
	if err := owner.QueryRow(context.Background(),
		"SELECT locked_until, failed_attempts FROM users WHERE id = 'US_ALPHA_ONLY_000000000000'").
		Scan(&locked, &failed); err != nil {
		t.Fatal(err)
	}
	if failed < 5 {
		t.Errorf("failed attempts = %d after five failures", failed)
	}
	if locked == nil {
		t.Fatal("the account was not locked after five failures")
	}

	// And the right password does not work while the lock stands, or an
	// attacker could use the reply to confirm a guess.
	if code, _ := call(t, base, "SignIn", map[string]string{
		"email": "alpha.only@example.test", "password": alphaPassword}); code == http.StatusOK {
		t.Error("a locked account was signed in with the right password")
	}
}
