//go:build e2e

// Tenant isolation, proved against a real PostgreSQL.
//
// Row-level security is the kind of control that reports success while doing
// nothing. The tables say RLS is enabled, the policies are listed, `\d` shows
// them — and every cross-tenant read still succeeds, because the connecting role
// is a superuser and superusers are not subject to policies. Reading the schema
// cannot tell you which of those two worlds you are in. Only a query can.
//
// So these tests do not inspect the catalogue and conclude. They write two
// tenants' rows and then try to steal them.
package e2e

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	alpha = "T_AAAAAAAAAAAAAAAAAAAAAAAA"
	beta  = "T_BBBBBBBBBBBBBBBBBBBBBBBB"
)

// isolated brings up a database holding every service's schema, applies the
// isolation policies, and returns a connection as the owner and one as the
// unprivileged application role.
func isolated(t *testing.T) (owner, app *pgx.Conn) {
	t.Helper()
	ctx := context.Background()

	admin, err := pgx.Connect(ctx, dsn(t, "postgres"))
	if err != nil {
		t.Fatalf("connect as admin: %v", err)
	}
	defer admin.Close(ctx)

	const db = "e2e_isolation"
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+db+" WITH (FORCE)"); err != nil {
		t.Fatalf("drop %s: %v", db, err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+db); err != nil {
		t.Fatalf("create %s: %v", db, err)
	}

	owner, err = pgx.Connect(ctx, dsn(t, db))
	if err != nil {
		t.Fatalf("connect as owner: %v", err)
	}
	t.Cleanup(func() { owner.Close(context.Background()) })

	root := workspaceRoot(t)
	run := func(path string) {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if _, err := owner.Exec(ctx, string(b)); err != nil {
			t.Fatalf("apply %s: %v", path, err)
		}
	}

	run("pkg/database/schema/gen_ulid_polyfill.sql")
	schemas, err := filepath.Glob(filepath.Join(root, "services", "*", "internal", "db", "schema.sql"))
	if err != nil || len(schemas) == 0 {
		t.Fatalf("no service schemas found under %s (%v)", root, err)
	}
	for _, s := range schemas {
		rel, _ := filepath.Rel(root, s)
		run(rel)
	}
	run("libs/integrity/isolation/isolation.sql")

	if _, err := owner.Exec(ctx, "SELECT gavya_apply_tenant_isolation()"); err != nil {
		t.Fatalf("apply isolation: %v", err)
	}
	if _, err := owner.Exec(ctx, "SELECT gavya_grant_app_access()"); err != nil {
		t.Fatalf("grant app access: %v", err)
	}

	appDSN := strings.Replace(dsn(t, db), "postgres://", "postgres://", 1)
	app, err = pgx.Connect(ctx, asRole(appDSN, "gavya_app"))
	if err != nil {
		t.Fatalf("connect as gavya_app: %v", err)
	}
	t.Cleanup(func() { app.Close(context.Background()) })

	seed(t, owner)
	return owner, app
}

// asRole rewrites the user in a DSN, so the application role connects to the
// same database the owner just prepared.
func asRole(d, role string) string {
	cfg, err := pgx.ParseConfig(d)
	if err != nil {
		return d
	}
	cfg.User = role
	cfg.Password = ""
	return fmt.Sprintf("postgres://%s@%s:%d/%s?sslmode=disable",
		cfg.User, cfg.Host, cfg.Port, cfg.Database)
}

// seed writes one tenant and one animal for each of two tenants, as the owner.
func seed(t *testing.T, owner *pgx.Conn) {
	t.Helper()
	ctx := context.Background()
	for _, tt := range []struct{ id, slug, email, currency string }{
		{alpha, "alpha", "a@example.test", "INR"},
		{beta, "beta", "b@example.test", "KES"},
	} {
		if _, err := owner.Exec(ctx, `
			INSERT INTO tenants (id,name,slug,contact_email,currency,currency_scale,created_by,updated_by)
			VALUES ($1,$2,$3,$4,$5,2,'seed','seed')`,
			tt.id, strings.ToTitle(tt.slug)+" Dairy", tt.slug, tt.email, tt.currency); err != nil {
			t.Fatalf("seed tenant %s: %v", tt.slug, err)
		}
	}
	for _, c := range []struct{ id, tenant, tag string }{
		{"C_AAAAAAAAAAAAAAAAAAAAAAAA", alpha, "ALPHA-1"},
		{"C_BBBBBBBBBBBBBBBBBBBBBBBB", beta, "BETA-1"},
	} {
		if _, err := owner.Exec(ctx, `
			INSERT INTO cattle (id,tenant_id,tag_number,gender,created_by,updated_by)
			VALUES ($1,$2,$3,'F','seed','seed')`, c.id, c.tenant, c.tag); err != nil {
			t.Fatalf("seed cattle %s: %v", c.tag, err)
		}
	}
}

