//go:build e2e

// A database that has lived through every version of a schema is the same
// database as one built fresh from the current version.
//
// migration_test.go checks that the working-tree schema applies on top of the
// committed one. That catches a statement that errors. It does not catch the
// case that has actually happened here: a statement that succeeds and does
// nothing — CREATE TABLE IF NOT EXISTS on a table that exists, which quietly
// skips every column and constraint added inside it. And it only runs while the
// working tree differs from HEAD; the moment the change is committed every
// schema is "unchanged" and the upgrade path is never looked at again.
//
// So this does the other half. For every service it builds two databases: one
// from the current schema alone, and one by applying every committed version of
// that schema in order, oldest first, then the current one — the life of a
// database that was deployed on the first version and upgraded at each release.
// Then it compares the two at the catalogue: every column and its type, every
// constraint by its definition, every index, every function, every trigger.
//
// The first run of this found four services whose upgraded database differed
// from a fresh one, every one of them having passed the apply-on-top test:
// six CHECK constraints missing from settlement's producer_payables, two from
// procurement's priced_collections, a column left behind on production's
// batches, and tenants.currency as varchar on one and char on the other.
package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestADatabaseThatLivedThroughEveryVersionMatchesAFreshOne(t *testing.T) {
	root := repoRoot(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH, so there is no history to live through")
	}

	schemas, err := filepath.Glob(filepath.Join(root, "services", "*-service", "internal", "db", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if len(schemas) < 20 {
		t.Fatalf("found only %d schemas; the glob has probably stopped matching, "+
			"and a check that finds nothing passes", len(schemas))
	}

	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn(t, "postgres"))
	if err != nil {
		t.Fatalf("connect as admin: %v", err)
	}
	defer admin.Close(context.Background())

	polyfill, err := os.ReadFile(filepath.Join(root, "pkg", "database", "schema", "gen_ulid_polyfill.sql"))
	if err != nil {
		t.Fatalf("read the ULID polyfill, which deployment applies before any schema: %v", err)
	}

	withHistory := 0
	for _, path := range schemas {
		rel, _ := filepath.Rel(root, path)
		svc := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(path))))
		t.Run(svc, func(t *testing.T) {
			versions := committedVersions(t, root, rel)
			if len(versions) < 2 {
				// One committed version and the working tree is not a life, it
				// is a birth. Still checked — the working tree may differ from
				// that one commit — but said plainly.
				t.Logf("%s has %d committed version(s); this compares fresh against that one", svc, len(versions))
			} else {
				withHistory++
			}
			current, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			fresh := buildDatabase(t, ctx, admin, "e2e_fresh_"+shortHash(svc), string(polyfill), [][]byte{current})
			aged := buildDatabase(t, ctx, admin, "e2e_aged_"+shortHash(svc), string(polyfill), append(versions, current))

			if diff := catalogueDiff(fresh, aged); diff != "" {
				t.Errorf("a database that lived through every version of %s differs from one "+
					"built fresh:\n%s\n"+
					"Every version applied without error, which is why nothing said so. A "+
					"column or constraint added inside CREATE TABLE IF NOT EXISTS never "+
					"reaches a table that already exists; it needs an ALTER guarded on its "+
					"own absence, beside the CREATE. A column removed from the CREATE needs "+
					"a DROP COLUMN IF EXISTS, or every upgraded database keeps it.",
					svc, diff)
			}
		})
	}
	t.Logf("%d schemas checked, %d with more than one committed version", len(schemas), withHistory)
}

// committedVersions is every committed version of one file, oldest first.
func committedVersions(t *testing.T, root, rel string) [][]byte {
	t.Helper()
	log := exec.Command("git", "log", "--reverse", "--format=%H", "--follow", "--", rel)
	log.Dir = root
	out, err := log.Output()
	if err != nil {
		t.Fatalf("git log %s: %v", rel, err)
	}
	var versions [][]byte
	for _, hash := range strings.Fields(string(out)) {
		show := exec.Command("git", "show", hash+":"+rel)
		show.Dir = root
		src, err := show.Output()
		if err != nil {
			// The file moved or did not exist at this commit under this path;
			// --follow can name commits from before a rename.
			continue
		}
		versions = append(versions, src)
	}
	return versions
}

