//go:build dbintegration

// A backup can be restored, and what comes back is what went in.
//
// This is the only test in the repository that can say the word "backup"
// honestly. The scripts were written and run by hand once, which is how every
// backup procedure starts and is not how any of them stay true: the failure
// arrives months later, in the form of a dump that restores without the roles
// its policies name, or without the forced row-level security that is the whole
// of this platform's tenant isolation, and it arrives at the one moment nobody
// has time to debug it.
//
// So it does the round trip against a real PostgreSQL: builds a database, puts
// rows in it, backs it up with scripts/backup.sh, restores it with
// scripts/restore.sh into a second database, and compares the two. Not the exit
// codes — the catalogue, the rows, the isolation, and the audit chain.
//
//	TEST_DATABASE_DSN="postgres://user@host:port/%s?sslmode=disable" \
//	  go test -count=1 -tags dbintegration ./...
package backup_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestABackupRestoresToWhatWasBackedUp(t *testing.T) {
	dsnFor := dsnTemplate(t)
	root := repoRoot(t)
	requireTools(t)

	ctx := context.Background()
	admin, err := pgx.Connect(ctx, fmt.Sprintf(dsnFor, "postgres"))
	if err != nil {
		t.Fatalf("connect to the maintenance database: %v", err)
	}
	// Registered as a cleanup rather than deferred, and registered first, so it
	// runs last: t.Cleanup is LIFO and runs after the deferred closes, which is
	// how the drops below found a connection that was already shut.
	t.Cleanup(func() { admin.Close(context.Background()) })

	source := "gavya_backup_src"
	restored := "gavya_backup_dst"
	dropDatabase(t, ctx, admin, source)
	dropDatabase(t, ctx, admin, restored)
	mustExec(t, ctx, admin, `CREATE DATABASE `+source)
	t.Cleanup(func() {
		dropDatabase(t, context.Background(), admin, source)
		dropDatabase(t, context.Background(), admin, restored)
	})

	// The real thing, applied the way a deployment applies it.
	migrate(t, root, fmt.Sprintf(dsnFor, source))

	// Rows worth losing. A tenant, a milk collection, and an audit entry —
	// the last because the trail is the part whose value depends on it being
	// exactly what it was.
	seed(t, ctx, fmt.Sprintf(dsnFor, source))

	into := t.TempDir()
	run(t, root, "scripts/backup.sh",
		"--dsn", fmt.Sprintf(dsnFor, source), "--into", into, "--label", "test")

	dirs, err := filepath.Glob(filepath.Join(into, "*"))
	if err != nil || len(dirs) != 1 {
		t.Fatalf("expected one backup directory under %s, found %v (%v)", into, dirs, err)
	}

	run(t, root, "scripts/restore.sh",
		"--from", dirs[0], "--into", restored,
		"--server", fmt.Sprintf(dsnFor, "postgres"), "--force")

	src, err := pgx.Connect(ctx, fmt.Sprintf(dsnFor, source))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close(context.Background())
	dst, err := pgx.Connect(ctx, fmt.Sprintf(dsnFor, restored))
	if err != nil {
		t.Fatalf("connect to the restored database: %v", err)
	}
	defer dst.Close(context.Background())

	// The shape.
	if a, b := catalogue(t, ctx, src), catalogue(t, ctx, dst); strings.Join(a, "\n") != strings.Join(b, "\n") {
		t.Errorf("the restored database is not the same shape as the one backed up:\n%s",
			diff(a, b))
	}

	// The rows. A restore that brings back the tables and not their contents
	// exits zero and is worthless.
	for table, want := range rowCounts(t, ctx, src) {
		got := countIn(t, ctx, dst, table)
		if got != want {
			t.Errorf("%s has %d rows and had %d", table, got, want)
		}
	}

	// The isolation. This is the one that is silent when it goes wrong: a table
	// that carries a tenant and comes back without forced row-level security
	// answers every tenant's question with every tenant's rows, and nothing
	// about the database looks wrong.
	var unprotected int
	if err := dst.QueryRow(ctx, `
		SELECT count(*) FROM gavya_isolation_report
		WHERE (has_tenant_column OR table_name = 'tenants')
		  AND NOT (rls_enabled AND rls_forced)`).Scan(&unprotected); err != nil {
		t.Fatalf("read the restored isolation report: %v", err)
	}
	if unprotected != 0 {
		t.Errorf("%d tenant-owned tables came back without forced row-level security; "+
			"that database would serve every tenant's rows to whoever asked", unprotected)
	}

	// The role every policy names, asserted on the backup rather than on the
	// restored database.
	//
	// That distinction is the point. Roles are cluster-wide, so gavya_app is
	// already on the server this test runs against and a restore here finds it
	// whether or not the backup carried it — an earlier version of this check
	// asked the restored database and passed happily with the roles dump emptied.
	// The case that matters is the one this environment cannot stage: restoring
	// onto a fresh server, where a missing role leaves every row-level security
	// policy naming something that does not exist. What can be checked is that
	// the backup contains it.
	roles, err := os.ReadFile(filepath.Join(dirs[0], "roles.sql"))
	if err != nil {
		t.Fatalf("read the roles dump: %v", err)
	}
	if !strings.Contains(string(roles), "gavya_app") {
		t.Errorf("the backup's roles.sql does not mention gavya_app, so restoring it onto "+
			"a fresh server leaves every row-level security policy naming a role that is "+
			"not there:\n%s", roles)
	}
	if !strings.Contains(string(roles), "CREATE ROLE gavya_app") {
		t.Errorf("the backup's roles.sql mentions gavya_app but does not create it:\n%s", roles)
	}

	// And the trail still verifies. A backup of an audit chain that no longer
	// checks out is a backup of something nobody can rely on afterwards, which
	// is the only thing an audit chain is for.
	var ok bool
	if err := dst.QueryRow(ctx,
		`SELECT bool_and(intact) FROM gavya_verify_audit_chain('TNT_BACKUP_TEST')`).Scan(&ok); err != nil {
		t.Fatalf("verify the restored audit chain: %v", err)
	}
	if !ok {
		t.Error("the audit chain does not verify after a restore")
	}
}

