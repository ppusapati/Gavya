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

	files := []string{filepath.Join(root, "pkg", "database", "schema", "gen_ulid_polyfill.sql")}
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
		if _, err := conn.Exec(ctx, string(sql)); err != nil {
			rel, _ := filepath.Rel(root, f)
			return fmt.Errorf("apply %s: %w", rel, err)
		}
	}
	fmt.Fprintf(os.Stderr, "provision: %s holds %d schemas\n", name, len(schemas))
	fmt.Println(dsn)
	return nil
}
