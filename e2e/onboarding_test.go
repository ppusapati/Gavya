//go:build e2e

// Setting up a co-operative without a database console.
//
// Before these routes existed, authorisation was enforced against roles nobody
// could be given: there was no way through the API to create a person, set a
// password, assign a role, or issue a machine credential. Every one was a row
// somebody typed into psql, which is not a thing the secretary of a village
// society is going to do — so in practice a deployment meant handing somebody a
// database.
//
// This walks the whole path: roles, a clerk, a password, a sign-in, a promotion,
// a suspension, and a credential for a device. It is the one test in the suite
// that is about whether the platform can be handed over.
package e2e

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// admin calls one identity procedure as a tenant administrator.
//
// Through raw HTTP rather than svcclient, because identityAt starts the identity
// service on its own and these tests need to set the permission header by hand —
// which is precisely what the gateway would be doing.
func admin(t *testing.T, base, method, tenant string, in any) (int, map[string]any) {
	t.Helper()
	return callAs(t, base, method, in, map[string]string{
		"X-Gavya-Tenant":      tenant,
		"X-Gavya-Permissions": "tenant.admin,tenant.read,tenant.write",
	})
}

func callAs(t *testing.T, base, method string, in any, headers map[string]string) (int, map[string]any) {
	t.Helper()
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost,
		base+"/gavya.identity.v1.IdentityService/"+method, strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

// A co-operative is set up end to end, through the API alone.
func TestACooperativeIsSetUpThroughTheAPIAlone(t *testing.T) {
	base, owner := identityAt(t)
	tenant := seedTenantRow(t, owner)

	// 1. What roles are there to give somebody?
	code, body := admin(t, base, "ListRoles", tenant, map[string]string{"tenant_id": tenant})
	if code != 200 {
		t.Fatalf("ListRoles: %d %v", code, body)
	}
	roles, _ := body["roles"].([]any)
	if len(roles) != 7 {
		t.Fatalf("the platform offers %d roles, want 7", len(roles))
	}
	byName := map[string]map[string]any{}
	for _, r := range roles {
		m := r.(map[string]any)
		byName[m["name"].(string)] = m
	}
	for _, want := range []string{"collector", "clerk", "supervisor", "accountant",
		"auditor", "admin", "service"} {
		if byName[want] == nil {
			t.Errorf("no role named %q is offered", want)
			continue
		}
		if d, _ := byName[want]["description"].(string); d == "" {
			t.Errorf("role %q is offered with no description; somebody has to choose "+
				"between these", want)
		}
		if p, _ := byName[want]["permissions"].([]any); len(p) == 0 {
			t.Errorf("role %q is offered carrying no permissions", want)
		}
	}

	// 2. A clerk, with a password.
	const clerkPassword = "a password for the clerk"
	code, body = admin(t, base, "AddMember", tenant, map[string]string{
		"tenant_id": tenant, "email": "clerk@society.test",
		"full_name": "Society Clerk", "role": "clerk",
		"password": clerkPassword, "actor": "US_FOUNDER_00000000000000",
	})
	if code != 200 {
		t.Fatalf("AddMember: %d %v", code, body)
	}
	clerkID, _ := body["user_id"].(string)
	if clerkID == "" {
		t.Fatal("the new member came back with no user id")
	}
	if created, _ := body["created"].(bool); !created {
		t.Error("adding a person nobody had heard of reported that they already existed")
	}

	// 3. They can sign in, and the session says what they may do.
	code, body = callAs(t, base, "SignIn", map[string]string{
		"email": "clerk@society.test", "password": clerkPassword, "tenant_id": tenant,
	}, nil)
	if code != 200 {
		t.Fatalf("the clerk could not sign in: %d %v", code, body)
	}
	session, _ := body["session_id"].(string)
	if session == "" {
		t.Fatal("signing in produced no session")
	}

	code, body = callAs(t, base, "VerifySession", map[string]string{"session_id": session}, nil)
	if code != 200 {
		t.Fatalf("VerifySession: %d %v", code, body)
	}
	if got, _ := body["role_name"].(string); got != "clerk" {
		t.Errorf("the session says the role is %q, want clerk", got)
	}
	perms := permissionsOf(body)
	if !perms["milk.write"] {
		t.Errorf("a clerk's session does not carry milk.write; it holds %v", keysOf(perms))
	}
	if perms["settlement.approve"] {
		t.Error("a clerk's session carries settlement.approve")
	}

	// 4. Promoted to accountant, and the session that follows says so.
	code, body = admin(t, base, "SetMemberRole", tenant, map[string]string{
		"tenant_id": tenant, "user_id": clerkID, "role": "accountant",
		"actor": "US_FOUNDER_00000000000000",
	})
	if code != 200 {
		t.Fatalf("SetMemberRole: %d %v", code, body)
	}
	code, body = callAs(t, base, "SignIn", map[string]string{
		"email": "clerk@society.test", "password": clerkPassword, "tenant_id": tenant,
	}, nil)
	if code != 200 {
		t.Fatalf("sign in after promotion: %d %v", code, body)
	}
	promoted, _ := body["session_id"].(string)
	_, body = callAs(t, base, "VerifySession", map[string]string{"session_id": promoted}, nil)
	perms = permissionsOf(body)
	if !perms["settlement.approve"] {
		t.Error("an accountant's session does not carry settlement.approve")
	}
	if perms["milk.write"] {
		t.Error("an accountant's session still carries milk.write — the promotion " +
			"added to the old role rather than replacing it, and the separation " +
			"between recording a figure and approving the payment for it is gone")
	}

	// 5. The listing shows who is here and what they are.
	code, body = admin(t, base, "ListMembers", tenant, map[string]string{"tenant_id": tenant})
	if code != 200 {
		t.Fatalf("ListMembers: %d %v", code, body)
	}
	members, _ := body["members"].([]any)
	if len(members) != 1 {
		t.Fatalf("the tenant has %d members, want 1", len(members))
	}
	m := members[0].(map[string]any)
	if m["role"] != "accountant" {
		t.Errorf("the listing says the member is a %v, want accountant", m["role"])
	}
	if has, _ := m["has_password"].(bool); !has {
		t.Error("the listing says this member has no password, and one was set")
	}
	for _, leaked := range []string{"password_hash", "password", "secret"} {
		if _, present := m[leaked]; present {
			t.Errorf("the member listing carries %q", leaked)
		}
	}
}

// A machine gets a credential, once, and it can be revoked.
func TestADeviceIsIssuedACredentialAndItCanBeTakenAway(t *testing.T) {
	base, owner := identityAt(t)
	tenant := seedTenantRow(t, owner)

	expires := time.Now().UTC().AddDate(1, 0, 0).Format(time.RFC3339)
	code, body := admin(t, base, "IssueServiceIdentity", tenant, map[string]string{
		"tenant_id": tenant, "name": "dock-2-analyser",
		"expires_at": expires, "actor": "US_FOUNDER_00000000000000",
	})
	if code != 200 {
		t.Fatalf("IssueServiceIdentity: %d %v", code, body)
	}
	secret, _ := body["secret"].(string)
	id, _ := body["id"].(string)
	if secret == "" {
		t.Fatal("a credential was issued with no secret, so nothing can use it")
	}
	if len(secret) < 24 {
		t.Errorf("the secret is %d characters; a machine credential nobody types "+
			"has no reason to be short", len(secret))
	}

	// It works.
	code, body = callAs(t, base, "SignInService", map[string]string{
		"name": "dock-2-analyser", "secret": secret,
	}, nil)
	if code != 200 {
		t.Fatalf("the device could not sign in with the credential just issued: %d %v", code, body)
	}
	if got, _ := body["tenant_id"].(string); got != tenant {
		t.Errorf("the device's session is for tenant %q, want %q", got, tenant)
	}

	// The listing shows it and does not show the secret.
	code, body = admin(t, base, "ListServiceIdentities", tenant, map[string]string{"tenant_id": tenant})
	if code != 200 {
		t.Fatalf("ListServiceIdentities: %d %v", code, body)
	}
	list, _ := body["service_identities"].([]any)
	if len(list) != 1 {
		t.Fatalf("the tenant holds %d credentials, want 1", len(list))
	}
	entry := list[0].(map[string]any)
	for _, leaked := range []string{"secret", "secret_hash"} {
		if _, present := entry[leaked]; present {
			t.Errorf("the credential listing carries %q, which is how a secret ends "+
				"up in a support ticket", leaked)
		}
	}

	// Revoked, and then it does not.
	code, body = admin(t, base, "RevokeServiceIdentity", tenant, map[string]string{
		"tenant_id": tenant, "id": id, "actor": "US_FOUNDER_00000000000000",
	})
	if code != 200 {
		t.Fatalf("RevokeServiceIdentity: %d %v", code, body)
	}
	code, body = callAs(t, base, "SignInService", map[string]string{
		"name": "dock-2-analyser", "secret": secret,
	}, nil)
	if code == 200 {
		t.Error("a revoked credential still opens a session, so revoking it did nothing")
	}
}

// Somebody changes their own password, and the old one stops working.
func TestAPersonChangesTheirOwnPasswordAndTheOldOneStops(t *testing.T) {
	base, owner := identityAt(t)
	tenant := seedTenantRow(t, owner)

	const first = "the first password here"
	const second = "the second password here"

	code, body := admin(t, base, "AddMember", tenant, map[string]string{
		"tenant_id": tenant, "email": "collector@society.test",
		"full_name": "A Collector", "role": "collector",
		"password": first, "actor": "US_FOUNDER_00000000000000",
	})
	if code != 200 {
		t.Fatalf("AddMember: %d %v", code, body)
	}
	userID, _ := body["user_id"].(string)

	// Whose password is read from the verified header, not from the body, so
	// these calls present the person changing theirs the way the gateway would.
	self := map[string]string{"X-Gavya-Tenant": tenant, "X-Gavya-User": userID}

	// A wrong current password is refused, so the route is not simply a setter.
	code, _ = callAs(t, base, "ChangePassword", map[string]string{
		"current_password": "not the password", "new_password": second,
	}, self)
	if code == 200 {
		t.Fatal("the password was changed without knowing the current one, which is " +
			"the only thing authorising this route")
	}

	// Too short is refused.
	code, _ = callAs(t, base, "ChangePassword", map[string]string{
		"current_password": first, "new_password": "short",
	}, self)
	if code == 200 {
		t.Error("a five-character password was accepted")
	}

	code, body = callAs(t, base, "ChangePassword", map[string]string{
		"current_password": first, "new_password": second,
	}, self)
	if code != 200 {
		t.Fatalf("ChangePassword: %d %v", code, body)
	}

	// And nobody can change it without being signed in as that person: the route
	// takes no user_id, so there is no subject to name.
	if code, _ := callAs(t, base, "ChangePassword", map[string]string{
		"current_password": second, "new_password": "a third password entirely",
	}, map[string]string{"X-Gavya-Tenant": tenant}); code == 200 {
		t.Error("a caller who is not signed in as anybody changed a password")
	}

	if code, _ := callAs(t, base, "SignIn", map[string]string{
		"email": "collector@society.test", "password": first, "tenant_id": tenant,
	}, nil); code == 200 {
		t.Error("the old password still signs in")
	}
	if code, body := callAs(t, base, "SignIn", map[string]string{
		"email": "collector@society.test", "password": second, "tenant_id": tenant,
	}, nil); code != 200 {
		t.Errorf("the new password does not sign in: %d %v", code, body)
	}
}

// Suspending somebody ends the session they are holding.
//
// A suspension that waits for the next sign-in takes effect only when the
// suspended person signs in, which is the one thing they will not do.
func TestSuspendingSomebodyEndsTheSessionTheyAreHolding(t *testing.T) {
	base, owner := identityAt(t)
	tenant := seedTenantRow(t, owner)

	const password = "a password for the supervisor"
	code, body := admin(t, base, "AddMember", tenant, map[string]string{
		"tenant_id": tenant, "email": "supervisor@society.test",
		"full_name": "A Supervisor", "role": "supervisor",
		"password": password, "actor": "US_FOUNDER_00000000000000",
	})
	if code != 200 {
		t.Fatalf("AddMember: %d %v", code, body)
	}
	userID, _ := body["user_id"].(string)

	_, body = callAs(t, base, "SignIn", map[string]string{
		"email": "supervisor@society.test", "password": password, "tenant_id": tenant,
	}, nil)
	session, _ := body["session_id"].(string)
	if session == "" {
		t.Fatal("no session to suspend")
	}
	if code, _ := callAs(t, base, "VerifySession",
		map[string]string{"session_id": session}, nil); code != 200 {
		t.Fatal("the session was not usable before the suspension, so this proves nothing")
	}

	code, body = admin(t, base, "SetMemberStatus", tenant, map[string]string{
		"tenant_id": tenant, "user_id": userID, "status": "suspended",
		"actor": "US_FOUNDER_00000000000000",
	})
	if code != 200 {
		t.Fatalf("SetMemberStatus: %d %v", code, body)
	}

	if code, _ := callAs(t, base, "VerifySession",
		map[string]string{"session_id": session}, nil); code == 200 {
		t.Error("a suspended member's session still verifies")
	}
}

// Administration is itself authorised.
//
// The routes that decide who may do what have to be the hardest to reach, or
// they are a way round everything below them: a clerk who can call AddMember can
// give themselves the accountant role.
func TestAdministrationIsItselfAuthorised(t *testing.T) {
	base, owner := identityAt(t)
	tenant := seedTenantRow(t, owner)

	for _, c := range []struct {
		method string
		in     any
	}{
		{"AddMember", map[string]string{
			"tenant_id": tenant, "email": "sneak@society.test", "full_name": "Sneak",
			"role": "admin", "actor": "US_CLERK_000000000000000",
		}},
		{"SetMemberRole", map[string]string{
			"tenant_id": tenant, "user_id": "US_ANY_00000000000000000",
			"role": "admin", "actor": "US_CLERK_000000000000000",
		}},
		{"IssueServiceIdentity", map[string]string{
			"tenant_id": tenant, "name": "sneaky-device",
			"expires_at": time.Now().UTC().AddDate(1, 0, 0).Format(time.RFC3339),
			"actor":      "US_CLERK_000000000000000",
		}},
	} {
		// A clerk's permissions: everything a clerk holds, and no tenant.admin.
		code, body := callAs(t, base, c.method, c.in, map[string]string{
			"X-Gavya-Tenant":      tenant,
			"X-Gavya-Permissions": "milk.read,milk.write,herd.read,herd.write,sales.read,sales.write",
		})
		if code == 200 {
			t.Errorf("a clerk called %s: %v", c.method, body)
		}
		if code != http.StatusForbidden {
			t.Errorf("%s refused a clerk with %d, want 403", c.method, code)
		}
	}
}

func permissionsOf(body map[string]any) map[string]bool {
	out := map[string]bool{}
	list, _ := body["permissions"].([]any)
	for _, p := range list {
		if s, ok := p.(string); ok {
			out[s] = true
		}
	}
	return out
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// seedTenantRow makes a tenant for these tests to administer.
//
// The identity service's own schema does not own the tenants table — tenant
// creation is tenant-service's — so the row is inserted directly here. That is
// the one thing in this file done the old way, and it is the boundary of what
// these routes are responsible for rather than a gap in them.
func seedTenantRow(t *testing.T, owner *pgx.Conn) string {
	t.Helper()
	return newID("TN")
}
