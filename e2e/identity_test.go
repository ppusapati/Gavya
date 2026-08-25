//go:build e2e

// Identity, isolated and proved the same way everything else is.
//
// The identity tables are where the two hardest cases in this schema live: a
// table with no tenant column that still must not be readable across tenants,
// and the handful of lookups that legitimately run before any tenant is known
// because they are what establishes it.
package e2e

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// seedIdentity gives each tenant a role, and creates three people: one in alpha,
// one in beta, and one who belongs to both — which is the case the whole design
// exists for and the one a tenant_id column on the user could not express.
func seedIdentity(t *testing.T, owner *pgx.Conn) {
	t.Helper()
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := owner.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	for _, r := range []struct{ id, tenant string }{
		{"RL_ALPHA_MANAGER_000000000", alpha},
		{"RL_BETA_MANAGER_0000000000", beta},
	} {
		exec(`INSERT INTO roles (id,tenant_id,name,permissions,created_by,updated_by)
		      VALUES ($1,$2,'manager','{collections.read}','seed','seed')`, r.id, r.tenant)
	}

	for _, u := range []struct{ id, email, name string }{
		{"US_ALPHA_ONLY_000000000000", "alpha.only@example.test", "Alpha Only"},
		{"US_BETA_ONLY_0000000000000", "beta.only@example.test", "Beta Only"},
		{"US_BOTH_TENANTS_0000000000", "both@example.test", "Works At Both"},
	} {
		exec(`INSERT INTO users (id,email,email_normalised,full_name,password_hash,created_by,updated_by)
		      VALUES ($1,$2::text,lower($2::text),$3,'$argon2id$v=19$m=65536,t=3,p=4$c2FsdA$aGFzaA','seed','seed')`,
			u.id, u.email, u.name)
	}

	for _, m := range []struct{ id, tenant, user, role string }{
		{"MB_1_000000000000000000000", alpha, "US_ALPHA_ONLY_000000000000", "RL_ALPHA_MANAGER_000000000"},
		{"MB_2_000000000000000000000", beta, "US_BETA_ONLY_0000000000000", "RL_BETA_MANAGER_0000000000"},
		{"MB_3_000000000000000000000", alpha, "US_BOTH_TENANTS_0000000000", "RL_ALPHA_MANAGER_000000000"},
		{"MB_4_000000000000000000000", beta, "US_BOTH_TENANTS_0000000000", "RL_BETA_MANAGER_0000000000"},
	} {
		exec(`INSERT INTO tenant_memberships (id,tenant_id,user_id,role_id,created_by,updated_by)
		      VALUES ($1,$2,$3,$4,'seed','seed')`, m.id, m.tenant, m.user, m.role)
	}
}

// `users` has no tenant column and cannot have one, so the sweep that isolates
// everything else leaves it alone. Left alone, one tenant reads every other
// tenant's people — their addresses and their password hashes.
func TestATenantSeesOnlyItsOwnPeople(t *testing.T) {
	owner, app := isolated(t)
	seedIdentity(t, owner)
	ctx := context.Background()

	for _, tc := range []struct {
		tenant string
		want   []string
	}{
		{alpha, []string{"Alpha Only", "Works At Both"}},
		{beta, []string{"Beta Only", "Works At Both"}},
	} {
		scopeTo(t, app, tc.tenant)
		rows, err := app.Query(ctx, "SELECT full_name FROM users ORDER BY full_name")
		if err != nil {
			t.Fatalf("%s: %v", tc.tenant, err)
		}
		var got []string
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				t.Fatal(err)
			}
			got = append(got, n)
		}
		rows.Close()
		if len(got) != len(tc.want) {
			t.Errorf("%s sees %v, want %v", tc.tenant, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s sees %v, want %v", tc.tenant, got, tc.want)
				break
			}
		}
	}
}

// The person who works at both is one account, visible from both — which is the
// whole reason identity is not owned by a tenant. A tenant_id on the user would
// have made this two accounts, two passwords and two things to revoke.
func TestOnePersonWorkingAtTwoTenantsIsOneAccount(t *testing.T) {
	owner, app := isolated(t)
	seedIdentity(t, owner)
	ctx := context.Background()

	seen := map[string]string{}
	for _, tenant := range []string{alpha, beta} {
		scopeTo(t, app, tenant)
		var id string
		if err := app.QueryRow(ctx,
			"SELECT id FROM users WHERE full_name = 'Works At Both'").Scan(&id); err != nil {
			t.Fatalf("%s cannot see the shared user: %v", tenant, err)
		}
		seen[tenant] = id
	}
	if seen[alpha] != seen[beta] {
		t.Errorf("the same person is %q in alpha and %q in beta", seen[alpha], seen[beta])
	}
}

