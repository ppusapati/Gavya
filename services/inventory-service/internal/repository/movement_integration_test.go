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

// fixture is one tenant with one warehouse and one SKU, isolated from every
// other test by its identifiers.
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
	quantity, err := domain.FormatQuantity(q)
	if err != nil {
		t.Fatalf("FormatQuantity(%v): %v", q, err)
	}
	actor := newTestID("usr")
	return f.repo.ApplyStockMovement(context.Background(), &domain.StockMovement{
		ID:           newTestID("mov"),
		TenantID:     f.tenant,
		WarehouseID:  f.warehouse,
		SKUID:        f.sku,
		MovementType: kind,
		Quantity:     q,
		MovedAt:      time.Now(),
		MovedBy:      actor,
		CreatedBy:    actor,
		UpdatedBy:    actor,
	}, quantity, newTestID("itm"))
}

func (f *fixture) onHand(t *testing.T) float64 {
	t.Helper()
	item, err := f.repo.GetInventoryItem(context.Background(), f.warehouse, f.sku, f.tenant)
	if err != nil {
		t.Fatalf("GetInventoryItem: %v", err)
	}
	return item.QuantityOnHand
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
	if out.Item.QuantityOnHand != 10 {
		t.Errorf("on hand = %v, want 10", out.Item.QuantityOnHand)
	}
	if out.Movement.Quantity != 10 {
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
		if out.Item.QuantityOnHand != step.want {
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
	if out.Item.QuantityOnHand != 3 {
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
	if on := f.onHand(t); on != 5 {
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

	if on := f.onHand(t); on != 1000-movements {
		t.Errorf("on hand = %v, want %d — %v movements were lost", on, 1000-movements, 1000-movements-on)
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

	if on := f.onHand(t); on != 0 {
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

	if on := f.onHand(t); on != 10 {
		t.Errorf("on hand = %.17g, want exactly 10", on)
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
