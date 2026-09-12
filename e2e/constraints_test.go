//go:build e2e

// What the database refuses on its own.
//
// There are 241 CHECK constraints across twenty-one schemas and not one of them
// was exercised by anything. That is a particular kind of untested: they are the
// second line, and the first line — the domain validators in Go — catches almost
// everything before it reaches PostgreSQL. A movement from a node to itself is
// refused by material-service's domain package; the constraint that says the same
// thing has never fired, and a constraint that has never fired is
// indistinguishable from one that is wrong.
//
// Which is the point of having it. The constraints are not there to serve the
// handlers, they are there for the paths that do not go through them: a repair
// script, a bulk load, a psql session at nine at night during a settlement run, a
// handler written next year by somebody who did not know the rule. Those are
// exactly the paths where a mistake reaches a producer's money, and exactly the
// paths no test drives.
//
// So these tests write straight to the database, as the application role, saying
// the things the handlers would never say — and require PostgreSQL to refuse.
package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Every named CHECK constraint a schema declares exists in a database built
// from it, and every live one is enforced against rows already there.
//
// By name rather than by count, and that distinction was arrived at the hard
// way. Counting every CHECK( in the file reports production-service missing three
// and tenant-service missing one — both wrong, because those are inside DO $$
// blocks whose entire purpose is to do nothing when the CREATE TABLE above has
// already provided the rule. Excluding the blocks then reports billing,
// inventory and order having more than they declare — also wrong, because in
// those schemas the block does run on a fresh database. A count cannot tell a
// conditional that correctly declined from one that silently failed.
//
// A name can. The 86 named constraints declared outside those blocks are
// unconditional: each is either in the database under that name or it is not.
// Anonymous inline CHECKs are left out, not because they matter less but because
// they are part of a CREATE TABLE that either ran or failed loudly — there is no
// quiet way for one of those to go missing.
//
// The other half is NOT VALID. A constraint added that way applies to rows
// written from now on and not to the ones already there, which are the rows
// somebody added the constraint because of. Validating afterwards is a separate
// step and the step people forget.
func TestEveryNamedCheckConstraintIsCreatedAndValidated(t *testing.T) {
	root := repoRoot(t)
	ctx := context.Background()

	schemas, err := filepath.Glob(filepath.Join(root, "services", "*-service",
		"internal", "db", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if len(schemas) < 15 {
		t.Fatalf("found only %d schemas; the glob has probably stopped matching, "+
			"and a check that finds nothing passes", len(schemas))
	}

	admin, err := pgx.Connect(ctx, dsn(t, "postgres"))
	if err != nil {
		t.Fatalf("connect as admin: %v", err)
	}
	defer admin.Close(context.Background())

	named := regexp.MustCompile(`(?i)\bCONSTRAINT\s+([a-z_0-9]+)\s+CHECK\s*\(`)
	upgradeBlock := regexp.MustCompile(`(?is)DO\s*\$\$.*?\$\$\s*;`)
	// Line comments, stripped before matching. These schemas explain themselves
	// at length, and a constraint described in prose — or one commented out
	// while somebody worked on it — would otherwise be demanded of the database.
	// There is none today; this is so that writing one does not produce a
	// failure about the wrong thing.
	lineComment := regexp.MustCompile(`(?m)--[^\n]*`)

	checked := 0
	for _, path := range schemas {
		svc := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(path))))
		t.Run(svc, func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var want []string
			declared := lineComment.ReplaceAll(upgradeBlock.ReplaceAll(src, nil), nil)
			for _, m := range named.FindAllSubmatch(declared, -1) {
				want = append(want, strings.ToLower(string(m[1])))
			}
			if len(want) == 0 {
				t.Skip("no named CHECK constraints outside upgrade blocks")
			}
			sort.Strings(want)

			db := "e2e_checks_" + strings.ReplaceAll(svc, "-", "_")
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
			if _, err := conn.Exec(ctx, string(src)); err != nil {
				t.Fatalf("apply the %s schema: %v", svc, err)
			}

			live := map[string]bool{}
			var unvalidated []string
			rows, err := conn.Query(ctx, `
				SELECT conname, convalidated FROM pg_constraint
				 WHERE contype = 'c' AND connamespace = 'public'::regnamespace`)
			if err != nil {
				t.Fatalf("read constraints: %v", err)
			}
			for rows.Next() {
				var name string
				var valid bool
				if err := rows.Scan(&name, &valid); err != nil {
					t.Fatal(err)
				}
				live[strings.ToLower(name)] = true
				if !valid {
					unvalidated = append(unvalidated, name)
				}
			}
			rows.Close()

			var missing []string
			for _, name := range want {
				if !live[name] {
					missing = append(missing, name)
				}
			}
			if len(missing) > 0 {
				t.Errorf("%d constraints are declared in %s's schema and absent from a "+
					"database built from it:\n  %s\n"+
					"A rule written in the schema file and not in the database is a rule "+
					"a reader of that file believes in and nothing enforces.",
					len(missing), svc, strings.Join(missing, "\n  "))
			}
			sort.Strings(unvalidated)
			if len(unvalidated) > 0 {
				t.Errorf("%d constraints in %s are NOT VALID:\n  %s\n"+
					"Those hold for rows written from now on and not for the rows "+
					"already there.", len(unvalidated), svc, strings.Join(unvalidated, "\n  "))
			}
			checked += len(want)
		})
	}
	t.Logf("checked %d named CHECK constraints across %d schemas", checked, len(schemas))
}

