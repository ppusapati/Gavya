//go:build dbintegration

// What reference enforcement does when a table is not where it used to be.
//
// gavya_enforce_references took a schema and defaulted to 'public', and the
// deployment called it with no argument. Everything else in the isolation layer
// already sweeps every non-system schema — the policies, the grants, the
// unconstrained-reference report — and this was the one piece that did not.
//
// That mattered twice over. It already meant the five reference-shaped columns
// on the stub tables in `masters` could not be enforced, which the decisions in
// references.sql say in as many words. And it was about to mean much more: giving
// each service a schema of its own moves twenty-seven services' tables out of
// public, and this would then have enforced nothing for any of them — without
// failing, because it answers 'skipped: ... does not exist here' and the
// deployment only reads REFUSED. Every declared reference in the platform would
// have quietly stopped being a key.
//
// It now finds each decision's table wherever it lives. These are the two
// branches that creates, neither of which anything else exercises: a table that
// exists in two schemas, and a table whose target does not travel with it.
package schema_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/ppusapati/gavya/tools/dbadmin/internal/schema"
)

// A table in two schemas has no one place a reference belongs.
//
// Refused rather than guessed at: enforcing on whichever copy the catalogue
// returned first gives one of the two a constraint the other does not have, and
// which one depends on an ordering nobody chose. It is the same collision
// tools/dbadmin's coexist check reports, seen from the other end.
func TestAReferenceOnATableInTwoSchemasIsRefused(t *testing.T) {
	ctx := context.Background()
	conn := built(ctx, t, "refs_twin")

	// A second cattle_ownership, in a schema of its own. LIKE copies the columns
	// and nothing else, which is enough: the decision is looked up by name.
	if _, err := conn.Exec(ctx, `
		CREATE SCHEMA twin;
		CREATE TABLE twin.cattle_ownership (LIKE public.cattle_ownership);`); err != nil {
		t.Fatalf("make a second cattle_ownership: %v", err)
	}

	outcome := enforcementFor(ctx, t, conn, "cattle_ownership_cattle_id_fkey")
	if !strings.HasPrefix(outcome, "REFUSED") {
		t.Fatalf("with cattle_ownership in two schemas, enforcement said %q — it has to "+
			"refuse, or one of the two copies silently gets a key the other does not",
			outcome)
	}
	for _, name := range []string{"public", "twin"} {
		if !strings.Contains(outcome, name) {
			t.Errorf("the refusal does not name %s, so it does not say where to look: %q",
				name, outcome)
		}
	}
}

// A table that moved and a target that did not.
//
// This is what a half-finished namespace move looks like, and the honest answer
// is to skip it and say so rather than reach across into another schema. A
// foreign key between two services' namespaces is a coupling nobody declared,
// and adding it here would make it permanent before anyone had noticed.
func TestAReferenceWhoseTargetDidNotTravelIsSkipped(t *testing.T) {
	ctx := context.Background()
	conn := built(ctx, t, "refs_apart")

	// cattle_ownership moves; cattle stays. The key that was there has to go
	// with it, or the move itself would be refused.
	if _, err := conn.Exec(ctx, `
		CREATE SCHEMA solo;
		ALTER TABLE public.cattle_ownership DROP CONSTRAINT IF EXISTS cattle_ownership_cattle_id_fkey;
		ALTER TABLE public.cattle_ownership SET SCHEMA solo;`); err != nil {
		t.Fatalf("move cattle_ownership: %v", err)
	}

	outcome := enforcementFor(ctx, t, conn, "cattle_ownership_cattle_id_fkey")
	if !strings.HasPrefix(outcome, "skipped") {
		t.Fatalf("with cattle_ownership in solo and cattle still in public, enforcement "+
			"said %q — it has to skip rather than reach across schemas", outcome)
	}
	if !strings.Contains(outcome, "solo") || !strings.Contains(outcome, "cattle") {
		t.Errorf("the skip does not say which table in which schema could not be "+
			"satisfied: %q", outcome)
	}
}

// And the ordinary case, which is what the other two are measured against: every
// decision marked enforce finds its table and is already a key by the time the
// plan has finished.
func TestEveryDecisionIsEnforcedOnAnUntouchedDatabase(t *testing.T) {
	ctx := context.Background()
	conn := built(ctx, t, "refs_plain")

	rows, err := conn.Query(ctx, `
		SELECT constraint_name, outcome FROM gavya_enforce_references()
		 WHERE outcome NOT LIKE 'already%' AND outcome NOT LIKE 'now%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, outcome string
		if err := rows.Scan(&name, &outcome); err != nil {
			t.Fatal(err)
		}
		t.Errorf("%s: %s", name, outcome)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	var enforced, keys int
	if err := conn.QueryRow(ctx,
		`SELECT count(*) FROM gavya_reference_decisions() WHERE enforce`).Scan(&enforced); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRow(ctx, `
		SELECT count(*) FROM gavya_reference_decisions() d
		 WHERE d.enforce AND EXISTS (
		     SELECT 1 FROM pg_constraint k
		       JOIN pg_class c ON c.oid = k.conrelid
		       JOIN pg_attribute a ON a.attrelid = c.oid AND a.attname = d.column_name
		      WHERE k.contype = 'f' AND c.relname = d.table_name
		        AND a.attnum = ANY (k.conkey))`).Scan(&keys); err != nil {
		t.Fatal(err)
	}
	if keys != enforced {
		t.Errorf("%d decisions are marked enforce and %d of them are keys; the list "+
			"claims a guarantee the database does not make", enforced, keys)
	}
	if enforced == 0 {
		t.Fatal("no decision is marked enforce, so the two tests above are comparing " +
			"against nothing")
	}
}

// enforcementFor runs the enforcement and returns what it said about one
// constraint.
func enforcementFor(ctx context.Context, t *testing.T, conn *pgx.Conn, name string) string {
	t.Helper()
	var outcome string
	err := conn.QueryRow(ctx,
		`SELECT outcome FROM gavya_enforce_references() WHERE constraint_name = $1`,
		name).Scan(&outcome)
	if err != nil {
		t.Fatalf("enforcement said nothing about %s: %v", name, err)
	}
	return outcome
}

// built runs the whole plan against a database of its own.
//
// schema.Plan() rather than a list written here: it is the sequence the
// deployment performs, and a second copy of an order this particular would not
// stay in step with it.
func built(ctx context.Context, t *testing.T, name string) *pgx.Conn {
	t.Helper()
	dsnFor := dsnTemplate(t)
	root := repoRoot(t)

	admin, err := pgx.Connect(ctx, fmt.Sprintf(dsnFor, "postgres"))
	if err != nil {
		t.Fatalf("connect to the maintenance database: %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()

	// gavya_grant_app_access grants to a role that has to exist. Cluster-wide
	// and idempotent; the deployment's init script creates it before any of
	// this runs.
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

	conn, err := pgx.Connect(ctx, fmt.Sprintf(dsnFor, name))
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
			if _, err := conn.Exec(ctx, string(sql)); err != nil {
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
	return conn
}
