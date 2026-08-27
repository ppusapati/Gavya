//go:build e2e

// A schema file must upgrade the version before it.
//
// This has now gone wrong three times in this repository, each time differently
// and each time silently:
//
//   - references.sql could not be applied twice: a DROP FUNCTION was refused by
//     a view built on it, and because the DROP failed everything after it in the
//     file was skipped.
//   - foreignkeys.sql had the same shape one file over, refused by two views in
//     references.sql.
//   - production-service added columns to production_formulations inside its
//     CREATE TABLE IF NOT EXISTS, which does nothing when the table exists. On a
//     fresh database the columns appeared; on any database that had ever run the
//     previous version they did not, and the service failed at the first insert.
//
// All three passed every test that applied the schema to an empty database,
// which is what every other schema test in this repository does.
//
// So this one does the other thing: it takes the committed version of each
// schema, applies it, then applies the working-tree version on top, and requires
// both to succeed. That is what a deployment does — it upgrades what is already
// there — and it is the only shape of test that catches this.
package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// TestEverySchemaUpgradesTheCommittedVersion applies HEAD's schema and then the
// working tree's, for every service.
//
// Skipped rather than failed when git is unavailable or a schema is new: a
// schema with no committed version has nothing to upgrade from, and saying so is
// better than inventing a baseline.
func TestEverySchemaUpgradesTheCommittedVersion(t *testing.T) {
	root := repoRoot(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH, so there is no committed version to upgrade from")
	}

	// schema.sql and the per-service isolation overrides, which are the files a
	// deployment applies. queries.sql holds statements the service runs, not a
	// schema, and applying one would fail for reasons that say nothing.
	schemas, err := filepath.Glob(filepath.Join(root, "services", "*", "internal", "db", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	overrides, err := filepath.Glob(filepath.Join(root, "services", "*", "internal", "db", "isolation.sql"))
	if err != nil {
		t.Fatal(err)
	}
	schemas = append(schemas, overrides...)
	shared, err := filepath.Glob(filepath.Join(root, "libs", "integrity", "isolation", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	schemas = append(schemas, shared...)
	if len(schemas) == 0 {
		t.Fatal("no schema files found; this check would pass against a repository with none")
	}

	checked := 0
	for _, path := range schemas {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		name := strings.NewReplacer("/", "_", ".", "_", "-", "_").Replace(rel)

		t.Run(rel, func(t *testing.T) {
			committed, err := gitShow(root, rel)
			if err != nil {
				t.Skipf("%s has no committed version to upgrade from: %v", rel, err)
			}
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			working := string(b)
			if committed == working {
				t.Skip("unchanged from the committed version")
			}
			checked++

			db := "e2e_upgrade_" + shortHash(name)
			ctx := context.Background()
			admin, err := pgx.Connect(ctx, dsn(t, "postgres"))
			if err != nil {
				t.Fatalf("connect as admin: %v", err)
			}
			defer admin.Close(ctx)

			if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+db+" WITH (FORCE)"); err != nil {
				t.Fatalf("drop %s: %v", db, err)
			}
			if _, err := admin.Exec(ctx, "CREATE DATABASE "+db); err != nil {
				t.Fatalf("create %s: %v", db, err)
			}
			defer func() {
				_, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+db+" WITH (FORCE)")
			}()

			conn, err := pgx.Connect(ctx, dsn(t, db))
			if err != nil {
				t.Fatalf("connect to %s: %v", db, err)
			}
			defer conn.Close(context.Background())

			// The isolation files depend on the service schemas being there, and
			// on each other in a fixed order. Applying the whole set first gives
			// each file the world it actually runs in.
			if err := applyAllCommitted(ctx, conn, root, rel); err != nil {
				t.Skipf("could not stand up the committed baseline for %s: %v", rel, err)
			}

			// The upgrade itself. This is the assertion.
			if _, err := conn.Exec(ctx, working); err != nil {
				t.Errorf("the working-tree version of %s does not apply on top of the "+
					"committed one:\n  %v\n\nA schema file must produce the same database "+
					"whether it is applied to an empty one or to the version before it. "+
					"CREATE TABLE IF NOT EXISTS does nothing when the table exists, so a "+
					"column added inside one never appears on a database that already ran "+
					"the previous version — and nothing says so until the first insert.",
					rel, err)
				return
			}

			// And again, because a deployment that is re-run must not break.
			if _, err := conn.Exec(ctx, working); err != nil {
				t.Errorf("%s applies once on top of the committed version and not twice: %v",
					rel, err)
			}
		})
	}

	if checked == 0 {
		t.Log("no schema differs from its committed version; nothing to upgrade")
	}
}

// applyAllCommitted stands up the committed state of every schema, so the file
// under test is applied to the world it would actually meet.
func applyAllCommitted(ctx context.Context, conn *pgx.Conn, root, target string) error {
	order, err := filepath.Glob(filepath.Join(root, "services", "*", "internal", "db", "schema.sql"))
	if err != nil {
		return err
	}
	// The isolation files come after the schemas they read, and in this order.
	for _, f := range []string{
		"libs/integrity/isolation/isolation.sql",
		"libs/integrity/isolation/foreignkeys.sql",
		"libs/integrity/isolation/references.sql",
	} {
		order = append(order, filepath.Join(root, f))
	}

	for _, path := range order {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sql, err := gitShow(root, rel)
		if err != nil {
			// A schema with no committed version is new; there is nothing of it
			// to stand up.
			continue
		}
		if _, err := conn.Exec(ctx, sql); err != nil {
			return fmt.Errorf("committed %s: %w", rel, err)
		}
	}
	return nil
}

func gitShow(root, rel string) (string, error) {
	cmd := exec.Command("git", "show", "HEAD:"+rel)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func shortHash(s string) string {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return fmt.Sprintf("%08x", h)
}