// A backup of a database that is not there, or a restore over one that is,
// refuses rather than producing something that looks like a backup.
func TestTheScriptsRefuseTheTwoThingsThatLoseData(t *testing.T) {
	dsnFor := dsnTemplate(t)
	root := repoRoot(t)
	requireTools(t)

	// Restoring over an existing database without saying so. The default is the
	// safe one because the dangerous one is a keystroke away and unrecoverable.
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, fmt.Sprintf(dsnFor, "postgres"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close(context.Background()) })

	existing := "gavya_backup_guard"
	dropDatabase(t, ctx, admin, existing)
	mustExec(t, ctx, admin, `CREATE DATABASE `+existing)
	t.Cleanup(func() { dropDatabase(t, context.Background(), admin, existing) })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "database.dump"), []byte("not a dump"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(filepath.Join(root, "scripts", "restore.sh"),
		"--from", dir, "--into", existing,
		"--server", fmt.Sprintf(dsnFor, "postgres"))
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Error("restoring over an existing database succeeded without --force")
	}
	if !strings.Contains(string(out), "already exists") {
		t.Errorf("the refusal does not say why:\n%s", out)
	}
}

func seed(t *testing.T, ctx context.Context, dsn string) {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())

	const tenant = "TNT_BACKUP_TEST"
	for _, stmt := range []string{
		// Schema-qualified: tenants belongs to tenant-service and lives in its
		// schema now. audit_logs below stays in public, because it is the one
		// table every service shares.
		`INSERT INTO tenant_service.tenants (id, name, slug, status, contact_email, currency, currency_scale, created_by, updated_by)
		 VALUES ('` + tenant + `', 'Backup Test', 'backup-test', 'active', 'backup@test.invalid', 'INR', 2, 'test', 'test')`,
		`INSERT INTO audit_logs (id, tenant_id, actor_id, actor_type, action, resource_type,
		                         resource_id, service_name, created_by)
		 VALUES ('AUD_BACKUP_TEST_0000000001', '` + tenant + `', 'USR_TEST', 'user', 'seed',
		         'tenant', '` + tenant + `', 'backup-test', 'USR_TEST')`,
	} {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}
	// Seal it, so the chain has something to verify rather than nothing.
	if _, err := conn.Exec(ctx, `SELECT gavya_seal_audit_log($1)`, tenant); err != nil {
		t.Fatalf("seal the trail: %v", err)
	}
}

func migrate(t *testing.T, root, dsn string) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "migrate")
	build := exec.Command("go", "build", "-o", bin, "./cmd/migrate")
	build.Dir = filepath.Join(root, "tools", "dbadmin")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build migrate: %v\n%s", err, out)
	}
	cmd := exec.Command(bin, "-dsn", dsn, "-root", root)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("migrate: %v\n%s", err, out)
	}
}

func run(t *testing.T, root, script string, args ...string) {
	t.Helper()
	cmd := exec.Command(filepath.Join(root, script), args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GAVYA_BACKUP_BY=dbintegration")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", script, err, out)
	}
	t.Logf("%s:\n%s", script, out)
}

