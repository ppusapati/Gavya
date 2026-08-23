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
	"github.com/ppusapati/gavya/services/billing-service/internal/domain"
)

// Money arithmetic is the thing this repository must not get wrong, and every
// property below is a property of what PostgreSQL does with these statements.
//
//	TEST_DATABASE_DSN="postgres://user@host:port/%s?sslmode=disable" go test ./...

const testDatabase = "billing_repo_test"

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
	repo     Repository
	tenant   string
	invoice  string
	money    Money
	taxIncl  bool
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	return newFixtureIn(t, rupees, false)
}

func newFixtureIn(t *testing.T, money Money, taxInclusive bool) *fixture {
	t.Helper()
	pool := testPool(t)
	f := &fixture{
		repo:    New(pool),
		tenant:  newTestID("tnt"),
		invoice: newTestID("inv"),
		money:   money,
		taxIncl: taxInclusive,
	}

	if _, err := pool.Exec(context.Background(),
		`INSERT INTO invoices (id,tenant_id,customer_id,invoice_number,status,currency,tax_inclusive,issued_at,due_at,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,'draft',$5,$6,NOW(),NOW() + INTERVAL '30 days',$7,$7)`,
		f.invoice, f.tenant, newTestID("cst"), newTestID("num"),
		money.Code, taxInclusive, newTestID("usr")); err != nil {
		t.Fatalf("create invoice: %v", err)
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
	return f.repo.AddItemAndRetotal(context.Background(), &domain.InvoiceItem{
		ID:        newTestID("itm"),
		TenantID:  f.tenant,
		InvoiceID:   f.invoice,
		Description: "Toned milk, 1L pouch",
		CreatedBy:   actor,
		UpdatedBy:   actor,
	}, q, p, rate, f.money)
}

func (f *fixture) setStatus(t *testing.T, status string) {
	t.Helper()
	pool := testPool(t)
	if _, err := pool.Exec(context.Background(),
		`UPDATE invoices SET status=$3 WHERE id=$1 AND tenant_id=$2`, f.invoice, f.tenant, status); err != nil {
		t.Fatalf("set status: %v", err)
	}
}

func TestAnInvoiceLineIsTheProductOfItsQuantityAndPrice(t *testing.T) {
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
func TestAnInvoiceLineRoundsOnceAtTheEnd(t *testing.T) {
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
// could carry a total that disagreed with the sum of its own lines.
func TestTheInvoiceTotalAlwaysEqualsTheSumOfItsLines(t *testing.T) {
	f := newFixture(t)

	lines := []struct{ q, p float64 }{{1, 10.10}, {2, 5.05}, {3.5, 2.20}, {0.25, 99.99}}
	var want float64
	for _, l := range lines {
		out, err := f.add(t, l.q, l.p)
		if err != nil {
			t.Fatalf("add %v x %v: %v", l.q, l.p, err)
		}
		want += out.Item.TotalPrice
		if out.Invoice.SubTotal != want {
			t.Fatalf("sub total = %v, want %v (the sum of the lines so far)", out.Invoice.SubTotal, want)
		}
	}
}

func TestTaxAndTotalFollowTheSubTotal(t *testing.T) {
	f := newFixture(t)

	out, err := f.add(t, 1, 100)
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if out.Invoice.SubTotal != 100 {
		t.Fatalf("sub total = %v, want 100", out.Invoice.SubTotal)
	}
	if out.Invoice.TaxAmount != 18 {
		t.Errorf("tax = %v, want 18", out.Invoice.TaxAmount)
	}
	if out.Invoice.TotalAmount != 118 {
		t.Errorf("total = %v, want 118", out.Invoice.TotalAmount)
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
	if out.Invoice.SubTotal != 10.10 {
		t.Errorf("sub total = %.17g, want exactly 10.10", out.Invoice.SubTotal)
	}
}

// The draft check used to happen in a different call from the write, so an order
// could be confirmed in between and still take another line.
func TestASentInvoiceTakesNoMoreLines(t *testing.T) {
	f := newFixture(t)
	if _, err := f.add(t, 1, 10); err != nil {
		t.Fatal(err)
	}
	f.setStatus(t, domain.InvoiceSent)

	if _, err := f.add(t, 1, 10); !errors.Is(err, ErrNotDraft) {
		t.Fatalf("err = %v, want ErrNotDraft", err)
	}
}

func TestAddingToAnInvoiceThatDoesNotExistIsNotFound(t *testing.T) {
	f := newFixture(t)
	f.invoice = newTestID("gone")

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
	if out.Invoice.SubTotal != want {
		t.Errorf("sub total = %v, want %v — lines were lost", out.Invoice.SubTotal, want)
	}
}

// A line that cannot be written must leave the order's totals alone, rather
// than half-applying a change.
func TestAFailedLineLeavesTheInvoiceUntouched(t *testing.T) {
	f := newFixture(t)
	first, err := f.add(t, 2, 25)
	if err != nil {
		t.Fatal(err)
	}

	// A duplicate id cannot be inserted, so the whole transaction must roll back.
	actor := newTestID("usr")
	_, err = f.repo.AddItemAndRetotal(context.Background(), &domain.InvoiceItem{
		ID:          first.Item.ID,
		TenantID:    f.tenant,
		InvoiceID:   f.invoice,
		Description: "duplicate",
		CreatedBy:   actor,
		UpdatedBy:   actor,
	}, "1.000", "99.00", taxRate, f.money)
	if err == nil {
		t.Fatal("a duplicate line was accepted")
	}

	out, err := f.add(t, 1, 5)
	if err != nil {
		t.Fatal(err)
	}
	if out.Invoice.SubTotal != 55 {
		t.Errorf("sub total = %v, want 55 — the failed line changed the order", out.Invoice.SubTotal)
	}
}

func (f *fixture) pay(t *testing.T, amount float64) (*PaymentOutcome, error) {
	t.Helper()
	a, err := exact.NonNegativeDecimal(amount, 2, 12)
	if err != nil {
		t.Fatalf("amount %v: %v", amount, err)
	}
	actor := newTestID("usr")
	return f.repo.RecordPaymentAndSettle(context.Background(), &domain.Payment{
		ID:            newTestID("pay"),
		TenantID:      f.tenant,
		InvoiceID:     f.invoice,
		Currency:      "INR",
		PaymentMethod: "upi",
		PaidAt:        time.Now(),
		CreatedBy:     actor,
		UpdatedBy:     actor,
	}, a, f.money)
}

func TestAPartPaymentLeavesTheInvoiceUnsettled(t *testing.T) {
	f := newFixture(t)
	if _, err := f.add(t, 1, 100); err != nil { // total becomes 118.00
		t.Fatal(err)
	}

	out, err := f.pay(t, 50)
	if err != nil {
		t.Fatalf("pay: %v", err)
	}
	if out.Invoice.Status == domain.InvoicePaid {
		t.Errorf("a 50 payment settled an invoice for %v", out.Invoice.TotalAmount)
	}
}

func TestPaymentsThatCoverTheInvoiceSettleIt(t *testing.T) {
	f := newFixture(t)
	if _, err := f.add(t, 1, 100); err != nil {
		t.Fatal(err)
	}

	if _, err := f.pay(t, 100); err != nil {
		t.Fatal(err)
	}
	out, err := f.pay(t, 18)
	if err != nil {
		t.Fatal(err)
	}
	if out.Invoice.Status != domain.InvoicePaid {
		t.Errorf("status = %q after paying %v of %v, want paid",
			out.Invoice.Status, 118.0, out.Invoice.TotalAmount)
	}
}

// The defect: two payments arriving together each summed without the other, so
// an invoice they jointly covered could be left unsettled.
func TestConcurrentPaymentsStillSettleTheInvoice(t *testing.T) {
	f := newFixture(t)
	if _, err := f.add(t, 1, 100); err != nil {
		t.Fatal(err)
	}

	const payers = 6
	var wg sync.WaitGroup
	errs := make(chan error, payers)
	for i := 0; i < payers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.pay(t, 19.67); err != nil { // 6 * 19.67 = 118.02
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent payment: %v", err)
	}

	inv, err := f.repo.GetInvoice(context.Background(), f.invoice, f.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Status != domain.InvoicePaid {
		t.Errorf("status = %q, want paid — concurrent payments covering %v did not settle it",
			inv.Status, inv.TotalAmount)
	}
}

// Settling records when the invoice was actually settled, so a later payment
// must not move that instant.
func TestAlreadySettledInvoicesKeepTheirPaidInstant(t *testing.T) {
	f := newFixture(t)
	if _, err := f.add(t, 1, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pay(t, 118); err != nil {
		t.Fatal(err)
	}
	settled, err := f.repo.GetInvoice(context.Background(), f.invoice, f.tenant)
	if err != nil {
		t.Fatal(err)
	}

	out, err := f.pay(t, 1)
	if err != nil {
		t.Fatal(err)
	}
	if settled.PaidAt == nil || out.Invoice.PaidAt == nil {
		t.Fatal("a settled invoice has no paid instant")
	}
	if !out.Invoice.PaidAt.Equal(*settled.PaidAt) {
		t.Errorf("paid instant moved from %v to %v", settled.PaidAt, out.Invoice.PaidAt)
	}
}

/* ---- multi-currency ---- */

// A yen has no minor unit. Rounding a yen line to two decimals would produce a
// figure no Japanese invoice could show, and storing ¥100 as 100.00 misstates
// what the number is.
func TestAYenInvoiceRoundsToWholeYen(t *testing.T) {
	f := newFixtureIn(t, Money{Code: "JPY", Scale: 0}, false)

	out, err := f.add(t, 3, 1250) // 3 × ¥1,250
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if out.Item.TotalPrice != 3750 {
		t.Errorf("line total = %v, want 3750", out.Item.TotalPrice)
	}
	// 3750 × 18% = 675 exactly, with no minor unit to round into.
	if out.Invoice.TaxAmount != 675 {
		t.Errorf("tax = %v, want 675", out.Invoice.TaxAmount)
	}
	if out.Invoice.TotalAmount != 4425 {
		t.Errorf("total = %v, want 4425", out.Invoice.TotalAmount)
	}
}

// A dinar has three. Rounding to two would discard a fils, which is real money.
func TestADinarInvoiceKeepsItsThirdDecimal(t *testing.T) {
	f := newFixtureIn(t, Money{Code: "KWD", Scale: 3}, false)

	out, err := f.add(t, 2, 1.375) // 2 × KD 1.375
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if out.Item.TotalPrice != 2.750 {
		t.Errorf("line total = %v, want 2.750", out.Item.TotalPrice)
	}
	// 2.750 × 18% = 0.495 exactly — a figure that rounding to two decimals
	// would have turned into 0.50 and quietly overcharged by five fils.
	if out.Invoice.TaxAmount != 0.495 {
		t.Errorf("tax = %v, want 0.495", out.Invoice.TaxAmount)
	}
}

// A tenant records money in one currency. A second one arriving is refused by
// the database rather than sitting alongside the first, indistinguishable in
// every total that adds them up.
func TestATenantCannotRecordASecondCurrency(t *testing.T) {
	f := newFixture(t)
	if _, err := f.add(t, 1, 100); err != nil {
		t.Fatal(err)
	}

	f.money = Money{Code: "USD", Scale: 2}
	if _, err := f.add(t, 1, 100); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("err = %v, want ErrCurrencyMismatch", err)
	}
}

// The code and the scale have to agree. The same digits at a different scale
// are a different amount of money.
func TestACurrencyAtTheWrongScaleIsRefused(t *testing.T) {
	f := newFixture(t)
	if _, err := f.add(t, 1, 100); err != nil {
		t.Fatal(err)
	}

	f.money = Money{Code: "INR", Scale: 3}
	if _, err := f.add(t, 1, 100); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("err = %v, want ErrCurrencyMismatch", err)
	}
}

func TestAPaymentInADifferentCurrencyFromItsInvoiceIsRefused(t *testing.T) {
	f := newFixture(t)
	if _, err := f.add(t, 1, 100); err != nil {
		t.Fatal(err)
	}

	f.money = Money{Code: "EUR", Scale: 2}
	if _, err := f.pay(t, 118); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("err = %v, want ErrCurrencyMismatch", err)
	}
}

/* ---- tax ---- */

// The rate belongs to the line. An invoice that mixes exempt and rated goods —
// which a dairy catalogue does routinely — cannot be taxed at one rate.
func TestEachLineIsTaxedAtItsOwnRate(t *testing.T) {
	f := newFixture(t)

	if _, err := f.addAt(t, 1, 100, "0.000"); err != nil { // exempt: fresh milk
		t.Fatal(err)
	}
	out, err := f.addAt(t, 1, 100, "12.000") // rated: ghee
	if err != nil {
		t.Fatal(err)
	}

	if out.Invoice.SubTotal != 200 {
		t.Errorf("sub total = %v, want 200", out.Invoice.SubTotal)
	}
	if out.Invoice.TaxAmount != 12 {
		t.Errorf("tax = %v, want 12 — only the rated line should be taxed", out.Invoice.TaxAmount)
	}
	if out.Invoice.TotalAmount != 212 {
		t.Errorf("total = %v, want 212", out.Invoice.TotalAmount)
	}
}

// Much of the world quotes a price that already contains the tax. The tax is
// then extracted from the price rather than added to it, and getting that
// backwards overcharges every customer.
func TestTaxInclusivePricingExtractsRatherThanAdds(t *testing.T) {
	f := newFixtureIn(t, rupees, true)

	// £120 quoted inclusive of 20% VAT is £100 net and £20 tax.
	out, err := f.addAt(t, 1, 120, "20.000")
	if err != nil {
		t.Fatal(err)
	}

	if out.Invoice.TotalAmount != 120 {
		t.Errorf("total = %v, want 120 — the quoted price is what is charged", out.Invoice.TotalAmount)
	}
	if out.Invoice.TaxAmount != 20 {
		t.Errorf("tax = %v, want 20", out.Invoice.TaxAmount)
	}
	if out.Invoice.SubTotal != 100 {
		t.Errorf("net = %v, want 100", out.Invoice.SubTotal)
	}
}

// The same figures the other way round, so the two models cannot be confused.
func TestTaxExclusivePricingAddsRatherThanExtracts(t *testing.T) {
	f := newFixtureIn(t, rupees, false)

	out, err := f.addAt(t, 1, 120, "20.000")
	if err != nil {
		t.Fatal(err)
	}

	if out.Invoice.SubTotal != 120 {
		t.Errorf("net = %v, want 120", out.Invoice.SubTotal)
	}
	if out.Invoice.TaxAmount != 24 {
		t.Errorf("tax = %v, want 24", out.Invoice.TaxAmount)
	}
	if out.Invoice.TotalAmount != 144 {
		t.Errorf("total = %v, want 144", out.Invoice.TotalAmount)
	}
}

func TestAZeroRatedInvoiceCarriesNoTax(t *testing.T) {
	f := newFixture(t)

	out, err := f.addAt(t, 2, 45.50, "0.000")
	if err != nil {
		t.Fatal(err)
	}
	if out.Invoice.TaxAmount != 0 {
		t.Errorf("tax = %v, want 0", out.Invoice.TaxAmount)
	}
	if out.Invoice.TotalAmount != out.Invoice.SubTotal {
		t.Errorf("total %v differs from net %v with no tax", out.Invoice.TotalAmount, out.Invoice.SubTotal)
	}
}

// The line total is rounded at the currency's scale, not at two. A dinar line
// whose product lands on the third decimal proves it: rounded at two it would
// be 1.01, and the tenth of a fils would have been invented rather than earned.
func TestADinarLineTotalRoundsAtThreeDecimals(t *testing.T) {
	f := newFixtureIn(t, Money{Code: "KWD", Scale: 3}, false)

	out, err := f.addAt(t, 3, 0.335, "0.000")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if out.Item.TotalPrice != 1.005 {
		t.Errorf("line total = %v, want 1.005 (3 × 0.335, rounded at three decimals)", out.Item.TotalPrice)
	}
}