// buildDatabase creates a database and applies the polyfill and then each
// schema in order, returning its catalogue.
//
// A version that fails to apply is reported and skipped rather than fatal: an
// old version may have depended on something that no longer exists, and what
// matters is the state the database ends in. But every failure is logged, so a
// history that never applied at all cannot pass as an upgrade that did.
func buildDatabase(t *testing.T, ctx context.Context, admin *pgx.Conn, name, polyfill string, schemas [][]byte) []string {
	t.Helper()
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
		t.Fatalf("drop %s: %v", name, err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})

	conn, err := pgx.Connect(ctx, dsn(t, name))
	if err != nil {
		t.Fatalf("connect to %s: %v", name, err)
	}
	defer conn.Close(context.Background())

	if _, err := conn.Exec(ctx, polyfill); err != nil {
		t.Fatalf("apply the ULID polyfill to %s: %v", name, err)
	}
	applied := 0
	for i, sql := range schemas {
		if _, err := conn.Exec(ctx, string(sql)); err != nil {
			t.Logf("%s: version %d of %d did not apply and was skipped: %v", name, i+1, len(schemas), err)
			continue
		}
		applied++
	}
	if applied == 0 {
		t.Fatalf("%s: no version applied at all, so there is nothing to compare", name)
	}
	// The current version is the last one, and it has to have applied: a
	// database whose latest upgrade failed is not an upgraded database.
	if _, err := conn.Exec(ctx, string(schemas[len(schemas)-1])); err != nil {
		t.Fatalf("%s: the current schema does not apply: %v", name, err)
	}
	return catalogue(t, ctx, conn)
}

// catalogue is everything about a database's structure that a service can
// observe, one line per fact, sorted.
//
// Constraints are compared by definition and indexes with their names removed:
// an upgrade path that adds the same rule under a different name has produced
// the same database, and a check that failed on the name would send somebody to
// rename things that behave identically. Columns carry type, length, precision,
// nullability and default, because CHAR(3) and VARCHAR(3) compare differently
// and a default that is present on one and not the other is a row that inserts
// on one and not the other.
func catalogue(t *testing.T, ctx context.Context, conn *pgx.Conn) []string {
	t.Helper()
	rows, err := conn.Query(ctx, `
		SELECT 'column   ' || table_name || '.' || column_name || ' ' || data_type
		       || ' len=' || COALESCE(character_maximum_length::text, '-')
		       || ' prec=' || COALESCE(numeric_precision::text, '-') || ',' || COALESCE(numeric_scale::text, '-')
		       || ' null=' || is_nullable
		       || ' default=' || COALESCE(column_default, '-')
		  FROM information_schema.columns WHERE table_schema NOT IN ('pg_catalog', 'information_schema') AND table_schema NOT LIKE 'pg\\_%'
		UNION ALL
		SELECT 'constraint ' || conrelid::regclass::text || ' ' || pg_get_constraintdef(oid)
		  FROM pg_constraint WHERE connamespace = 'public'::regnamespace
		UNION ALL
		SELECT 'index    ' || tablename || ' ' || regexp_replace(indexdef, 'INDEX \S+ ON', 'INDEX ? ON')
		  FROM pg_indexes WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
		UNION ALL
		SELECT 'function ' || proname || '(' || pg_get_function_identity_arguments(oid) || ') secdef=' || prosecdef
		  FROM pg_proc WHERE pronamespace = 'public'::regnamespace
		UNION ALL
		SELECT 'trigger  ' || tgrelid::regclass::text || ' ' || tgname
		  FROM pg_trigger WHERE NOT tgisinternal
		UNION ALL
		SELECT 'view     ' || table_name
		  FROM information_schema.views WHERE table_schema NOT IN ('pg_catalog', 'information_schema') AND table_schema NOT LIKE 'pg\\_%'`)
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
	sort.Strings(facts)
	return facts
}

// catalogueDiff says what fresh has that aged lacks, and the reverse.
func catalogueDiff(fresh, aged []string) string {
	in := func(list []string) map[string]bool {
		m := make(map[string]bool, len(list))
		for _, s := range list {
			m[s] = true
		}
		return m
	}
	f, a := in(fresh), in(aged)
	var b strings.Builder
	for _, s := range fresh {
		if !a[s] {
			fmt.Fprintf(&b, "  fresh has, upgraded lacks: %s\n", s)
		}
	}
	for _, s := range aged {
		if !f[s] {
			fmt.Fprintf(&b, "  upgraded has, fresh lacks: %s\n", s)
		}
	}
	return b.String()
}
