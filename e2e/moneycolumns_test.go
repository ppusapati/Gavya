//go:build e2e

// Every money column holds the same number of decimals.
//
// The services that predate the integrity work created their money columns as
// NUMERIC(12,2), which is a rupee assumption baked into a schema. Each of them
// then grew a migration widening those columns to NUMERIC(18,4) — four decimals
// being the most any ISO 4217 currency has — so that one schema serves a yen
// deployment, a rupee one and a dinar one, with the tenant's currency saying how
// many of the four are real.
//
// order-service's widening named 'order_invoices', which is not a table in that
// schema; it is 'invoices'. It also omitted 'returns'. The loop matched nothing
// for a table that does not exist and reported success, so orders were widened
// and the invoices raised from them were not. A deployment recording dinars
// could place an order at 1.234 and have its invoice total rounded to 1.23 by
// the column, silently, leaving the invoice disagreeing with the order it came
// from.
//
// Reading the schema file did not show this: the widening is a DO block that
// searches information_schema at apply time, so what it actually does is a fact
// about the database, not about the text. This test applies each schema and
// asks the database.
package e2e

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

// moneyColumn names a column that holds an amount of currency, and nothing else.
//
// The list is explicit rather than derived from a name pattern, because the
// distinction this test rests on is one only a person can make: order_items.
// quantity is NUMERIC(10,3) and correctly so — it counts litres, not money — and
// tax_rate is a percentage, not an amount. A pattern over column names would
// either sweep those in or need exceptions, and either way would stop saying
// anything.
var moneyColumns = map[string][]string{
	"order-service": {
		"orders.sub_total", "orders.tax_amount", "orders.total_amount",
		"order_items.unit_price", "order_items.total_price",
		"invoices.sub_total", "invoices.tax_amount", "invoices.total_amount",
		"returns.refund_amount",
	},
	"billing-service": {
		"invoices.sub_total", "invoices.tax_amount", "invoices.total_amount",
		"invoice_items.unit_price", "invoice_items.total_price",
		"payments.amount",
	},
	"product-catalog-service": {"skus.price"},
	"cattle-market-service": {
		"cattle_listings.asking_price",
		"cattle_bids.bid_amount",
		"cattle_sales.sale_price",
	},
	"health-service": {"vet_visits.cost"},
}

// moneyScale is how many decimals a money column holds: four, the most any ISO
// 4217 currency has. A column with fewer cannot record a dinar; one with more
// records digits no currency has.
const moneyScale = 4

// moneyPrecision is the total width. Eighteen digits at scale four is up to
// about 10^14 of any currency, which is far beyond any figure a dairy writes and
// well inside what an int64 holds at that scale.
const moneyPrecision = 18

func TestEveryMoneyColumnHoldsFourDecimals(t *testing.T) {
	root := repoRoot(t)
	ctx := context.Background()

	admin, err := pgx.Connect(ctx, dsn(t, "postgres"))
	if err != nil {
		t.Fatalf("connect as admin: %v", err)
	}
	defer admin.Close(ctx)

	services := make([]string, 0, len(moneyColumns))
	for s := range moneyColumns {
		services = append(services, s)
	}
	sort.Strings(services)

	for _, svc := range services {
		t.Run(svc, func(t *testing.T) {
			db := "e2e_money_" + strings.ReplaceAll(svc, "-", "_")
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

			b, err := os.ReadFile(filepath.Join(root, "services", svc, "internal", "db", "schema.sql"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Exec(ctx, string(b)); err != nil {
				t.Fatalf("apply %s schema: %v", svc, err)
			}

			// One query for the whole schema, so a column that is missing from
			// the table entirely is reported as missing rather than as the wrong
			// type — those are different mistakes and the message has to say
			// which.
			rows, err := conn.Query(ctx, `
				SELECT table_name || '.' || column_name, numeric_precision, numeric_scale
				  FROM information_schema.columns
				 WHERE table_schema = 'public' AND data_type = 'numeric'`)
			if err != nil {
				t.Fatalf("read columns: %v", err)
			}
			found := map[string]string{}
			for rows.Next() {
				var name string
				var precision, scale int32
				if err := rows.Scan(&name, &precision, &scale); err != nil {
					t.Fatal(err)
				}
				found[name] = fmt.Sprintf("NUMERIC(%d,%d)", precision, scale)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}

			want := fmt.Sprintf("NUMERIC(%d,%d)", moneyPrecision, moneyScale)
			for _, col := range moneyColumns[svc] {
				got, ok := found[col]
				if !ok {
					t.Errorf("%s has no numeric column %s; either it was renamed and this "+
						"list was not, or the table is gone", svc, col)
					continue
				}
				if got != want {
					t.Errorf("%s.%s is %s, want %s\n"+
						"A money column narrower than four decimals cannot record a dinar, "+
						"and PostgreSQL rounds an over-precise value into it without saying "+
						"so — leaving this service's figures disagreeing with the ones it "+
						"was given, with nothing in the record to say when.",
						svc, col, got, want)
				}
			}
		})
	}
}