// Membership is what grants access, so removing it has to remove access.
func TestEndingSomebodysMembershipEndsTheirVisibility(t *testing.T) {
	owner, app := isolated(t)
	seedIdentity(t, owner)
	ctx := context.Background()

	if _, err := owner.Exec(ctx,
		"UPDATE tenant_memberships SET deleted_at = NOW() WHERE id = 'MB_3_000000000000000000000'"); err != nil {
		t.Fatal(err)
	}

	scopeTo(t, app, alpha)
	var n int
	if err := app.QueryRow(ctx,
		"SELECT count(*) FROM users WHERE full_name = 'Works At Both'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("alpha still sees somebody whose membership ended")
	}

	// And beta, where the membership stands, is unaffected.
	scopeTo(t, app, beta)
	if err := app.QueryRow(ctx,
		"SELECT count(*) FROM users WHERE full_name = 'Works At Both'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Error("ending a membership in alpha removed the person from beta as well")
	}
}

// A membership must not be able to point at a role defined in another tenant.
// Row-level security does not stop it — a foreign key is checked by the system
// rather than by the querying role — and the consequence here is somebody
// holding another tenant's permissions.
//
// Two things enforce this and it is worth knowing which. The schema declares the
// reference as composite, and gavya_make_foreign_keys_tenant_safe would convert
// it even if the schema had not: declaring it single-column and running the
// converter still passes, because the converter repairs it. That is the useful
// property — a service added later that writes the naive foreign key gets fixed
// at deploy rather than shipping a hole — but it means this test holds the
// converter, not the declaration.
func TestAMembershipCannotBorrowAnotherTenantsRole(t *testing.T) {
	owner, app := isolated(t)
	seedIdentity(t, owner)
	scopeTo(t, app, alpha)

	_, err := app.Exec(context.Background(), `
		INSERT INTO tenant_memberships (id,tenant_id,user_id,role_id,created_by,updated_by)
		VALUES ('MB_X_000000000000000000000',$1,'US_BETA_ONLY_0000000000000',
		        'RL_BETA_MANAGER_0000000000','alpha','alpha')`, alpha)
	if err == nil {
		t.Fatal("alpha granted one of its people a role belonging to beta")
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23503" {
		t.Errorf("refused with %v, want a foreign key violation (23503)", err)
	}
}

// Finding the account for an email address is what establishes the tenant, so it
// cannot already know one. It runs with the definer's rights for exactly that
// reason, and this is the test that it does.
func TestTheLoginLookupWorksBeforeATenantIsKnown(t *testing.T) {
	owner, app := isolated(t)
	seedIdentity(t, owner)
	ctx := context.Background()

	// No tenant on this connection at all — the state every sign-in starts from.
	var found bool
	var userID, hash *string
	if err := app.QueryRow(ctx,
		`SELECT found, user_id, password_hash FROM gavya_find_user_for_login($1)`,
		"alpha.only@example.test").Scan(&found, &userID, &hash); err != nil {
		t.Fatalf("the login lookup was refused: %v", err)
	}
	if !found || userID == nil || *userID != "US_ALPHA_ONLY_000000000000" {
		t.Fatalf("found=%v user=%v, want the account", found, userID)
	}
	if hash == nil || *hash == "" {
		t.Error("no stored credential came back, so nothing could be verified")
	}
}

// An address with no account must come back in the same shape as one with an
// account, so the caller does the same work either way. A caller that can return
// early is measurably faster on a missing account, and that difference tells an
// attacker which addresses are worth attacking.
func TestAnUnknownAddressComesBackInTheSameShape(t *testing.T) {
	owner, app := isolated(t)
	seedIdentity(t, owner)

	rows, err := app.Query(context.Background(),
		`SELECT found, user_id, password_hash FROM gavya_find_user_for_login($1)`,
		"nobody@example.test")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		n++
		var found bool
		var userID, hash *string
		if err := rows.Scan(&found, &userID, &hash); err != nil {
			t.Fatal(err)
		}
		if found {
			t.Error("an address with no account reported found")
		}
	}
	// Checked before the count is judged. Without this a query that failed
	// outright reads as "no rows", which is the very thing being asserted about.
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d rows for an unknown address, want exactly 1 — a caller that gets "+
			"no row can return early, and that is a timing signal", n)
	}
}

