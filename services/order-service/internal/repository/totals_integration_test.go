package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/exact"
	"github.com/ppusapati/gavya/services/order-service/internal/domain"
)

// testIDs supplies the identifiers audit entries carry. The chain does not care
// what the id is, only that it is unique.
type testIDs struct{}

func (testIDs) New() string { return newTestID("AU") }

// Money arithmetic is the thing this repository must not get wrong, and every
// property below is a property of what PostgreSQL does with these statements.
//
//	TEST_DATABASE_DSN="postgres://user@host:port/%s?sslmode=disable" go test ./...

const testDatabase = "order_repo_test"

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	tpl := os.Getenv("TEST_DATABASE_DSN")
	if tpl == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}
	ctx := context.Background()

	admin, err := pgxpool.New(ctx, fmt.Sprintf(tpl, "postgres"))
	if err != nil {
		t.Fatalf("connect to the maintenance database: %v", err)
	}
	defer admin.Close()

	var exists bool
	if err := admin.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname=$1)`, testDatabase).Scan(&exists); err != nil {
		t.Fatalf("look for %s: %v", testDatabase, err)
	}
	if !exists {
		if _, err := admin.Exec(ctx, `CREATE DATABASE "`+testDatabase+`"`); err != nil {
			t.Fatalf("create %s: %v", testDatabase, err)
		}
	}

	pool, err := pgxpool.New(ctx, fmt.Sprintf(tpl, testDatabase))
	if err != nil {
		t.Fatalf("connect to %s: %v", testDatabase, err)
	}
	t.Cleanup(pool.Close)

	schema, err := os.ReadFile(filepath.Join("..", "db", "schema.sql"))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if _, err := pool.Exec(ctx, string(schema)); err != nil {
		t.Fatalf("apply schema: %v", err)
	}
	return pool
}

var idSeq atomic.Int64

func newTestID(prefix string) string {
	body := fmt.Sprintf("%s%d%d", prefix, time.Now().UnixNano(), idSeq.Add(1))
	if len(body) > 26 {
		return body[:26]
	}
	return body + "0000000000000000000000000"[:26-len(body)]
}

// Rates are percentages now, per line, so 18 means eighteen per cent.
const taxRate = "18.000"

// INR at two decimals, unless a test says otherwise.
var rupees = Money{Code: "INR", Scale: 2}

type fixture struct {
	repo    Repository
	tenant  string
	order   string
	money   Money
	taxIncl bool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return newFixtureIn(t, rupees, false)
}

func newFixtureIn(t *testing.T, money Money, taxInclusive bool) *fixture {
	t.Helper()
	pool := testPool(t)
	f := &fixture{
		repo:    New(pool, testIDs{}),
		tenant:  newTestID("tnt"),
		order:   newTestID("ord"),
		money:   money,
		taxIncl: taxInclusive,
	}

	if _, err := pool.Exec(context.Background(),
		`INSERT INTO orders (id,tenant_id,customer_id,order_number,status,currency,tax_inclusive,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,'draft',$5,$6,$7,$7)`,
		f.order, f.tenant, newTestID("cst"), newTestID("num"),
		money.Code, taxInclusive, newTestID("usr")); err != nil {
		t.Fatalf("create order: %v", err)
	}
	return f
}

func (f *fixture) add(t *testing.T, quantity, unitPrice float64) (*ItemOutcome, error) {
	t.Helper()
	return f.addAt(t, quantity, unitPrice, taxRate)
}

func (f *fixture) addAt(t *testing.T, quantity, unitPrice float64, rate string) (*ItemOutcome, error) {
	t.Helper()
	q, err := exact.NonNegativeDecimal(quantity, 3, 10)
	if err != nil {
		t.Fatalf("quantity %v: %v", quantity, err)
	}
	p, err := exact.NonNegativeDecimal(unitPrice, f.money.Scale, 18)
	if err != nil {
		t.Fatalf("unit price %v: %v", unitPrice, err)
	}
	actor := newTestID("usr")
	return f.repo.AddItemAndRetotal(context.Background(), &domain.OrderItem{
		ID:        newTestID("itm"),
		TenantID:  f.tenant,
		OrderID:   f.order,
		SKUID:     newTestID("sku"),
		ProductID: newTestID("prd"),
		Status:    "pending",
		CreatedBy: actor,
		UpdatedBy: actor,
	}, q, p, rate, f.money)
}

func (f *fixture) setStatus(t *testing.T, status string) {
	t.Helper()
	pool := testPool(t)
	if _, err := pool.Exec(context.Background(),
		`UPDATE orders SET status=$3 WHERE id=$1 AND tenant_id=$2`, f.order, f.tenant, status); err != nil {
		t.Fatalf("set status: %v", err)
	}
}

func TestALineTotalIsTheProductOfItsQuantityAndPrice(t *testing.T) {
	f := newFixture(t)

	out, err := f.add(t, 3, 19.99)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if out.Item.TotalPrice != 59.97 {
		t.Errorf("line total = %v, want 59.97", out.Item.TotalPrice)
	}
}

// 2.3 * 45.55 is 104.765, which the column cannot hold. It must be rounded once,
// by the database, to a figure that is reproducible — not arrived at through a
// float64 product that was already slightly wrong before rounding.
func TestALineTotalRoundsOnceAtTheEnd(t *testing.T) {
	f := newFixture(t)

	out, err := f.add(t, 2.3, 45.55)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if out.Item.TotalPrice != 104.77 {
		t.Errorf("line total = %v, want 104.77 (2.3 * 45.55 = 104.765, rounded once)", out.Item.TotalPrice)
	}
}

// The defect: the totals update's error was logged and thrown away, so an order
// could carry a subtotal that disagreed with the sum of its own lines.
func TestTheOrderTotalAlwaysEqualsTheSumOfItsLines(t *testing.T) {
	f := newFixture(t)

	lines := []struct{ q, p float64 }{{1, 10.10}, {2, 5.05}, {3.5, 2.20}, {0.25, 99.99}}
	var want float64
	for _, l := range lines {
		out, err := f.add(t, l.q, l.p)
		if err != nil {
			t.Fatalf("add %v x %v: %v", l.q, l.p, err)
		}
		want += out.Item.TotalPrice
		if out.Order.SubTotal != want {
			t.Fatalf("sub total = %v, want %v (the sum of the lines so far)", out.Order.SubTotal, want)
		}
	}
}

func TestTaxAndTotalFollowTheSubTotal(t *testing.T) {
	f := newFixture(t)

	out, err := f.add(t, 1, 100)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if out.Order.SubTotal != 100 {
		t.Fatalf("sub total = %v, want 100", out.Order.SubTotal)
	}
	if out.Order.TaxAmount != 18 {
		t.Errorf("tax = %v, want 18", out.Order.TaxAmount)
	}
	if out.Order.TotalAmount != 118 {
		t.Errorf("total = %v, want 118", out.Order.TotalAmount)
	}
}

// Adding a hundred lines of 0.10 in float64 leaves 9.999999999999998. The
// database sums in NUMERIC, so it is exactly 10.
func TestManySmallLinesSumExactly(t *testing.T) {
	f := newFixture(t)

	for i := 0; i < 100; i++ {
		if _, err := f.add(t, 1, 0.10); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
	}

	out, err := f.add(t, 1, 0.10)
	if err != nil {
		t.Fatal(err)
	}
	if out.Order.SubTotal != 10.10 {
		t.Errorf("sub total = %.17g, want exactly 10.10", out.Order.SubTotal)
	}
}

// The draft check used to happen in a different call from the write, so an order
// could be confirmed in between and still take another line.
func TestAConfirmedOrderTakesNoMoreLines(t *testing.T) {
	f := newFixture(t)
	if _, err := f.add(t, 1, 10); err != nil {
		t.Fatal(err)
	}
	f.setStatus(t, domain.OrderConfirmed)

	if _, err := f.add(t, 1, 10); !errors.Is(err, ErrNotDraft) {
		t.Fatalf("err = %v, want ErrNotDraft", err)
	}
}

func TestAddingToAnOrderThatDoesNotExistIsNotFound(t *testing.T) {
	f := newFixture(t)
	f.order = newTestID("gone")

	if _, err := f.add(t, 1, 10); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// Concurrent lines must all be counted. The old shape summed outside a
// transaction, so two adds could each write totals that omitted the other.
func TestConcurrentLinesAreAllCounted(t *testing.T) {
	f := newFixture(t)

	const lines = 15
	var wg sync.WaitGroup
	errs := make(chan error, lines)
	for i := 0; i < lines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.add(t, 1, 2.50); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent add: %v", err)
	}

	out, err := f.add(t, 1, 2.50)
	if err != nil {
		t.Fatal(err)
	}
	want := 2.50 * (lines + 1)
	if out.Order.SubTotal != want {
		t.Errorf("sub total = %v, want %v — lines were lost", out.Order.SubTotal, want)
	}
}

// A line that cannot be written must leave the order's totals alone, rather
// than half-applying a change.
func TestAFailedLineLeavesTheOrderUntouched(t *testing.T) {
	f := newFixture(t)
	first, err := f.add(t, 2, 25)
	if err != nil {
		t.Fatal(err)
	}

	// A duplicate id cannot be inserted, so the whole transaction must roll back.
	actor := newTestID("usr")
	_, err = f.repo.AddItemAndRetotal(context.Background(), &domain.OrderItem{
		ID:        first.Item.ID,
		TenantID:  f.tenant,
		OrderID:   f.order,
		SKUID:     newTestID("sku"),
		ProductID: newTestID("prd"),
		Status:    "pending",
		CreatedBy: actor,
		UpdatedBy: actor,
	}, "1.000", "99.00", taxRate, f.money)
	if err == nil {
		t.Fatal("a duplicate line was accepted")
	}

	out, err := f.add(t, 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	if out.Order.SubTotal != 55 {
		t.Errorf("sub total = %v, want 55 — the failed line changed the order", out.Order.SubTotal)
	}
}

/* ---- multi-currency and tax ---- */

func TestAYenOrderRoundsToWholeYen(t *testing.T) {
	f := newFixtureIn(t, Money{Code: "JPY", Scale: 0}, false)

	out, err := f.add(t, 3, 1250)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if out.Item.TotalPrice != 3750 {
		t.Errorf("line total = %v, want 3750", out.Item.TotalPrice)
	}
	if out.Order.TotalAmount != 4425 {
		t.Errorf("total = %v, want 4425", out.Order.TotalAmount)
	}
}

// Rounded at two decimals this line would be 1.01, and a tenth of a fils would
// have been invented rather than earned.
func TestADinarOrderLineRoundsAtThreeDecimals(t *testing.T) {
	f := newFixtureIn(t, Money{Code: "KWD", Scale: 3}, false)

	out, err := f.addAt(t, 3, 0.335, "0.000")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if out.Item.TotalPrice != 1.005 {
		t.Errorf("line total = %v, want 1.005", out.Item.TotalPrice)
	}
}

func TestATenantCannotOrderInASecondCurrency(t *testing.T) {
	f := newFixture(t)
	if _, err := f.add(t, 1, 100); err != nil {
		t.Fatal(err)
	}

	f.money = Money{Code: "USD", Scale: 2}
	if _, err := f.add(t, 1, 100); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("err = %v, want ErrCurrencyMismatch", err)
	}
}

func TestEachOrderLineIsTaxedAtItsOwnRate(t *testing.T) {
	f := newFixture(t)

	if _, err := f.addAt(t, 1, 100, "0.000"); err != nil { // exempt
		t.Fatal(err)
	}
	out, err := f.addAt(t, 1, 100, "12.000")
	if err != nil {
		t.Fatal(err)
	}

	if out.Order.SubTotal != 200 {
		t.Errorf("sub total = %v, want 200", out.Order.SubTotal)
	}
	if out.Order.TaxAmount != 12 {
		t.Errorf("tax = %v, want 12 — only the rated line should be taxed", out.Order.TaxAmount)
	}
}

func TestTaxInclusiveOrdersExtractRatherThanAdd(t *testing.T) {
	f := newFixtureIn(t, rupees, true)

	out, err := f.addAt(t, 1, 120, "20.000")
	if err != nil {
		t.Fatal(err)
	}
	if out.Order.TotalAmount != 120 {
		t.Errorf("total = %v, want 120 — the quoted price is what is charged", out.Order.TotalAmount)
	}
	if out.Order.TaxAmount != 20 {
		t.Errorf("tax = %v, want 20", out.Order.TaxAmount)
	}
	if out.Order.SubTotal != 100 {
		t.Errorf("net = %v, want 100", out.Order.SubTotal)
	}
}