// Every constraint on producer_payables refuses the thing it names.
//
// This is the table where being wrong costs a producer money, and every rule on
// it is stated twice: once in settlement-service's domain package and once here.
// The Go copy is what a caller meets. This one is what a repair script meets, and
// a repair script run against a settlement that has already gone out is the
// situation these were written for.
//
// Written as one violation per constraint, from a row that is otherwise valid, so
// a failure names the single rule that did not hold rather than "the insert was
// refused".
func TestEveryPayableConstraintRefusesWhatItNames(t *testing.T) {
	ctx := context.Background()
	// Its own database, built from settlement-service's schema.sql.
	//
	// The first version used the harness's e2e_settlement, and that is a shared
	// database other tests write to. Worse, while mutation-testing this file a
	// constraint was dropped from it by hand and not put back, and the next full
	// run reported a missing constraint that was present in the repository the
	// whole time — a test failing about the state of a database rather than about
	// the code it is meant to be checking. Building its own makes what it reports
	// reproducible from the schema file alone.
	conn := freshSchema(t, ctx, "settlement-service", "e2e_payable_checks")

	tenant := newID("TN")
	cycle := seedCycle(t, ctx, conn, tenant)

	// A payable that breaks nothing. Every case below is this row with one field
	// changed, which is what makes the constraint named in the failure the only
	// thing that differs.
	valid := map[string]any{
		"tenant_id":                   tenant,
		"cycle_id":                    cycle,
		"producer_ref":                "P-001",
		"currency":                    "INR",
		"amount_scale":                4,
		"gross_minor_units":           int64(1000),
		"deducted_minor_units":        int64(250),
		"net_minor_units":             int64(750),
		"carried_forward_minor_units": int64(0),
		"status":                      "PAYABLE",
		"kind":                        "SETTLEMENT",
	}

	// Prove the baseline is actually insertable. Without this every case below
	// could be passing because the row is malformed for some other reason, and
	// the whole test would report success while checking nothing.
	if err := insertPayable(ctx, conn, valid, nil); err != nil {
		t.Fatalf("the baseline payable was refused, so every case below would pass "+
			"for the wrong reason: %v", err)
	}

	for _, c := range []struct {
		constraint string
		why        string
		change     map[string]any
	}{
		{
			"producer_payables_adds_up",
			"a payable whose parts do not add up is a document a producer can hold " +
				"up in a room and be right about",
			map[string]any{"net_minor_units": int64(900)},
		},
		{
			"producer_payables_deducted_minor_units_check",
			"a negative deduction is a payment dressed as a recovery",
			map[string]any{"deducted_minor_units": int64(-100), "net_minor_units": int64(1100)},
		},
		{
			"producer_payables_carried_forward_minor_units_check",
			"a negative carry-forward takes money out of the next fortnight with " +
				"nothing on this one to explain it",
			map[string]any{"carried_forward_minor_units": int64(-1)},
		},
		{
			"producer_payables_amount_scale_check",
			"a scale outside 0..9 makes every figure in the row mean something else",
			map[string]any{"amount_scale": 12},
		},
		{
			"producer_payables_status_check",
			"a status nothing recognises is a payable no query finds and no report counts",
			map[string]any{"status": "NEARLY"},
		},
		{
			"producer_payables_kind_check",
			"a kind nothing recognises escapes every rule below that is written in " +
				"terms of the kind",
			map[string]any{"kind": "SOMETHING"},
		},
		{
			"producer_payables_settlement_gross_is_not_negative",
			"a fortnight cannot earn a negative amount of milk money; a settlement " +
				"row below zero means the gathering arithmetic went wrong",
			map[string]any{
				"gross_minor_units": int64(-1000), "deducted_minor_units": int64(0),
				"net_minor_units": int64(-1000),
			},
		},
		{
			"producer_payables_adjustment_has_a_reason",
			"an unexplained payment outside the settlement that computed it is the " +
				"single row in this schema most worth explaining",
			map[string]any{
				"kind": "ADJUSTMENT", "deducted_minor_units": int64(0),
				"net_minor_units": int64(1000),
			},
		},
		{
			"producer_payables_adjustment_is_not_zero",
			"an adjustment of zero moves no money and puts a line on a statement " +
				"saying nothing happened",
			map[string]any{
				"kind": "ADJUSTMENT", "reason": "a correction",
				"gross_minor_units": int64(0), "deducted_minor_units": int64(0),
				"net_minor_units": int64(0),
			},
		},
		{
			"producer_payables_adjustment_deducts_nothing",
			"a deduction on an adjustment is a recovery taken twice: once in the " +
				"settlement and again in the correction of it",
			map[string]any{
				"kind": "ADJUSTMENT", "reason": "a correction",
				"gross_minor_units": int64(1000), "deducted_minor_units": int64(250),
				"net_minor_units": int64(750),
			},
		},
		{
			"producer_payables_only_adjustments_adjust",
			"a settlement row that claims to correct another one is two fortnights " +
				"pointing at each other",
			map[string]any{"adjusts_payable_id": newID("PY")},
		},
		{
			"producer_payables_hold_has_a_reason",
			"a producer whose money is held and nobody wrote down why",
			map[string]any{"status": "HELD"},
		},
		{
			"producer_payables_approval_is_attributed",
			"an approval with no approver is a decision nobody made",
			map[string]any{"status": "APPROVED", "approved_at": "2026-01-01T00:00:00Z"},
		},
		{
			"producer_payables_payment_is_attributed",
			"a payment with nobody recorded as having made it",
			map[string]any{
				"status": "PAID", "approved_at": "2026-01-01T00:00:00Z",
				"approved_by": "US_A", "paid_at": "2026-01-02T00:00:00Z",
			},
		},
		{
			"producer_payables_paid_after_approval",
			"money that left before anybody approved it leaving",
			map[string]any{
				"status": "PAID", "paid_at": "2026-01-02T00:00:00Z", "paid_by": "US_A",
			},
		},
	} {
		t.Run(c.constraint, func(t *testing.T) {
			// A producer of its own for each case.
			//
			// There is a unique index over (tenant, cycle, producer) for
			// settlement rows, and sharing a producer across cases makes every
			// one of them depend on PostgreSQL checking the CHECK before the
			// index. It does today; nothing promises it, and the failure would
			// be a case reporting success because a different rule refused the
			// row. Found by dropping producer_payables_adds_up and watching the
			// unique index answer in its place.
			change := map[string]any{"producer_ref": "P-" + c.constraint}
			for k, v := range c.change {
				change[k] = v
			}
			err := insertPayable(ctx, conn, valid, change)
			if err == nil {
				t.Fatalf("the database accepted this row.\n%s\nThe rule is written in "+
					"settlement-service's domain package as well, so a caller never "+
					"gets this far — which is why nothing noticed the constraint was "+
					"not doing its half.", c.why)
			}
			// Named, not merely refused. A row rejected by some other rule is a
			// test that passes without exercising the one it claims to.
			if !strings.Contains(err.Error(), c.constraint) {
				t.Errorf("refused by something other than %s: %v", c.constraint, err)
			}
		})
	}
}