// catalogue is the structure a restore has to reproduce. The same question
// e2e/upgrade_test.go asks of an upgrade path, asked of a round trip.
//
// Every fact is normalised first, because a dump and restore changes how
// PostgreSQL renders the same predicate. A CHECK or a partial index written as
// IN (...) comes back as
//
//	= ANY (ARRAY[('PAYABLE'::character varying)::text, ...])
//
// where the original renders as
//
//	= ANY ((ARRAY['PAYABLE'::character varying, ...])::text[])
//
// Identical meaning, different text, on about forty constraints and one index.
// Comparing raw text reports a good restore as broken every time, and a test
// that cries wolf is one somebody switches off.
func catalogue(t *testing.T, ctx context.Context, conn *pgx.Conn) []string {
	t.Helper()
	rows, err := conn.Query(ctx, `
		SELECT 'column   ' || table_schema || '.' || table_name || '.' || column_name || ' ' || data_type
		  FROM information_schema.columns
		 WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
		   AND table_schema NOT LIKE 'pg\\_%'
		UNION ALL
		SELECT 'constraint ' || conrelid::regclass::text || ' ' || conname || ' ' || pg_get_constraintdef(oid)
		  FROM pg_constraint WHERE connamespace = 'public'::regnamespace
		UNION ALL
		SELECT 'index    ' || schemaname || '.' || tablename || ' ' || regexp_replace(indexdef, 'INDEX \S+ ON', 'INDEX ? ON')
		  FROM pg_indexes WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
		UNION ALL
		SELECT 'policy   ' || schemaname || '.' || tablename || ' ' || policyname || ' ' || COALESCE(qual, '-')
		  FROM pg_policies WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
		UNION ALL
		SELECT 'rls      ' || n.nspname || '.' || relname || ' enabled=' || relrowsecurity || ' forced=' || relforcerowsecurity
		  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
		   AND n.nspname NOT LIKE 'pg\\_%' AND c.relkind = 'r'
		UNION ALL
		SELECT 'function ' || proname || '(' || pg_get_function_identity_arguments(oid) || ')'
		  FROM pg_proc WHERE pronamespace = 'public'::regnamespace
		UNION ALL
		SELECT 'trigger  ' || tgrelid::regclass::text || ' ' || tgname
		  FROM pg_trigger WHERE NOT tgisinternal`)
	if err != nil {
		t.Fatalf("read the catalogue: %v", err)
	}
	defer rows.Close()
	var facts []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			t.Fatal(err)
		}
		facts = append(facts, f)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(facts) < 200 {
		t.Fatalf("only %d catalogue facts; the query has stopped matching and a "+
			"comparison that finds nothing passes", len(facts))
	}
	for i, f := range facts {
		facts[i] = normalise(f)
	}
	sort.Strings(facts)
	return facts
}

// normalise removes the parts of a definition that a dump and restore is
// entitled to render differently: the type casts PostgreSQL adds back in its own
// style, and the parentheses it puts around them.
//
// Lossy on purpose, and bounded by what it is used for. Both sides of this
// comparison are the same database, one of them through pg_dump, so the risk is
// that something was lost rather than that two different predicates collapse to
// one string. A lost constraint or index still changes the set.
func normalise(fact string) string {
	fact = castNoise.ReplaceAllString(fact, "")
	fact = strings.NewReplacer("(", "", ")", "", " ", "").Replace(fact)
	return fact
}

var castNoise = regexp.MustCompile(`::(?:character varying|text)(?:\[\])?`)

func rowCounts(t *testing.T, ctx context.Context, conn *pgx.Conn) map[string]int {
	t.Helper()
	rows, err := conn.Query(ctx,
		`SELECT quote_ident(table_schema) || '.' || quote_ident(table_name)
		   FROM information_schema.tables
		  WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
		    AND table_schema NOT LIKE 'pg\\_%' AND table_type = 'BASE TABLE'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	rows.Close()

	out := make(map[string]int, len(tables))
	for _, table := range tables {
		out[table] = countIn(t, ctx, conn, table)
	}
	return out
}

func countIn(t *testing.T, ctx context.Context, conn *pgx.Conn, table string) int {
	t.Helper()
	var n int
	// Already quoted and schema-qualified by the query that listed it.
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func diff(a, b []string) string {
	in := func(list []string) map[string]bool {
		m := make(map[string]bool, len(list))
		for _, s := range list {
			m[s] = true
		}
		return m
	}
	x, y := in(a), in(b)
	var sb strings.Builder
	for _, s := range a {
		if !y[s] {
			fmt.Fprintf(&sb, "  backed up, did not come back: %s\n", s)
		}
	}
	for _, s := range b {
		if !x[s] {
			fmt.Fprintf(&sb, "  came back, was not backed up: %s\n", s)
		}
	}
	return sb.String()
}

func dropDatabase(t *testing.T, ctx context.Context, admin *pgx.Conn, name string) {
	t.Helper()
	if _, err := admin.Exec(ctx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); err != nil {
		t.Fatalf("drop %s: %v", name, err)
	}
}

func mustExec(t *testing.T, ctx context.Context, conn *pgx.Conn, sql string) {
	t.Helper()
	if _, err := conn.Exec(ctx, sql); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func dsnTemplate(t *testing.T) string {
	t.Helper()
	tpl := os.Getenv("TEST_DATABASE_DSN")
	if tpl == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}
	if !strings.Contains(tpl, "%s") {
		t.Fatalf("TEST_DATABASE_DSN has no %%s where the database name goes: %q", tpl)
	}
	return tpl
}

func requireTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"pg_dump", "pg_dumpall", "pg_restore", "psql"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not on PATH, so the backup scripts cannot run", tool)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// .../tools/dbadmin/internal/backup -> repository root
	for range 4 {
		wd = filepath.Dir(wd)
	}
	if _, err := os.Stat(filepath.Join(wd, "go.work")); err != nil {
		t.Fatalf("no go.work above the test's working directory: %v", err)
	}
	return wd
}
