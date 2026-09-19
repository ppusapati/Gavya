//go:build dbintegration

// What the seed claims, checked against a real database.
//
// The claim is "test data in all the tables", and the only way that claim can be
// true is if something counts. A seed is exactly the kind of artefact that rots
// invisibly: a service adds a table, the seed does not, and the database a person
// demonstrates from has a gap in it that nobody notices until somebody clicks the
// one screen that reads it. Nothing else in this repository would fail.
//
// So the count is taken from the catalogue rather than from a list. A table that
// exists is a table this expects rows in, and a new one fails this test the day
// it is created rather than the day somebody looks.
//
//	TEST_DATABASE_DSN='postgres://user@host:port/%s?sslmode=disable' \
//	go test -count=1 -tags dbintegration ./tools/dbadmin/cmd/seed
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/ppusapati/gavya/tools/dbadmin/internal/schema"
)

// Every table the platform has holds something.
//
// The three that hold rows without this file having written them are named
// below, with why. Everything else is the seed's job.
func TestTheSeedFillsEveryTable(t *testing.T) {
	ctx := context.Background()
	dsn, conn := builtFrom(ctx, t, "seed_fills_every_table")

	if err := run(dsn, repoRoot(t), true); err != nil {
		t.Fatalf("seed: %v", err)
	}

	filled, empty, err := coverage(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) > 0 {
		t.Errorf("%d of %d tables are still empty after seeding:\n  %s\n\n"+
			"A demonstration database with a gap in it is one where the screen that "+
			"reads the missing table looks broken. Add rows to deploy/seed/seed.sql, "+
			"or say here why that table cannot have any.",
			len(empty), filled+len(empty), strings.Join(empty, "\n  "))
	}
	if filled < 100 {
		t.Errorf("only %d tables were counted, which is fewer than this platform has; "+
			"the count is reading the wrong database", filled)
	}
}

