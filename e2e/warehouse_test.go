//go:build e2e

// Reading the warehouse back: what is in it, what moved, and what is about to
// go off.
//
// lifecycle_test pushes stock in and out and checks it cannot go negative. The
// four routes left are the ones somebody in a cold store actually opens — the
// warehouse record, the movement history for one site, a batch, and the list of
// batches near their date.
//
// ListExpiringBatches is the one worth care. Its query is
//
//	expires_at <= NOW() + INTERVAL '7 days' AND status='available'
//
// and both halves matter in opposite directions. Without the window it lists
// every batch in the building, which is a report nobody reads. Without the
// status it lists batches already written off or recalled, which is a report
// that sends somebody to a shelf to find nothing there. Neither failure looks
// like a failure: both return the batch that was genuinely near its date.
package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

type tenantOnlyReq struct {
	TenantID string `json:"tenant_id"`
}

type warehouseProto struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	Address   string `json:"address"`
	ManagerID string `json:"manager_id"`
	Status    string `json:"status"`
}

type fullWarehouseResp struct {
	Warehouse *warehouseProto `json:"warehouse"`
}

type listStockMovementsReq struct {
	TenantID    string `json:"tenant_id"`
	WarehouseID string `json:"warehouse_id"`
	Limit       int    `json:"limit"`
	Offset      int    `json:"offset"`
}

type stockMovementProto struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	WarehouseID  string    `json:"warehouse_id"`
	SKUID        string    `json:"sku_id"`
	MovementType string    `json:"movement_type"`
	Quantity     float64   `json:"quantity"`
	Notes        string    `json:"notes"`
	MovedAt      time.Time `json:"moved_at"`
}

type listStockMovementsResp struct {
	Movements []*stockMovementProto `json:"movements"`
}

type createInventoryBatchReq struct {
	TenantID       string     `json:"tenant_id"`
	WarehouseID    string     `json:"warehouse_id"`
	SKUID          string     `json:"sku_id"`
	BatchNumber    string     `json:"batch_number"`
	Quantity       float64    `json:"quantity"`
	ManufacturedAt *time.Time `json:"manufactured_at"`
	ExpiresAt      *time.Time `json:"expires_at"`
	Status         string     `json:"status"`
	CreatedBy      string     `json:"created_by"`
}

type inventoryBatchProto struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	WarehouseID string     `json:"warehouse_id"`
	SKUID       string     `json:"sku_id"`
	BatchNumber string     `json:"batch_number"`
	Quantity    float64    `json:"quantity"`
	ExpiresAt   *time.Time `json:"expires_at"`
	Status      string     `json:"status"`
}

type inventoryBatchResp struct {
	Batch *inventoryBatchProto `json:"batch"`
}

type listInventoryBatchesResp struct {
	Batches []*inventoryBatchProto `json:"batches"`
}