// freshSchema builds a database from one service's schema.sql and connects to it.
func freshSchema(t *testing.T, ctx context.Context, svc, db string) *pgx.Conn {
	t.Helper()
	admin, err := pgx.Connect(ctx, dsn(t, "postgres"))
	if err != nil {
		t.Fatalf("connect as admin: %v", err)
	}
	defer admin.Close(context.Background())

	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+db+" WITH (FORCE)"); err != nil {
		t.Fatalf("drop %s: %v", db, err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+db); err != nil {
		t.Fatalf("create %s: %v", db, err)
	}

	conn, err := pgx.Connect(ctx, dsn(t, db))
	if err != nil {
		t.Fatalf("connect to %s: %v", db, err)
	}
	src, err := os.ReadFile(filepath.Join(repoRoot(t), "services", svc, "internal", "db", "schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, string(src)); err != nil {
		t.Fatalf("apply the %s schema: %v", svc, err)
	}

	t.Cleanup(func() {
		bg := context.Background()
		conn.Close(bg)
		a, err := pgx.Connect(bg, dsn(t, "postgres"))
		if err != nil {
			return
		}
		defer a.Close(bg)
		_, _ = a.Exec(bg, "DROP DATABASE IF EXISTS "+db+" WITH (FORCE)")
	})
	return conn
}

// seedCycle writes the payment cycle a payable has to belong to.
//
// Not a detail: producer_payables carries a foreign key to it, and the first
// version of this test skipped it. The baseline insert was refused for that
// reason, and every case below would then have "failed" — correctly, by
// coincidence — while exercising nothing at all. That is why the baseline is
// asserted separately before any of them runs.
func seedCycle(t *testing.T, ctx context.Context, conn *pgx.Conn, tenant string) string {
	t.Helper()
	id := newID("CY")
	if _, err := conn.Exec(ctx, "SELECT set_config('app.tenant_id', $1, false)", tenant); err != nil {
		t.Fatalf("set the tenant: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO payment_cycles
		(id,tenant_id,society_code,name,period_start,period_end,currency,amount_scale,
		 deduction_policy,status,created_by,updated_by)
		VALUES ($1,$2,'SOC-1','a fortnight','2026-01-01','2026-01-15','INR',4,
		        'CAP_AT_EARNINGS','OPEN','US_A','US_A')`, id, tenant); err != nil {
		t.Fatalf("seed a payment cycle: %v", err)
	}
	return id
}

// insertPayable writes the base row with change applied over it.
func insertPayable(ctx context.Context, conn *pgx.Conn, base, change map[string]any) error {
	row := map[string]any{"id": newID("PY")}
	for k, v := range base {
		row[k] = v
	}
	for k, v := range change {
		row[k] = v
	}

	cols := make([]string, 0, len(row))
	for k := range row {
		cols = append(cols, k)
	}
	sort.Strings(cols)

	args := make([]any, 0, len(cols))
	holders := make([]string, 0, len(cols))
	for i, c := range cols {
		args = append(args, row[c])
		holders = append(holders, fmt.Sprintf("$%d", i+1))
	}

	// The tenant, for the row-level security policies, which are as much a part
	// of writing here as the columns are.
	if _, err := conn.Exec(ctx, "SELECT set_config('app.tenant_id', $1, false)",
		row["tenant_id"]); err != nil {
		return err
	}
	_, err := conn.Exec(ctx, "INSERT INTO producer_payables ("+strings.Join(cols, ",")+
		") VALUES ("+strings.Join(holders, ",")+")", args...)
	return err
}
