// Command provision builds one database holding every service's schema, for
// the repository suites that expect to find one.
//
// Six suites carry the dbintegration build tag and read TEST_DATABASE_URL — a
// plain DSN to a database that already has the tables. They apply nothing
// themselves. scripts/check-all.sh never set that variable or passed that tag,
// so from the day they were written nothing ran them; the untagged suites beside
// them build their own database from TEST_DATABASE_DSN and were run all along.
//
// This is the missing half: given the same DSN template the e2e harness uses,
// it drops and recreates one database, applies the ULID polyfill and every
// schema.sql in the repository — the same set, in the same order, that
// e2e/isolation_test.go and deploy/postgres-init apply — and prints the DSN to
// hand to the suites.
//
// Every schema, not just the six services', because audit_logs lives in
// audit-service's schema and every service writes its trail there; a suite run
// against a database with only its own tables fails at the first audited write,
// which is the shape of failure this repository keeps finding.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

func main() {
	template := flag.String("dsn", os.Getenv("TEST_DATABASE_DSN"),
		"DSN template with a single %s where the database name goes")
	name := flag.String("name", "dbintegration", "database to create")
	root := flag.String("root", ".", "repository root")
	flag.Parse()

	if err := run(*template, *name, *root); err != nil {
		fmt.Fprintln(os.Stderr, "provision:", err)
		os.Exit(1)
	}
}

// prelude puts the connection in the schema this file's tables belong to.
//
// A copy of tools/dbadmin's Step.Prelude rather than an import: this command is
// in the e2e module and exists to build the database those suites read, and a
// dependency from e2e on the migration tool would tie the suite's ability to run
// to that tool's build. The rule it copies is one line long and is checked
// against the original by TestProvisionAgreesWithThePlan.
func prelude(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i, p := range parts {
		if p == "services" && i+1 < len(parts) {
			service := parts[i+1]
			// audit-service's one table is the shared audit trail, written by
			// twenty-five services under an unqualified name. It stays in public.
			if service == "audit-service" {
				break
			}
			ns := strings.ReplaceAll(service, "-", "_")
			return fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s; SET search_path = %s, public;", ns, ns)
		}
	}
	return "SET search_path = public;"
}

func run(template, name, root string) error {
	if !strings.Contains(template, "%s") {
		return fmt.Errorf("the DSN template has no %%s in it: %q", template)
	}
	ctx := context.Background()

	admin, err := pgx.Connect(ctx, fmt.Sprintf(template, "postgres"))
	if err != nil {
		return fmt.Errorf("connect to the maintenance database: %w", err)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
		return fmt.Errorf("drop %s: %w", name, err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}

	dsn := fmt.Sprintf(template, name)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", name, err)
	}
	defer conn.Close(ctx)

	polyfill := filepath.Join(root, "pkg", "database", "schema", "gen_ulid_polyfill.sql")
	files := []string{polyfill}
	schemas, err := filepath.Glob(filepath.Join(root, "services", "*", "internal", "db", "schema.sql"))
	if err != nil {
		return err
	}
	if len(schemas) < 20 {
		return fmt.Errorf("found only %d schemas under %s; is this the repository root?", len(schemas), root)
	}
	sort.Strings(schemas)
	files = append(files, schemas...)

	for _, f := range files {
		sql, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		// Each service's tables into that service's own schema, the way both
		// deployments now build it. Without this the suites below would run
		// against one flat namespace and would be the only arrangement in the
		// platform where two services can still collide.
		if _, err := conn.Exec(ctx, prelude(f)); err != nil {
			rel, _ := filepath.Rel(root, f)
			return fmt.Errorf("prepare the schema for %s: %w", rel, err)
		}
		if _, err := conn.Exec(ctx, string(sql)); err != nil {
			rel, _ := filepath.Rel(root, f)
			return fmt.Errorf("apply %s: %w", rel, err)
		}
	}
	fmt.Fprintf(os.Stderr, "provision: %s holds %d schemas\n", name, len(schemas))
	fmt.Println(dsn)
	return nil
}