// The lookup answers one question and must not become a way to read the table.
func TestTheLoginLookupDoesNotOpenTheUsersTable(t *testing.T) {
	owner, app := isolated(t)
	seedIdentity(t, owner)

	// With no tenant set, a direct read is refused — the function is the only
	// way in, and it only answers about one address at a time.
	var n int
	err := app.QueryRow(context.Background(), "SELECT count(*) FROM users").Scan(&n)
	if err == nil {
		t.Fatalf("an unscoped connection read %d users directly", n)
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "42501" {
		t.Errorf("failed with %v, want a refusal", err)
	}
}

func TestASessionResolvesToTheTenantItActsFor(t *testing.T) {
	owner, app := isolated(t)
	seedIdentity(t, owner)
	ctx := context.Background()

	if _, err := owner.Exec(ctx, `
		INSERT INTO auth_sessions (id,tenant_id,user_id,expires_at,created_by,updated_by)
		VALUES ('SE_LIVE_000000000000000000',$1,'US_ALPHA_ONLY_000000000000',
		        NOW() + INTERVAL '1 hour','seed','seed')`, alpha); err != nil {
		t.Fatal(err)
	}

	var valid bool
	var reason, tenant, user *string
	if err := app.QueryRow(ctx,
		"SELECT valid, reason, tenant_id, user_id FROM gavya_resolve_session($1)",
		"SE_LIVE_000000000000000000").Scan(&valid, &reason, &tenant, &user); err != nil {
		t.Fatalf("resolving a session was refused: %v", err)
	}
	if !valid {
		t.Fatalf("a live session did not resolve: %v", deref(reason))
	}
	if tenant == nil || *tenant != alpha {
		t.Errorf("session resolved to tenant %v, want alpha", deref(tenant))
	}
}

// The reason sessions are rows. Without this, a signed token works until it
// expires and dismissal, credential theft and a lost laptop are all
// unhandleable for the length of the lifetime.
func TestARevokedSessionStopsWorkingImmediately(t *testing.T) {
	owner, app := isolated(t)
	seedIdentity(t, owner)
	ctx := context.Background()

	for _, s := range []struct{ id, expires, revoked, wantReason string }{
		{"SE_REVOKED_000000000000000", "NOW() + INTERVAL '1 hour'", "NOW()", "signed out"},
		{"SE_EXPIRED_000000000000000", "NOW() - INTERVAL '1 second'", "NULL", "expired"},
	} {
		if _, err := owner.Exec(ctx, `
			INSERT INTO auth_sessions (id,tenant_id,user_id,expires_at,revoked_at,revoked_reason,
			                           created_by,updated_by)
			VALUES ($1,$2,'US_ALPHA_ONLY_000000000000',`+s.expires+`,`+s.revoked+`,
			        'signed out','seed','seed')`, s.id, alpha); err != nil {
			t.Fatal(err)
		}

		var valid bool
		var reason, tenant *string
		if err := app.QueryRow(ctx,
			"SELECT valid, reason, tenant_id FROM gavya_resolve_session($1)", s.id).
			Scan(&valid, &reason, &tenant); err != nil {
			t.Fatal(err)
		}
		if valid {
			t.Errorf("%s still resolves", s.id)
		}
		if tenant != nil {
			t.Errorf("%s yielded tenant %q despite being unusable — a caller that reads "+
				"the tenant without checking valid would scope the request anyway", s.id, *tenant)
		}
		if reason == nil || *reason != s.wantReason {
			t.Errorf("%s gave reason %v, want %q", s.id, deref(reason), s.wantReason)
		}
	}
}

func TestAnUnknownSessionDoesNotResolve(t *testing.T) {
	owner, app := isolated(t)
	seedIdentity(t, owner)

	var valid bool
	var reason *string
	if err := app.QueryRow(context.Background(),
		"SELECT valid, reason FROM gavya_resolve_session($1)", "SE_NEVER_EXISTED_000000000").
		Scan(&valid, &reason); err != nil {
		t.Fatal(err)
	}
	if valid {
		t.Fatal("a session that was never issued resolved")
	}
	if reason == nil || *reason != "no such session" {
		t.Errorf("reason = %v", deref(reason))
	}
}

// A service credential is a password nobody notices needs rotating, so the
// expiry is checked where the credential is read rather than left to whoever
// remembers.
func TestAServiceIdentityCarriesAnExpiry(t *testing.T) {
	owner, _ := isolated(t)
	ctx := context.Background()

	_, err := owner.Exec(ctx, `
		INSERT INTO service_identities (id,name,secret_hash,created_by,updated_by)
		VALUES ('SI_NO_EXPIRY_000000000000','recomputer','$argon2id$x','seed','seed')`)
	if err == nil {
		t.Fatal("a service credential with no expiry was accepted")
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23502" {
		t.Errorf("refused with %v, want a not-null violation on expires_at", err)
	}
}

// A session belongs to a person or to a service, never both and never neither.
// Either would leave an entry in the audit trail that cannot say who acted.
func TestASessionHasExactlyOneSubject(t *testing.T) {
	owner, _ := isolated(t)
	seedIdentity(t, owner)
	ctx := context.Background()

	if _, err := owner.Exec(ctx, `
		INSERT INTO service_identities (id,tenant_id,name,secret_hash,expires_at,created_by,updated_by)
		VALUES ('SI_1_000000000000000000000',$1,'ingestion','$argon2id$x',
		        NOW() + INTERVAL '30 days','seed','seed')`, alpha); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ name, user, service string }{
		{"neither", "NULL", "NULL"},
		{"both", "'US_ALPHA_ONLY_000000000000'", "'SI_1_000000000000000000000'"},
	} {
		_, err := owner.Exec(ctx, `
			INSERT INTO auth_sessions (id,tenant_id,user_id,service_identity_id,expires_at,
			                           created_by,updated_by)
			VALUES ('SE_BAD_00000000000000000',$1,`+tc.user+`,`+tc.service+`,
			        NOW() + INTERVAL '1 hour','seed','seed')`, alpha)
		if err == nil {
			t.Errorf("a session with %s subject was accepted", tc.name)
			_, _ = owner.Exec(ctx, "DELETE FROM auth_sessions WHERE id='SE_BAD_00000000000000000'")
		}
	}
}