// The tables the seed does not write, and why that is right.
//
// Kept as a test rather than as a comment because a reason that is not checked
// stops being true: if applying the schema ever stops filling these, this says
// so instead of the seed silently taking over the job.
//
// gavya_schema_history is deliberately not among them. It does not exist in a
// database built from schema.Plan() at all — tools/dbadmin/cmd/migrate creates
// it and writes its own record of what it applied — so it is neither the seed's
// to fill nor the schema's, and a database that never ran the migration runner
// correctly has no such table.
func TestTheTablesTheSeedDoesNotWriteAreFilledByTheSchema(t *testing.T) {
	ctx := context.Background()
	_, conn := builtFrom(ctx, t, "seed_schema_owned")

	// No seed has run. These hold rows anyway, because applying the schema is
	// what fills them: each is a service's own record of its schema steps.
	for _, table := range []string{
		"billing_service.schema_migrations",
		"order_service.schema_migrations",
	} {
		var n int64
		if err := conn.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n == 0 {
			t.Errorf("%s is empty on a freshly built database. It used to be filled by "+
				"applying the schema, which is why deploy/seed/seed.sql does not write "+
				"it. If that changed, the seed has to.", table)
		}
	}

	var exists bool
	if err := conn.QueryRow(ctx,
		`SELECT to_regclass('public.gavya_schema_history') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Error("public.gavya_schema_history exists in a database built from schema.Plan(). " +
			"It used to be created only by the migration runner, which is why the seed " +
			"leaves it alone; if the schema creates it now, something has to fill it.")
	}
}

// Running it twice does what running it once did.
//
// Every schema file in this platform is re-runnable and the seed is held to the
// same rule, because the alternative is a file somebody has to think about
// before running — and a seed is run most often by somebody who is not thinking
// about it, on a database they have just rebuilt.
func TestTheSeedIsRerunnable(t *testing.T) {
	ctx := context.Background()
	dsn, conn := builtFrom(ctx, t, "seed_rerunnable")
	root := repoRoot(t)

	if err := run(dsn, root, true); err != nil {
		t.Fatalf("first run: %v", err)
	}
	first := census(ctx, t, conn)

	if err := run(dsn, root, true); err != nil {
		t.Fatalf("second run: %v", err)
	}
	second := census(ctx, t, conn)

	for table, n := range first {
		if second[table] != n {
			t.Errorf("%s held %d rows after one run and %d after two. The seed is not "+
				"re-runnable, so applying it to a database that already has it "+
				"duplicates data rather than leaving it alone.", table, n, second[table])
		}
	}
}

// It refuses unless somebody says the database is not one anybody relies on.
func TestItRefusesUntilItIsTold(t *testing.T) {
	ctx := context.Background()
	dsn, _ := builtFrom(ctx, t, "seed_refuses_untold")

	err := run(dsn, repoRoot(t), false)
	if err == nil {
		t.Fatal("the seed applied without -i-am-not-in-production. The flag is the " +
			"only thing standing between a pipeline with DATABASE_URL set and a " +
			"production database full of invented producers.")
	}
	if !strings.Contains(err.Error(), "i-am-not-in-production") {
		t.Errorf("the refusal does not name the flag that lifts it: %v", err)
	}
}

// And it refuses anyway when the database holds somebody else's tenant.
//
// The flag is a claim, and this is the part that checks. A database with a real
// tenant in it does not need seeding — it has data — so the only thing seeding
// can do to it is add rows nobody asked for.
func TestItRefusesADatabaseThatHasSomebodyInIt(t *testing.T) {
	ctx := context.Background()
	dsn, conn := builtFrom(ctx, t, "seed_refuses_stranger")

	if _, err := conn.Exec(ctx, `
		INSERT INTO tenant_service.tenants
			(id, name, slug, contact_email, currency, currency_scale, created_by, updated_by)
		VALUES ('TEN_A_REAL_CUSTOMER_00001', 'Anand Milk Union', 'anand',
			'office@anand.example', 'INR', 2, 'someone', 'someone')`); err != nil {
		t.Fatalf("plant a real tenant: %v", err)
	}

	err := run(dsn, repoRoot(t), true)
	if err == nil {
		t.Fatal("the seed applied to a database holding a tenant it does not own, " +
			"with the flag set. The flag is a claim about the database and claims " +
			"can be wrong; this is the half that looks.")
	}
	if !strings.Contains(err.Error(), "Anand Milk Union") {
		t.Errorf("the refusal does not name whose data it found, which is the one "+
			"thing the person reading it needs: %v", err)
	}
}

// The coverage count notices a table with nothing in it.
//
// Without this, TestTheSeedFillsEveryTable is satisfied by a coverage function
// that returns no empty tables whatever the database holds — and it would go on
// passing while the seed quietly stopped covering half the platform. The same
// function both tests use is the point: this is the one that proves it can say
// no.
func TestTheCoverageCountNoticesAnEmptyTable(t *testing.T) {
	ctx := context.Background()
	_, conn := builtFrom(ctx, t, "seed_coverage_notices")

	if _, err := conn.Exec(ctx,
		`CREATE TABLE public.a_table_nobody_filled (id text primary key)`); err != nil {
		t.Fatal(err)
	}

	_, empty, err := coverage(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(empty, "public.a_table_nobody_filled") {
		t.Errorf("an empty table was not reported as empty; coverage returned %v.\n"+
			"Every other test here rests on this function being able to say no.", empty)
	}
}

// The tenants the command owns are the tenants the file writes.
//
// The list in main.go is what the stranger check compares against, and it is
// hand-written there on purpose — a parser that got it wrong would widen the
// refusal silently. This is the check that keeps the hand-written copy true.
func TestTheOwnedTenantsAreTheOnesTheFileWrites(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoRoot(t), File))
	if err != nil {
		t.Fatal(err)
	}

	written := map[string]bool{}
	for _, m := range regexp.MustCompile(`'(TEN_[A-Z0-9_]+)'`).FindAllStringSubmatch(string(body), -1) {
		written[m[1]] = true
	}
	if len(written) == 0 {
		t.Fatal("found no tenant identifiers in the seed; this test is reading it wrongly")
	}

	owned := map[string]bool{}
	for _, id := range tenants {
		owned[id] = true
		if !written[id] {
			t.Errorf("main.go owns %q and the seed never writes it. The stranger check "+
				"would then let a tenant of that name through.", id)
		}
	}
	for id := range written {
		if !owned[id] {
			t.Errorf("the seed writes tenant %q and main.go does not own it, so seeding "+
				"a database twice would refuse on the seed's own data.", id)
		}
	}
}

// census is every table's row count, for comparing one run against the next.
func census(ctx context.Context, t *testing.T, conn *pgx.Conn) map[string]int64 {
	t.Helper()
	rows, err := conn.Query(ctx, `
		SELECT n.nspname || '.' || c.relname
		  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE c.relkind = 'r' AND n.nspname NOT IN ('pg_catalog', 'information_schema')`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		names = append(names, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	out := make(map[string]int64, len(names))
	for _, name := range names {
		var n int64
		if err := conn.QueryRow(ctx, "SELECT count(*) FROM "+name).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", name, err)
		}
		out[name] = n
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// builtFrom makes a database of its own and applies the whole schema to it,
// returning its DSN and an open connection.
//
// A database per test rather than the shared one the other dbintegration suites
// read: this one counts every row in the database, so anything another suite
// left behind would be counted as the seed's work.
func builtFrom(ctx context.Context, t *testing.T, name string) (string, *pgx.Conn) {
	t.Helper()
	dsnFor := dsnTemplate(t)
	root := repoRoot(t)

	admin, err := pgx.Connect(ctx, fmt.Sprintf(dsnFor, "postgres"))
	if err != nil {
		t.Fatalf("connect to the maintenance database: %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()

	// gavya_grant_app_access grants to a role that has to exist.
	if _, err := admin.Exec(ctx, `
		DO $$ BEGIN
			IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'gavya_app') THEN
				CREATE ROLE gavya_app LOGIN;
			END IF;
		END $$;`); err != nil {
		t.Fatalf("ensure the application role: %v", err)
	}
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
		t.Fatalf("drop %s: %v", name, err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}

	dsn := fmt.Sprintf(dsnFor, name)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to %s: %v", name, err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_ = conn.Close(bg)
		dropper, err := pgx.Connect(bg, fmt.Sprintf(dsnFor, "postgres"))
		if err != nil {
			return
		}
		defer func() { _ = dropper.Close(bg) }()
		_, _ = dropper.Exec(bg, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})

	for _, step := range schema.Plan() {
		files, err := step.Files(root)
		if err != nil {
			t.Fatalf("%s: %v", step.Describe, err)
		}
		for _, f := range files {
			sql, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(ctx, step.Prelude(f)+string(sql)); err != nil {
				rel, _ := filepath.Rel(root, f)
				t.Fatalf("apply %s: %v", rel, err)
			}
		}
		if step.Command != "" {
			if _, err := conn.Exec(ctx, step.Command); err != nil {
				t.Fatalf("%s: %v", step.Describe, err)
			}
		}
	}
	return dsn, conn
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

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Skip("cannot find the repository root from here")
	return ""
}