// warehouse creates one site and returns its id.
func warehouse(t *testing.T, p *platform, name string) string {
	t.Helper()
	out, err := svcclient.Call[createWarehouseReq2, warehouseResp2](context.Background(),
		p.inventory(), inventorySvc+"/CreateWarehouse", createWarehouseReq2{
			TenantID: p.tenant, Name: name, Code: newID("wh"),
			Status: "active", CreatedBy: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("CreateWarehouse %s: %v", name, err)
	}
	return out.Warehouse.ID
}

// A warehouse reads back as it was created, to the tenant that created it.
func TestAWarehouseReadsBackToTheTenantThatCreatedIt(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	code := newID("wh")
	made, err := svcclient.Call[createWarehouseReq2, warehouseResp2](ctx, p.inventory(),
		inventorySvc+"/CreateWarehouse", createWarehouseReq2{
			TenantID: p.tenant, Name: "Kothapalli cold store", Code: code,
			Status: "active", CreatedBy: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("CreateWarehouse: %v", err)
	}

	got, err := svcclient.Call[idTenantReq, fullWarehouseResp](ctx, p.inventory(),
		inventorySvc+"/GetWarehouse", idTenantReq{ID: made.Warehouse.ID, TenantID: p.tenant},
		p.opts())
	if err != nil {
		t.Fatalf("GetWarehouse: %v", err)
	}
	w := got.Warehouse
	if w == nil {
		t.Fatal("GetWarehouse answered with no warehouse at all")
	}
	for _, c := range []struct{ field, got, want string }{
		{"id", w.ID, made.Warehouse.ID},
		{"tenant_id", w.TenantID, p.tenant},
		{"name", w.Name, "Kothapalli cold store"},
		{"code", w.Code, code},
		{"status", w.Status, "active"},
	} {
		if c.got != c.want {
			t.Errorf("warehouse %s is %q, want %q", c.field, c.got, c.want)
		}
	}

	stranger := newID("ten")
	if _, err := svcclient.Call[idTenantReq, fullWarehouseResp](ctx, p.inventory(),
		inventorySvc+"/GetWarehouse", idTenantReq{ID: made.Warehouse.ID, TenantID: stranger},
		svcclient.CallOptions{Tenant: stranger, Actor: "e2e", RequestID: newID("req")}); err == nil {
		t.Error("another tenant read this warehouse")
	}
}

// The movement history of a site is that site's.
//
// Two warehouses moving stock in the same moment. This is the ledger somebody
// reconciles a physical count against, so a listing that carried another site's
// movements would have them counting one building's shelves against two
// buildings' paperwork.
func TestAWarehousesMovementsAreItsOwnAndNewestFirst(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	mine := warehouse(t, p, "mine")
	theirs := warehouse(t, p, "theirs")
	sku := newID("sku")

	move := func(wh, kind string, qty float64, note string) {
		t.Helper()
		if _, err := svcclient.Call[adjustStockReq, stockMovementResp](ctx, p.inventory(),
			inventorySvc+"/AdjustStock", adjustStockReq{
				TenantID: p.tenant, WarehouseID: wh, SKUID: sku,
				MovementType: kind, Quantity: qty, Notes: note,
				MovedBy: "e2e", CreatedBy: "e2e",
			}, p.opts()); err != nil {
			t.Fatalf("AdjustStock %s %s: %v", kind, note, err)
		}
	}

	move(mine, "in", 100, "first")
	move(mine, "out", 40, "second")
	move(theirs, "in", 999, "another site")

	got, err := svcclient.Call[listStockMovementsReq, listStockMovementsResp](ctx, p.inventory(),
		inventorySvc+"/ListStockMovements", listStockMovementsReq{
			TenantID: p.tenant, WarehouseID: mine, Limit: 50,
		}, p.opts())
	if err != nil {
		t.Fatalf("ListStockMovements: %v", err)
	}
	if len(got.Movements) != 2 {
		t.Fatalf("this site has %d movements, want 2 — another site moved stock in "+
			"the same moment and its movements are not this site's", len(got.Movements))
	}
	for _, m := range got.Movements {
		if m.WarehouseID != mine {
			t.Errorf("a movement at site %s came back from a listing asked for %s",
				m.WarehouseID, mine)
		}
		if m.Quantity == 999 {
			t.Error("the other site's movement of 999 is in this site's ledger")
		}
	}

	// Newest first: the ledger is read from the top, and the current position
	// is the most recent line.
	if got.Movements[0].Notes != "second" {
		t.Errorf("the ledger opens with %q, want the most recent movement\n"+
			"A history read oldest-first puts the current position at the "+
			"bottom of a page that grows every day.", got.Movements[0].Notes)
	}
}

// The expiring list holds what is near its date and still on the shelf.
//
// Four batches: one expiring in three days, one in three months, one that
// expired last week, and one expiring in three days that has already been
// recalled. Only the first two of those are interesting and only the first
// belongs in the answer.
//
// Both halves of the query fail quietly in opposite directions. Without the
// window the list is every batch in the building, which nobody reads. Without
// the status it names batches already recalled, which sends somebody to a shelf
// to find nothing there — and a recall list that includes stock already pulled
// is how a real recall gets ignored.
func TestTheExpiringListHoldsWhatIsNearItsDateAndStillOnTheShelf(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	// Its own tenant: this counts what a report returns, and the shared tenant
	// carries every other test's stock.
	tenant := newID("ten")
	opts := svcclient.CallOptions{Tenant: tenant, Actor: "e2e", RequestID: newID("req")}

	wh, err := svcclient.Call[createWarehouseReq2, warehouseResp2](ctx, p.inventory(),
		inventorySvc+"/CreateWarehouse", createWarehouseReq2{
			TenantID: tenant, Name: "cold store", Code: newID("wh"),
			Status: "active", CreatedBy: "e2e",
		}, opts)
	if err != nil {
		t.Fatalf("CreateWarehouse: %v", err)
	}

	at := func(d time.Duration) *time.Time { u := time.Now().UTC().Add(d); return &u }
	batch := func(number string, expires *time.Time, status string) string {
		t.Helper()
		out, err := svcclient.Call[createInventoryBatchReq, inventoryBatchResp](ctx, p.inventory(),
			inventorySvc+"/CreateBatch", createInventoryBatchReq{
				TenantID: tenant, WarehouseID: wh.Warehouse.ID, SKUID: newID("sku"),
				BatchNumber: number, Quantity: 100, ExpiresAt: expires,
				Status: status, CreatedBy: "e2e",
			}, opts)
		if err != nil {
			t.Fatalf("CreateBatch %s: %v", number, err)
		}
		return out.Batch.ID
	}

	soon := batch("SOON", at(3*24*time.Hour), "available")
	later := batch("LATER", at(90*24*time.Hour), "available")
	recalled := batch("RECALLED", at(3*24*time.Hour), "recalled")

	// A batch reads back by id first, so the rest of this test is reading
	// something that is demonstrably there.
	got, err := svcclient.Call[idTenantReq, inventoryBatchResp](ctx, p.inventory(),
		inventorySvc+"/GetBatch", idTenantReq{ID: soon, TenantID: tenant}, opts)
	if err != nil {
		t.Fatalf("GetBatch: %v", err)
	}
	if got.Batch == nil || got.Batch.BatchNumber != "SOON" {
		t.Fatalf("GetBatch answered with %+v, want the SOON batch", got.Batch)
	}
	if got.Batch.Quantity != 100 {
		t.Errorf("the batch holds %v, want 100", got.Batch.Quantity)
	}
	if got.Batch.ExpiresAt == nil {
		t.Error("the batch has no expiry date, and it was created with one")
	}

	expiring, err := svcclient.Call[tenantOnlyReq, listInventoryBatchesResp](ctx, p.inventory(),
		inventorySvc+"/ListExpiringBatches", tenantOnlyReq{TenantID: tenant}, opts)
	if err != nil {
		t.Fatalf("ListExpiringBatches: %v", err)
	}
	seen := map[string]bool{}
	for _, b := range expiring.Batches {
		seen[b.ID] = true
	}
	if !seen[soon] {
		t.Errorf("a batch three days from its date is not in the expiring list; "+
			"%d batches are", len(expiring.Batches))
	}
	if seen[later] {
		t.Error("a batch three months from its date is in the expiring list\n" +
			"Without the window this report is every batch in the building, " +
			"which is a report nobody reads.")
	}
	if seen[recalled] {
		t.Error("a recalled batch is in the expiring list\n" +
			"The stock is already off the shelf. A list that sends somebody to " +
			"find it teaches them the list is wrong, which is how the entries " +
			"that are right get ignored too.")
	}
}