// deref renders a nullable aggregate for a failure message. A test that reports
// a pointer address instead of the rows it saw is a test nobody can act on.
func deref(s *string) string {
	if s == nil {
		return "nothing"
	}
	return *s
}

func scopeTo(t *testing.T, c *pgx.Conn, tenant string) {
	t.Helper()
	if _, err := c.Exec(context.Background(), "SET app.tenant_id = '"+tenant+"'"); err != nil {
		t.Fatalf("set tenant: %v", err)
	}
}

func TestOneTenantCannotReadAnother(t *testing.T) {
	_, app := isolated(t)
	ctx := context.Background()

	for _, tc := range []struct{ tenant, wantTag string }{
		{alpha, "ALPHA-1"},
		{beta, "BETA-1"},
	} {
		scopeTo(t, app, tc.tenant)
		var n int
		var tags *string
		if err := app.QueryRow(ctx,
			"SELECT count(*), string_agg(tag_number, ',') FROM cattle").Scan(&n, &tags); err != nil {
			t.Fatalf("%s: %v", tc.tenant, err)
		}
		if n != 1 || tags == nil || *tags != tc.wantTag {
			t.Errorf("%s sees %d rows (%s), want exactly its own %q",
				tc.tenant, n, deref(tags), tc.wantTag)
		}
	}
}

// The tenants table carries no tenant_id — its own key is the tenant — so it is
// the one table a column-driven sweep would skip. Skipped, one customer can read
// every other customer's name, contact address and plan.
func TestTheTenantRegistryIsAlsoIsolated(t *testing.T) {
	_, app := isolated(t)
	scopeTo(t, app, alpha)

	var slugs *string
	if err := app.QueryRow(context.Background(),
		"SELECT string_agg(slug, ',') FROM tenants").Scan(&slugs); err != nil {
		t.Fatal(err)
	}
	if slugs == nil || *slugs != "alpha" {
		t.Errorf("tenant registry shows %s to alpha, want only its own row", deref(slugs))
	}
}

