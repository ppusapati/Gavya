//go:build dbintegration

// Whether the twenty-eight schemas actually fit in one database.
//
// Both deployments put them there. Every service in docker-compose.yaml gets
// DATABASE_URL pointing at the same `dairy` database, the modulith gets one
// DATABASE_URL for all twenty-eight of its modules, and deploy/postgres-init
// applies every services/*/internal/db/schema.sql into it in a loop.
//
// Nothing checked that they can share it, and two things together made the
// failure silent.
//
// The schemas are written with CREATE TABLE IF NOT EXISTS, because they have to
// be re-runnable. So when two services define a table of the same name, the
// second one's definition is not an error — it is skipped. The service goes on
// believing in columns that were never created, and finds out at its first
// query.
//
// And the end-to-end suite gives every service a database of its own —
// e2e_order, e2e_billing, e2e_ingestion — which is right for isolating a test
// and means the suite runs in the one arrangement where the collision cannot
// happen. Every test passes. The deployment is broken.
//
// # HOW IT ASKS
//
// Not by parsing SQL. A schema file is not only its CREATE TABLE statements —
// several add columns further down, and a reader that stopped at the create
// would report columns missing that are there. PostgreSQL is the parser: each
// schema is applied alone to a database of its own and the catalogue read back,
// then all of them are applied to one database in the same order the deployment
// uses, and the two are compared.
//
// What a service gets alone is what it was written against. Anything it loses
// when it shares is a column its queries name and the database does not have.
package schema_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// knownCollisions are the ones this check found when it was written.
//
// They are here so the check can be green for everything else — a new collision
// introduced next month is what it is for, and it cannot report one from inside
// a build that is already red. Each entry says what it costs, because a list of
// known breakage with no consequence beside it is a list nobody empties.
//
// THIS MAP HAS TO REACH ZERO. It is not a set of accepted differences; it is one
// defect, written down, with a fix that is a decision rather than a typo — see
// the invoices entry.
var knownCollisions = map[string]string{
	"order-service invoices": "" +
		"billing-service and order-service both define a table called invoices and " +
		"they are not the same table. billing's has customer_id, reference_id and " +
		"reference_type; order's has order_id NOT NULL REFERENCES orders(id). " +
		"billing sorts first in the glob deploy/postgres-init loops over, so " +
		"billing's table is the one that exists and order's CREATE TABLE IF NOT " +
		"EXISTS is skipped in silence.\n" +
		"What it costs: every write order-service makes to an invoice fails with " +
		"\"column order_id of relation invoices does not exist\". Its invoicing has " +
		"never worked in either deployed shape. Nothing caught it because the " +
		"end-to-end suite gives each service its own database.\n" +
		"Fixing it is a decision, not a rename: either the two services get a " +
		"namespace each, or somebody decides whether billing and order share " +
		"invoicing at all.",
}

