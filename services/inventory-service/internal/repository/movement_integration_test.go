package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/exact"
	"github.com/ppusapati/gavya/libs/integrity/tenantctx"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"
	"github.com/ppusapati/gavya/services/inventory-service/internal/domain"
)

// These run the SQL rather than compiling it.
//
// Every property this file asserts — that a movement and its quantity commit
// together, that two concurrent movements do not lose one another, that stock
// stops at zero, that a running total stays exact — is a property of what
// PostgreSQL does with these statements. None of it can be shown by a test that
// substitutes a fake for the database.
//
//	TEST_DATABASE_DSN="postgres://user@host:port/%s?sslmode=disable" go test ./...
//
// The DSN carries a single %s where the database name goes.

const testDatabase = "inventory_repo_test"

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

	// This service's own schema, and the audit schema beside it: audit_logs
	// lives in audit-service's schema and deployment applies it to every
	// service's database. The moment this repository started recording what it
	// overwrote, a harness that applied only the first failed on a missing
	// table — less faithful than production in exactly the place the change
	// was made.
	for _, path := range []string{
		filepath.Join("..", "db", "schema.sql"),
		filepath.Join("..", "..", "..", "audit-service", "internal", "db", "schema.sql"),
	} {
		schema, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if _, err := pool.Exec(ctx, string(schema)); err != nil {
			t.Fatalf("apply %s: %v", path, err)
		}
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

// fixture is one tenant with one warehouse and one SKU, isolated from every
// other test by its identifiers.

// acting returns a context carrying what a real request carries.
//
// These tests called the repository with a bare context.Background(), which
// worked for as long as nothing on this path wrote an audit entry — and an audit
// entry is the one thing that refuses to be written without a tenant and an actor
// to attribute it to. A repository test that acts as nobody is testing the path a
// gateway never produces.
func (f *fixture) acting() context.Context {
	return tenantctx.WithActor(
		tenantdb.WithTenant(context.Background(), f.tenant),
		tenantctx.Actor{ID: "US_TEST_00000000000000000"},
	)
}

type fixture struct {
	repo      Repository
	tenant    string
	warehouse string
	sku       string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := testPool(t)
	f := &fixture{
		repo:      New(pool),
		tenant:    newTestID("tnt"),
		warehouse: newTestID("whs"),
		sku:       newTestID("sku"),
	}
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO warehouses (id,tenant_id,name,code,status,created_by,updated_by)
		 VALUES ($1,$2,'Test','`+newTestID("cd")+`','active',$3,$3)`,
		f.warehouse, f.tenant, newTestID("usr")); err != nil {
		t.Fatalf("create warehouse: %v", err)
	}
	return f
}

func (f *fixture) move(t *testing.T, kind domain.MovementType, q float64) (*MovementOutcome, error) {
	t.Helper()
	// The test values are typed as floats for brevity and rendered the way a
	// caller would type them, then held at the column's scale as the service
	// would hold them; a value the column cannot hold is a broken test.
	quantity, err := domain.AtColumn(exact.MustFixed(strconv.FormatFloat(q, 'f', -1, 64), domain.QuantityScale))
	if err != nil {
		t.Fatalf("quantity %v: %v", q, err)
	}
	actor := newTestID("usr")
	return f.repo.ApplyStockMovement(f.acting(), &domain.StockMovement{
		ID:           newTestID("mov"),
		TenantID:     f.tenant,
		WarehouseID:  f.warehouse,
		SKUID:        f.sku,
		MovementType: kind,
		Quantity:     quantity,
		MovedAt:      time.Now(),
		MovedBy:      actor,
		CreatedBy:    actor,
		UpdatedBy:    actor,
	}, newTestID("itm"))
}

func (f *fixture) onHand(t *testing.T) exact.Fixed {
	t.Helper()
	item, err := f.repo.GetInventoryItem(context.Background(), f.warehouse, f.sku, f.tenant)
	if err != nil {
		t.Fatalf("GetInventoryItem: %v", err)
	}
	return item.QuantityOnHand
}

// stock is a count the way the repository answers it: at the column's scale.
func stock(v float64) exact.Fixed {
	return exact.MustFixed(strconv.FormatFloat(v, 'f', int(domain.QuantityScale), 64), domain.QuantityScale)
}

func (f *fixture) movementCount(t *testing.T) int {
	t.Helper()
	list, err := f.repo.ListStockMovements(context.Background(), f.tenant, f.warehouse, 500, 0)
	if err != nil {
		t.Fatalf("ListStockMovements: %v", err)
	}
	return len(list)
}

func TestAMovementCreatesTheStockRowItNeeds(t *testing.T) {
	f := newFixture(t)

	out, err := f.move(t, domain.MovementIn, 10)
	if err != nil {
		t.Fatalf("in: %v", err)
	}
	if out.Item.QuantityOnHand != stock(10) {
		t.Errorf("on hand = %v, want 10", out.Item.QuantityOnHand)
	}
	if out.Movement.Quantity != stock(10) {
		t.Errorf("movement quantity = %v, want 10", out.Movement.Quantity)
	}
}

func TestMovementsAccumulate(t *testing.T) {
	f := newFixture(t)

	for _, step := range []struct {
		kind domain.MovementType
		q    float64
		want float64
	}{
		{domain.MovementIn, 10, 10},
		{domain.MovementIn, 5.5, 15.5},
		{domain.MovementOut, 3, 12.5},
		{domain.MovementTransfer, 2.5, 10},
		{domain.MovementAdjustment, 7, 7},
	} {
		out, err := f.move(t, step.kind, step.q)
		if err != nil {
			t.Fatalf("%s %v: %v", step.kind, step.q, err)
		}
		if out.Item.QuantityOnHand != stock(step.want) {
			t.Fatalf("after %s %v: on hand = %v, want %v", step.kind, step.q, out.Item.QuantityOnHand, step.want)
		}
	}
}

// An adjustment states the count after a stocktake, so it replaces the running
// total rather than moving it.
func TestAnAdjustmentSetsTheCountRatherThanChangingIt(t *testing.T) {
	f := newFixture(t)
	if _, err := f.move(t, domain.MovementIn, 100); err != nil {
		t.Fatal(err)
	}

	out, err := f.move(t, domain.MovementAdjustment, 3)
	if err != nil {
		t.Fatalf("adjustment: %v", err)
	}
	if out.Item.QuantityOnHand != stock(3) {
		t.Errorf("on hand = %v, want 3", out.Item.QuantityOnHand)
	}
}

func TestStockCannotGoBelowZero(t *testing.T) {
	f := newFixture(t)
	if _, err := f.move(t, domain.MovementIn, 5); err != nil {
		t.Fatal(err)
	}

	_, err := f.move(t, domain.MovementOut, 6)
	if !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("err = %v, want ErrInsufficientStock", err)
	}
	if on := f.onHand(t); on != stock(5) {
		t.Errorf("on hand = %v, want the refused movement to have changed nothing", on)
	}
}

func TestATransferCannotSendMoreThanIsHeld(t *testing.T) {
	f := newFixture(t)
	if _, err := f.move(t, domain.MovementIn, 2); err != nil {
		t.Fatal(err)
	}

	if _, err := f.move(t, domain.MovementTransfer, 2.001); !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("err = %v, want ErrInsufficientStock", err)
	}
}

func TestTakingFromAnEmptyShelfIsRefusedRatherThanCreatingDebt(t *testing.T) {
	f := newFixture(t)

	if _, err := f.move(t, domain.MovementOut, 1); !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("err = %v, want ErrInsufficientStock", err)
	}
}

// A refused movement must leave no trace. The old code wrote the movement row
// first and updated the quantity afterwards, so a movement could stand against
// stock that never moved.
func TestARefusedMovementIsNotRecorded(t *testing.T) {
	f := newFixture(t)
	if _, err := f.move(t, domain.MovementIn, 1); err != nil {
		t.Fatal(err)
	}
	before := f.movementCount(t)

	if _, err := f.move(t, domain.MovementOut, 99); !errors.Is(err, ErrInsufficientStock) {
		t.Fatalf("err = %v, want ErrInsufficientStock", err)
	}

	if after := f.movementCount(t); after != before {
		t.Errorf("%d movements recorded, want %d — a refused movement was written anyway", after, before)
	}
}

// The defect this replaced: two movements read the same quantity, each computed
// its own total in Go, and the second write overwrote the first. With twenty
// concurrent movements a lost update is close to certain.
func TestConcurrentMovementsDoNotLoseOneAnother(t *testing.T) {
	f := newFixture(t)
	if _, err := f.move(t, domain.MovementIn, 1000); err != nil {
		t.Fatal(err)
	}

	const movements = 20
	var wg sync.WaitGroup
	errs := make(chan error, movements)
	for i := 0; i < movements; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.move(t, domain.MovementOut, 1); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent out: %v", err)
	}

	if on := f.onHand(t); on != stock(float64(1000-movements)) {
		t.Errorf("on hand = %v, want %d — some movements were lost", on, 1000-movements)
	}
}

// Concurrency must not be able to drive stock negative either: with more takers
// than stock, some are refused and the shelf still ends at exactly zero.
func TestConcurrentMovementsCannotOversell(t *testing.T) {
	f := newFixture(t)
	if _, err := f.move(t, domain.MovementIn, 5); err != nil {
		t.Fatal(err)
	}

	const takers = 12
	var wg sync.WaitGroup
	var refused atomic.Int64
	for i := 0; i < takers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.move(t, domain.MovementOut, 1); err != nil {
				if !errors.Is(err, ErrInsufficientStock) {
					t.Errorf("unexpected error: %v", err)
				}
				refused.Add(1)
			}
		}()
	}
	wg.Wait()

	if on := f.onHand(t); !on.IsZero() {
		t.Errorf("on hand = %v, want 0", on)
	}
	if got := refused.Load(); got != takers-5 {
		t.Errorf("%d movements refused, want %d", got, takers-5)
	}
}

// Arithmetic in float64 drifts: adding 0.1 ten times leaves 0.9999999999999999.
// The database adds in NUMERIC, so a hundred movements of a tenth are ten.
func TestARunningTotalStaysExact(t *testing.T) {
	f := newFixture(t)

	for i := 0; i < 100; i++ {
		if _, err := f.move(t, domain.MovementIn, 0.1); err != nil {
			t.Fatalf("movement %d: %v", i, err)
		}
	}

	if on := f.onHand(t); on != stock(10) {
		t.Errorf("on hand = %v, want exactly 10.000", on)
	}
}

func TestAMovementAgainstAnUnknownWarehouseIsNamed(t *testing.T) {
	f := newFixture(t)
	f.warehouse = newTestID("gone")

	if _, err := f.move(t, domain.MovementIn, 1); !errors.Is(err, ErrUnknownWarehouse) {
		t.Fatalf("err = %v, want ErrUnknownWarehouse", err)
	}
}

// Stock is per tenant. One tenant's movements must not appear in another's
// count, however identical the warehouse and SKU look.
func TestStockIsHeldPerTenant(t *testing.T) {
	f := newFixture(t)
	if _, err := f.move(t, domain.MovementIn, 10); err != nil {
		t.Fatal(err)
	}

	other := f.tenant
	f.tenant = newTestID("tnt")
	defer func() { f.tenant = other }()

	if _, err := f.repo.GetInventoryItem(context.Background(), f.warehouse, f.sku, f.tenant); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want another tenant to see nothing", err)
	}
}

// A movement is recorded with the count it overwrote, as the literal the column
// holds, in the same transaction.
//
// Present is not the same as tested: a before-image written to the wrong place,
// or written empty, or written in a separate transaction that commits when the
// change does not, reads exactly like one that works. This reads the trail back
// out of audit_logs and requires the old count to be there, to differ from the
// new, and to be the exact decimal rather than a float rendering of it.
func TestAMovementRecordsTheCountItOverwrote(t *testing.T) {
	f := newFixture(t)
	pool := testPool(t)

	if _, err := f.move(t, domain.MovementIn, 12.5); err != nil {
		t.Fatalf("first movement: %v", err)
	}
	if _, err := f.move(t, domain.MovementIn, 7.25); err != nil {
		t.Fatalf("second movement: %v", err)
	}

	// The second movement's entry: it overwrote 12.500 with 19.750.
	var oldValue, newValue string
	if err := pool.QueryRow(context.Background(), `
		SELECT old_value::text, new_value::text
		  FROM audit_logs
		 WHERE tenant_id = $1 AND action = 'apply_stock_movement'
		 ORDER BY created_at DESC LIMIT 1`, f.tenant).Scan(&oldValue, &newValue); err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	if !strings.Contains(oldValue, `"12.5`) {
		t.Errorf("the before-image does not carry the count before the movement: %s", oldValue)
	}
	if !strings.Contains(newValue, `"19.75`) {
		t.Errorf("the after-image does not carry the count after the movement: %s", newValue)
	}
	// As a quoted decimal literal, not a bare float: the column is NUMERIC and
	// a trail of floats is a trail somebody can dispute on the third decimal.
	if strings.Contains(oldValue, `"quantity_on_hand":12.5`) {
		t.Errorf("the before-image renders the count as a float rather than the column's literal: %s", oldValue)
	}
}