func TestATenantCannotWriteARowBelongingToAnother(t *testing.T) {
	_, app := isolated(t)
	scopeTo(t, app, alpha)

	_, err := app.Exec(context.Background(), `
		INSERT INTO cattle (id,tenant_id,tag_number,gender,created_by,updated_by)
		VALUES ('C_XXXXXXXXXXXXXXXXXXXXXXXX',$1,'STOLEN','F','alpha','alpha')`, beta)
	if err == nil {
		t.Fatal("alpha inserted a row owned by beta")
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "42501" {
		t.Errorf("refused with %v, want a row-level security violation (42501)", err)
	}
}

func TestATenantCannotChangeAnotherTenantsRow(t *testing.T) {
	owner, app := isolated(t)
	ctx := context.Background()
	scopeTo(t, app, alpha)

	tag, err := app.Exec(ctx, "UPDATE cattle SET tag_number='HIJACKED' WHERE id='C_BBBBBBBBBBBBBBBBBBBBBBBB'")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if n := tag.RowsAffected(); n != 0 {
		t.Errorf("alpha changed %d of beta's rows", n)
	}

	// The row is checked from outside the policy, because "zero rows affected"
	// and "the row was changed but the count was wrong" look identical from
	// inside a tenant that cannot see it.
	var got string
	if err := owner.QueryRow(ctx,
		"SELECT tag_number FROM cattle WHERE id='C_BBBBBBBBBBBBBBBBBBBBBBBB'").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != "BETA-1" {
		t.Errorf("beta's animal is now tagged %q", got)
	}
}

func TestATenantCannotDeleteAnotherTenantsRow(t *testing.T) {
	owner, app := isolated(t)
	ctx := context.Background()
	scopeTo(t, app, alpha)

	if _, err := app.Exec(ctx, "DELETE FROM cattle WHERE id='C_BBBBBBBBBBBBBBBBBBBBBBBB'"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var n int
	if err := owner.QueryRow(ctx,
		"SELECT count(*) FROM cattle WHERE id='C_BBBBBBBBBBBBBBBBBBBBBBBB'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Error("alpha deleted beta's animal")
	}
}

// A connection that never set a tenant must fail loudly. The tempting
// alternative — current_setting(..., true), which yields NULL and therefore no
// rows — turns a connection-handling bug into an empty result set, and an empty
// result set is indistinguishable from a tenant that genuinely has no data. That
// is how this kind of defect reaches production.
func TestAQueryWithNoTenantSetIsRefusedRatherThanReturningNothing(t *testing.T) {
	_, app := isolated(t)

	var n int
	err := app.QueryRow(context.Background(), "SELECT count(*) FROM cattle").Scan(&n)
	if err == nil {
		t.Fatalf("a query with no tenant set returned %d rows instead of failing", n)
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "42704" {
		t.Errorf("failed with %v, want an unrecognised-parameter error naming app.tenant_id", err)
	}
}

// The control is the role, not the policy. This is the same query as
// TestOneTenantCannotReadAnother, run as the superuser that every service in
// docker-compose.yaml connects as today — and it reads both tenants with the
// policies fully in place. If this test ever stops seeing both rows, the reason
// is worth understanding before celebrating.
func TestASuperuserSeesEverythingDespiteThePolicies(t *testing.T) {
	owner, _ := isolated(t)
	ctx := context.Background()

	var super, bypass bool
	if err := owner.QueryRow(ctx,
		"SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = current_user").Scan(&super, &bypass); err != nil {
		t.Fatal(err)
	}
	if !super && !bypass {
		t.Skip("the owner connection is neither superuser nor BYPASSRLS, so there is nothing to demonstrate")
	}

	var n int
	if err := owner.QueryRow(ctx, "SELECT count(*) FROM cattle").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("the privileged role saw %d rows, want both tenants' — "+
			"this test exists to show that the policies alone are not the control", n)
	}
}

// The application role must not be able to escape the policies by any route.
func TestTheApplicationRoleHasNoWayPastThePolicies(t *testing.T) {
	owner, _ := isolated(t)

	var super, bypass, createrole bool
	if err := owner.QueryRow(context.Background(),
		"SELECT rolsuper, rolbypassrls, rolcreaterole FROM pg_roles WHERE rolname='gavya_app'").
		Scan(&super, &bypass, &createrole); err != nil {
		t.Fatal(err)
	}
	if super {
		t.Error("gavya_app is a superuser, so every policy in the database is decorative")
	}
	if bypass {
		t.Error("gavya_app holds BYPASSRLS, so every policy in the database is decorative")
	}
	if createrole {
		t.Error("gavya_app can create roles, so it can grant itself a way out")
	}
}

// Every table that carries a tenant has to be covered. One that is not is not a
// smaller problem than none of them being covered: it is the table an attacker
// reads.
func TestEveryTenantOwnedTableIsCovered(t *testing.T) {
	owner, _ := isolated(t)
	ctx := context.Background()

	rows, err := owner.Query(ctx, `
		SELECT schema_name, table_name, has_tenant_column, rls_enabled, rls_forced, policies
		FROM gavya_isolation_report`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var covered, uncovered, noTenant int
	for rows.Next() {
		var schema, table string
		var hasTenant, enabled, forced bool
		var policies int
		if err := rows.Scan(&schema, &table, &hasTenant, &enabled, &forced, &policies); err != nil {
			t.Fatal(err)
		}
		// The tenants table has no tenant_id and is isolated on its own key.
		if !hasTenant && table != "tenants" {
			noTenant++
			if enabled {
				t.Errorf("%s.%s has no tenant column but carries a tenant policy", schema, table)
			}
			continue
		}
		if enabled && forced && policies > 0 {
			covered++
			continue
		}
		uncovered++
		t.Errorf("%s.%s carries a tenant but is not isolated (enabled=%v forced=%v policies=%d)",
			schema, table, enabled, forced, policies)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if covered == 0 {
		t.Fatal("no tables were found to be covered, so this test would pass vacuously")
	}
	t.Logf("%d tenant-owned tables isolated, %d not, %d tables carry no tenant", covered, uncovered, noTenant)
}