// A table defined the same way twice is not a collision. tenant_currency is
// declared identically by five services and schema_migrations by two, and both
// are conventions rather than shared data — every one of those services gets
// exactly the table it expects.
func TestEverySchemaStillFitsWhenTheyShareOneDatabase(t *testing.T) {
	dsnFor := dsnTemplate(t)
	root := repoRoot(t)
	ctx := context.Background()

	admin, err := pgx.Connect(ctx, fmt.Sprintf(dsnFor, "postgres"))
	if err != nil {
		t.Fatalf("connect to the maintenance database: %v", err)
	}
	// Registered first so it runs last: the drops below need it open, and
	// cleanups run in reverse.
	t.Cleanup(func() { _ = admin.Close(ctx) })

	polyfill := filepath.Join(root, "pkg", "database", "schema", "gen_ulid_polyfill.sql")
	schemas, err := filepath.Glob(filepath.Join(root, "services", "*", "internal", "db", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if len(schemas) < 20 {
		t.Fatalf("found %d schemas under %s; there are twenty-eight, so this is not "+
			"reading the repository", len(schemas), root)
	}
	// The same order deploy/postgres-init applies them in. It matters: which of
	// two colliding definitions wins is decided by it.
	sort.Strings(schemas)

	// What each service gets when it has a database to itself. This is what it
	// was written against and what its queries assume.
	alone := map[string]map[string]bool{}
	for _, schema := range schemas {
		service := serviceOf(root, schema)
		cols := columnsAfterApplying(ctx, t, admin, dsnFor,
			"coexist_"+strings.ReplaceAll(service, "-", "_"), polyfill, schema)
		alone[service] = cols
	}

	// And what they get when they share one, which is what both deployments
	// build.
	shared := columnsAfterApplying(ctx, t, admin, dsnFor, "coexist_shared", polyfill, schemas...)

	// The comparison, and the report.
	//
	// whatIsLost is separate from everything above so that it can be tested
	// against catalogues somebody made up, which is the only thing that shows it
	// can find anything at all. Without that there was nothing checking the
	// checker: a version that reported no losses passed this test — the losses
	// it should have found were all explained by knownCollisions — and passed the
	// second test too, because that one did its own comparison. Both green, and
	// the check doing nothing.
	lost := whatIsLost(alone, shared)

	unexplained := 0
	for _, service := range sortedKeys(lost) {
		for _, table := range sortedKeys(lost[service]) {
			columns := lost[service][table]
			key := service + " " + table
			if why, known := knownCollisions[key]; known {
				t.Logf("KNOWN: %s loses %v from %s.\n%s", service, columns, table, why)
				continue
			}
			unexplained++
			t.Errorf("%s defines a table %q with columns %v that are not in the "+
				"database the deployment actually builds.\n"+
				"The same table is defined by: %v.\n"+
				"Its CREATE TABLE IF NOT EXISTS was skipped because another service's "+
				"ran first, so every query %s makes against those columns fails at "+
				"runtime — and nothing else notices, because the end-to-end suite "+
				"gives each service a database of its own.\n"+
				"Either give the services a namespace each, or decide which of them "+
				"owns the table.",
				service, table, columns, otherDefinersOf(alone, table, service), service)
		}
	}
	if unexplained == 0 {
		t.Logf("%d schemas, %d known collisions, nothing else lost",
			len(schemas), len(knownCollisions))
	}

	// And the other direction: every entry in knownCollisions still describes a
	// collision.
	//
	// A list like that rots in one direction. An entry stays after the thing it
	// describes is fixed, and then it is covering for nothing while looking like
	// diligence — and worse, it would go on excusing a *different* collision that
	// later appeared on the same table.
	for key := range knownCollisions {
		service, table, _ := strings.Cut(key, " ")
		if _, still := lost[service][table]; !still {
			t.Errorf("knownCollisions still lists %q and %s loses nothing from %s any "+
				"more. Delete the entry: a list of known breakage that outlives the "+
				"breakage is one nobody empties.", key, service, table)
		}
	}
}

// whatIsLost is the comparison: per service, per table, the columns a service
// has when it owns a database and does not have when it shares one.
func whatIsLost(alone map[string]map[string]bool, shared map[string]bool) map[string]map[string][]string {
	out := map[string]map[string][]string{}
	for service, cols := range alone {
		for col := range cols {
			if shared[col] {
				continue
			}
			table, column, ok := strings.Cut(col, ".")
			if !ok {
				continue
			}
			if out[service] == nil {
				out[service] = map[string][]string{}
			}
			out[service][table] = append(out[service][table], column)
		}
	}
	for _, tables := range out {
		for table := range tables {
			sort.Strings(tables[table])
		}
	}
	return out
}

// otherDefinersOf is the services other than this one that define a table of
// this name — the thing somebody needs to know next, and the thing that is
// tedious to find by hand across twenty-eight schema files.
func otherDefinersOf(alone map[string]map[string]bool, table, except string) []string {
	var out []string
	for service, cols := range alone {
		if service == except {
			continue
		}
		for col := range cols {
			if name, _, ok := strings.Cut(col, "."); ok && name == table {
				out = append(out, service)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// The comparison, against catalogues whose answer is known.
//
// Needs no database. It is the part that decides, and until it had this, a
// version of it that always answered "nothing lost" passed every other test in
// this file.
func TestTheComparisonFindsAColumnThatWentMissing(t *testing.T) {
	alone := map[string]map[string]bool{
		"billing-service": {"invoices.id": true, "invoices.customer_id": true},
		"order-service":   {"invoices.id": true, "invoices.order_id": true, "orders.id": true},
	}
	// What one database holding both actually ends up with: billing's invoices,
	// because it was applied first, plus order's own tables.
	shared := map[string]bool{"invoices.id": true, "invoices.customer_id": true, "orders.id": true}

	lost := whatIsLost(alone, shared)
	if len(lost) != 1 {
		t.Fatalf("one service loses something and the comparison says %d do: %v", len(lost), lost)
	}
	if got := lost["order-service"]["invoices"]; len(got) != 1 || got[0] != "order_id" {
		t.Fatalf("order-service loses invoices.order_id and the comparison says %v", got)
	}
	if _, wrongly := lost["billing-service"]; wrongly {
		t.Errorf("billing-service loses nothing and the comparison says it does: %v",
			lost["billing-service"])
	}

	// Nothing lost is reported as nothing, not as an empty entry somebody has to
	// filter: the report above iterates what comes back.
	if lost := whatIsLost(alone, map[string]bool{
		"invoices.id": true, "invoices.customer_id": true,
		"invoices.order_id": true, "orders.id": true,
	}); len(lost) != 0 {
		t.Errorf("with every column present the comparison still reports %v", lost)
	}

	// And who else defines it, which is the sentence that tells somebody what to
	// do about it.
	if others := otherDefinersOf(alone, "invoices", "order-service"); len(others) != 1 ||
		others[0] != "billing-service" {
		t.Errorf("the other definer of invoices is billing-service and this says %v", others)
	}
}

// columnsAfterApplying builds a database from these files and returns
// "table.column" for everything in it.
func columnsAfterApplying(ctx context.Context, t *testing.T, admin *pgx.Conn,
	dsnFor, name, polyfill string, schemas ...string,
) map[string]bool {
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

	conn, err := pgx.Connect(ctx, fmt.Sprintf(dsnFor, name))
	if err != nil {
		t.Fatalf("connect to %s: %v", name, err)
	}
	defer func() { _ = conn.Close(ctx) }()

	for _, f := range append([]string{polyfill}, schemas...) {
		sql, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx, string(sql)); err != nil {
			rel, _ := filepath.Rel(filepath.Dir(filepath.Dir(f)), f)
			t.Fatalf("apply %s to %s: %v", rel, name, err)
		}
	}

	rows, err := conn.Query(ctx, `
		SELECT table_name || '.' || column_name
		  FROM information_schema.columns
		 WHERE table_schema = 'public'`)
	if err != nil {
		t.Fatalf("read the catalogue of %s: %v", name, err)
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			t.Fatal(err)
		}
		out[col] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatalf("%s has no columns after applying %d schemas", name, len(schemas))
	}
	return out
}

// serviceOf is the service a schema file belongs to:
// services/<name>/internal/db/schema.sql.
func serviceOf(root, schema string) string {
	rel, err := filepath.Rel(filepath.Join(root, "services"), schema)
	if err != nil {
		return schema
	}
	return strings.SplitN(rel, string(filepath.Separator), 2)[0]
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// .../tools/dbadmin/internal/schema -> repository root
	for range 4 {
		wd = filepath.Dir(wd)
	}
	if _, err := os.Stat(filepath.Join(wd, "go.work")); err != nil {
		t.Fatalf("no go.work above the test's working directory: %v", err)
	}
	return wd
}
