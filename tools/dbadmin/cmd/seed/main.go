// Command seed fills an empty database with one co-operative's worth of data.
//
// The SQL is deploy/seed/seed.sql and a person can apply it with psql. This
// command exists for the two refusals, which psql cannot make.
//
//	seed -dsn "postgres://postgres@localhost:5432/dairy?sslmode=disable" \
//	     -i-am-not-in-production
//
// # WHY THE FLAG IS SPELLED LIKE THAT
//
// The flag is not a confirmation, it is a sentence somebody has to write down.
// -force and -yes are typed without reading; a flag that names the claim being
// made is harder to type by accident and much harder to defend afterwards.
//
// The risk this guards is not a careless operator. It is a script: a seed step
// added to a deployment pipeline "for the staging environment" and run against
// whatever DATABASE_URL happens to be set. That is how test data reaches
// production, and it reaches it silently, because inserting rows breaks nothing.
//
// # AND WHY IT REFUSES ANYWAY WHEN IT FINDS A STRANGER
//
// The flag is a claim about the database, and claims can be wrong. So the
// command also looks: if the database holds a tenant that is not one of the two
// this seed owns, it stops and names it, whatever the flag says.
//
// A database with a real tenant in it is not one anybody needs seeded — it has
// data. The only thing seeding it can do is add rows nobody asked for to a
// system somebody is relying on, and the flag would have been typed by then.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

// tenants is what this seed owns. Anything else in the database is somebody
// else's, and its presence is what the second refusal is about.
//
// It is here rather than parsed out of the SQL because a parser that got it
// wrong would widen the refusal silently, and the refusal is the whole point of
// this command. The test beside this package checks the list against the file.
var tenants = []string{
	"TEN_VALLEY_DAIRY_000000001",
	"TEN_HILL_CREAMERY_00000001",
}

// File is the seed, relative to the repository root.
const File = "deploy/seed/seed.sql"

func main() {
	dsn := flag.String("dsn", os.Getenv("DATABASE_URL"),
		"the database to seed")
	root := flag.String("root", ".", "repository root")
	confirmed := flag.Bool("i-am-not-in-production", false,
		"state plainly that this database is not one anybody is relying on")
	flag.Parse()

	if err := run(*dsn, *root, *confirmed); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		var r *refusal
		if errors.As(err, &r) {
			fmt.Fprintln(os.Stderr, r.detail)
		}
		os.Exit(1)
	}
}

// A refusal is a reason this command will not do what it was asked, with the
// paragraph a person needs beside it.
//
// Two fields because an error value and a message to a person are not the same
// thing. A Go error string is short and lowercase so that it reads correctly
// when something else wraps it; the three sentences explaining that seeding a
// production database cannot be undone belong on somebody's terminal, where they
// are read once and acted on.
type refusal struct {
	reason string
	detail string
}

func (r *refusal) Error() string { return r.reason }

func run(dsn, root string, confirmed bool) error {
	if strings.TrimSpace(dsn) == "" {
		return errors.New("no database given: pass -dsn or set DATABASE_URL")
	}
	if !confirmed {
		return &refusal{
			reason: "refusing without -i-am-not-in-production",
			detail: "This writes a made-up dairy into every table in the database. On a real\n" +
				"deployment that is unrecoverable without a restore, because the rows are\n" +
				"valid and indistinguishable from data somebody entered.",
		}
	}

	sql, err := os.ReadFile(filepath.Join(root, File))
	if err != nil {
		return fmt.Errorf("read %s: %w", File, err)
	}

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close(context.Background())

	strangers, err := strangeTenants(ctx, conn)
	if err != nil {
		return err
	}
	if len(strangers) > 0 {
		return &refusal{
			reason: fmt.Sprintf("this database already holds %d tenant(s) this seed does not own: %s",
				len(strangers), strings.Join(strangers, ", ")),
			detail: "Refusing whatever the flag says. A database with tenants in it has data,\n" +
				"and the only thing seeding can do to it is add rows nobody asked for.",
		}
	}

	// One transaction, so a seed that fails halfway leaves nothing behind. The
	// file opens with BEGIN and closes with COMMIT, and pgx sends it as one
	// simple-protocol batch, which is what lets SET LOCAL apply across the
	// statements that follow it.
	if _, err := conn.Exec(ctx, string(sql)); err != nil {
		return fmt.Errorf("apply %s: %w", File, err)
	}

	filled, empty, err := coverage(ctx, conn)
	if err != nil {
		return err
	}
	fmt.Printf("seed: applied %s\n", File)
	fmt.Printf("seed: %d of %d tables hold rows\n", filled, filled+len(empty))
	for _, t := range empty {
		fmt.Printf("seed:   still empty: %s\n", t)
	}
	return nil
}

// strangeTenants is every tenant in the database that this seed did not write.
//
// Read from tenant_service.tenants rather than from a marker table, because a
// marker is something the seed controls and this question is about everything
// else. A database with no tenants at all answers this with nothing, which is
// the empty database the seed is for.
func strangeTenants(ctx context.Context, conn *pgx.Conn) ([]string, error) {
	// No tenants table means no schema has been applied, and there is therefore
	// nobody in this database to protect. The apply below will report the missing
	// schema more clearly than a wrapped error from here would.
	var applied bool
	if err := conn.QueryRow(ctx,
		`SELECT to_regclass('tenant_service.tenants') IS NOT NULL`).Scan(&applied); err != nil {
		return nil, fmt.Errorf("look for the tenants table: %w", err)
	}
	if !applied {
		return nil, nil
	}

	rows, err := conn.Query(ctx,
		`SELECT id, name FROM tenant_service.tenants WHERE id <> ALL($1)`, tenants)
	if err != nil {
		return nil, fmt.Errorf("read the tenants: %w", err)
	}
	defer rows.Close()

	var found []string
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		found = append(found, fmt.Sprintf("%s (%s)", name, id))
	}
	sort.Strings(found)
	return found, rows.Err()
}

// coverage counts the tables that hold rows, and names the ones that do not.
//
// Printed rather than enforced: this command is for a person at a terminal, and
// the check that fails a build lives in the test beside the seed, where it can
// say which table and why it matters.
func coverage(ctx context.Context, conn *pgx.Conn) (int, []string, error) {
	rows, err := conn.Query(ctx, `
		SELECT n.nspname, c.relname
		  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE c.relkind = 'r'
		   AND n.nspname NOT IN ('pg_catalog', 'information_schema')
		 ORDER BY 1, 2`)
	if err != nil {
		return 0, nil, err
	}
	type table struct{ schema, name string }
	var all []table
	for rows.Next() {
		var t table
		if err := rows.Scan(&t.schema, &t.name); err != nil {
			rows.Close()
			return 0, nil, err
		}
		all = append(all, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}

	filled := 0
	var empty []string
	for _, t := range all {
		var n int64
		q := fmt.Sprintf(`SELECT count(*) FROM %s.%s`,
			pgx.Identifier{t.schema}.Sanitize(), pgx.Identifier{t.name}.Sanitize())
		if err := conn.QueryRow(ctx, q).Scan(&n); err != nil {
			return 0, nil, fmt.Errorf("count %s.%s: %w", t.schema, t.name, err)
		}
		if n > 0 {
			filled++
			continue
		}
		empty = append(empty, t.schema+"."+t.name)
	}
	return filled, empty, nil
}