// A person lands in one tenant when they sign in without saying which, and
// "one" has to mean one.
func TestAPersonHasAtMostOneDefaultTenant(t *testing.T) {
	owner, _ := isolated(t)
	seedIdentity(t, owner)
	ctx := context.Background()

	if _, err := owner.Exec(ctx,
		"UPDATE tenant_memberships SET is_default = TRUE WHERE id = 'MB_3_000000000000000000000'"); err != nil {
		t.Fatal(err)
	}
	_, err := owner.Exec(ctx,
		"UPDATE tenant_memberships SET is_default = TRUE WHERE id = 'MB_4_000000000000000000000'")
	if err == nil {
		t.Fatal("a person was given two default tenants")
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23505" {
		t.Errorf("refused with %v, want a uniqueness violation", err)
	}
}

// Failed sign-ins are the useful half of the record. A hundred failures against
// ninety addresses is a credential-stuffing run and looks like nothing at all if
// only the successes are kept.
func TestAFailedSignInAgainstAnUnknownAddressIsStillRecorded(t *testing.T) {
	owner, _ := isolated(t)
	ctx := context.Background()

	if _, err := owner.Exec(ctx, `
		INSERT INTO authentication_attempts (id,email_normalised,succeeded,failure_reason,
		                                     ip_address,attempted_at)
		VALUES ('AA_1_000000000000000000000','nobody@example.test',FALSE,'no such account',
		        '203.0.113.9',$1)`, time.Now()); err != nil {
		t.Fatalf("an attempt against an unknown address could not be recorded: %v", err)
	}

	var n int
	if err := owner.QueryRow(ctx,
		"SELECT count(*) FROM authentication_attempts WHERE NOT succeeded").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d failed attempts recorded, want 1", n)
	}
}
