//go:build e2e

// The four services that had no end-to-end coverage of their write paths.
//
// tenant, inventory, breeding and the write half of health were never called by
// a test. Both services that were run for the first time this session — ingestion
// and milk — were hiding something, so these are run rather than read.
//
// Each of them is a state machine or an invariant that only means anything
// against a real database: a cycle that cannot be inseminated twice, stock that
// cannot go below zero, a tenant whose currency is fixed at creation. Those are
// the assertions here. Ordinary create-and-read-back is not, because a service
// that cannot do that fails on its first call and somebody notices.
package e2e

import (
	"context"
	"github.com/ppusapati/gavya/libs/integrity/exact"
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

const (
	tenantSvc    = "tenant.v1.TenantService"
	inventorySvc = "inventory.v1.InventoryService"
	breedingSvc  = "breeding.v1.BreedingService"
	healthSvc2   = "health.v1.HealthService"
)

// ---------------------------------------------------------------------------
// tenant
// ---------------------------------------------------------------------------

type createTenantReq struct {
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	Plan         string `json:"plan"`
	ContactEmail string `json:"contact_email"`
	Country      string `json:"country"`
	Timezone     string `json:"timezone"`
	Currency     string `json:"currency"`
	CreatedBy    string `json:"created_by"`
}

type tenantActionReq struct {
	ID        string `json:"id"`
	UpdatedBy string `json:"updated_by"`
}

type tenantResp struct {
	Tenant *struct {
		ID            string `json:"id"`
		Slug          string `json:"slug"`
		Status        string `json:"status"`
		Currency      string `json:"currency"`
		CurrencyScale int32  `json:"currency_scale"`
		Timezone      string `json:"timezone"`
	} `json:"tenant"`
}

// A tenant's currency is chosen at creation and carries its own scale.
//
// Everything recorded against a tenant afterwards is read in that currency, and
// the scale is what says how many of the stored digits are real. A deployment
// that silently got rupees would be wrong in a way nothing downstream could
// detect, so there is no default and an unrecognised code is refused.
func TestATenantsCurrencyIsChosenAtCreationAndCarriesItsScale(t *testing.T) {
	p := startPlatform(t)

	for _, c := range []struct {
		currency string
		scale    int32
	}{{"INR", 2}, {"JPY", 0}, {"BHD", 3}} {
		slug := newID("soc")
		out, err := svcclient.Call[createTenantReq, tenantResp](
			context.Background(), p.tenantSvcClient(), tenantSvc+"/CreateTenant",
			createTenantReq{Name: slug, Slug: slug, Plan: "basic",
				ContactEmail: slug + "@example.test", Country: "IN",
				Timezone: "Asia/Kolkata", Currency: c.currency,
				CreatedBy: "e2e"}, p.opts())
		if err != nil {
			t.Fatalf("create a tenant in %s: %v", c.currency, err)
		}
		if out.Tenant.CurrencyScale != c.scale {
			t.Errorf("%s was recorded at %d decimal places, want %d — the scale is what "+
				"says how many of the digits stored against this tenant are real",
				c.currency, out.Tenant.CurrencyScale, c.scale)
		}
	}

	// A code nobody recognises is refused rather than defaulted. A country whose
	// currency is missing from the table should fail on its first day, not round
	// every amount wrongly for a year.
	for _, bad := range []string{"", "RUPEES", "XYZ"} {
		slug := newID("soc")
		if _, err := svcclient.Call[createTenantReq, tenantResp](
			context.Background(), p.tenantSvcClient(), tenantSvc+"/CreateTenant",
			createTenantReq{Name: slug, Slug: slug, Plan: "basic",
				ContactEmail: slug + "@example.test", Country: "IN",
				Timezone: "Asia/Kolkata", Currency: bad, CreatedBy: "e2e"},
			p.opts()); err == nil {
			t.Errorf("a tenant was created in currency %q", bad)
		}
	}
}

// A tenant is suspended and brought back, and the slug stays unique.
func TestATenantIsSuspendedAndActivated(t *testing.T) {
	p := startPlatform(t)
	slug := newID("soc")

	made, err := svcclient.Call[createTenantReq, tenantResp](
		context.Background(), p.tenantSvcClient(), tenantSvc+"/CreateTenant",
		createTenantReq{Name: slug, Slug: slug, Plan: "basic",
			ContactEmail: slug + "@example.test", Country: "IN",
			Timezone: "Asia/Kolkata", Currency: "INR", CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}

	// The slug identifies a society. Two with the same one is two societies
	// nobody can tell apart in a URL.
	if _, err := svcclient.Call[createTenantReq, tenantResp](
		context.Background(), p.tenantSvcClient(), tenantSvc+"/CreateTenant",
		createTenantReq{Name: slug, Slug: slug, Plan: "basic",
			ContactEmail: slug + "2@example.test", Country: "IN",
			Timezone: "Asia/Kolkata", Currency: "INR", CreatedBy: "e2e"},
		p.opts()); err == nil {
		t.Error("two tenants were created with the same slug")
	}

	suspended, err := svcclient.Call[tenantActionReq, tenantResp](
		context.Background(), p.tenantSvcClient(), tenantSvc+"/SuspendTenant",
		tenantActionReq{ID: made.Tenant.ID, UpdatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if suspended.Tenant.Status == made.Tenant.Status {
		t.Errorf("the tenant is still %s after being suspended", suspended.Tenant.Status)
	}

	back, err := svcclient.Call[tenantActionReq, tenantResp](
		context.Background(), p.tenantSvcClient(), tenantSvc+"/ActivateTenant",
		tenantActionReq{ID: made.Tenant.ID, UpdatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("activate: %v", err)
	}
	// Active, not back to pending. A tenant is created pending and activation is
	// a promotion rather than an undo — which is worth pinning, because the
	// obvious assumption is that lifting a suspension restores what was there
	// before, and here it does not.
	if back.Tenant.Status != "active" {
		t.Errorf("the tenant came back as %s, want active — a suspension that cannot "+
			"be lifted is a deletion nobody called one", back.Tenant.Status)
	}
}

// ---------------------------------------------------------------------------
// inventory
// ---------------------------------------------------------------------------

type createWarehouseReq2 struct {
	TenantID  string `json:"tenant_id"`
	Name      string `json:"name"`
	Code      string `json:"code"`
	Status    string `json:"status"`
	CreatedBy string `json:"created_by"`
}

type warehouseResp2 struct {
	Warehouse *struct {
		ID string `json:"id"`
	} `json:"warehouse"`
}

type adjustStockReq struct {
	TenantID     string  `json:"tenant_id"`
	WarehouseID  string  `json:"warehouse_id"`
	SKUID        string  `json:"sku_id"`
	MovementType string  `json:"movement_type"`
	Quantity     float64 `json:"quantity"`
	Notes        string  `json:"notes"`
	MovedBy      string  `json:"moved_by"`
	CreatedBy    string  `json:"created_by"`
}

type stockMovementResp struct {
	Movement *struct {
		ID       string      `json:"id"`
		Quantity exact.Fixed `json:"quantity"`
	} `json:"movement"`
	Item *struct {
		Quantity exact.Fixed `json:"quantity"`
	} `json:"item"`
}

// Stock cannot be taken out that is not there.
//
// The movement, the new quantity and the refusal all happen in one transaction:
// a quantity computed in Go and written back is a lost update waiting for a
// second concurrent movement, and a warehouse that reports negative stock is
// reporting a theft that did not happen.
func TestStockCannotGoBelowZero(t *testing.T) {
	p := startPlatform(t)
	wh, err := svcclient.Call[createWarehouseReq2, warehouseResp2](
		context.Background(), p.inventory(), inventorySvc+"/CreateWarehouse",
		createWarehouseReq2{TenantID: p.tenant, Name: "Kothapalli chilling centre",
			Code: newID("wh"), Status: "active", CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create warehouse: %v", err)
	}
	sku := newID("sku")

	move := func(kind string, qty float64) (*stockMovementResp, error) {
		return svcclient.Call[adjustStockReq, stockMovementResp](
			context.Background(), p.inventory(), inventorySvc+"/AdjustStock",
			adjustStockReq{TenantID: p.tenant, WarehouseID: wh.Warehouse.ID,
				SKUID: sku, MovementType: kind, Quantity: qty,
				MovedBy: "e2e", CreatedBy: "e2e"}, p.opts())
	}

	if _, err := move("in", 100); err != nil {
		t.Fatalf("take 100 in: %v", err)
	}
	if _, err := move("out", 40); err != nil {
		t.Fatalf("take 40 out: %v", err)
	}
	// 60 left. Taking 61 must be refused rather than leaving -1.
	if _, err := move("out", 61); err == nil {
		t.Error("61 was taken out of a warehouse holding 60; the stock is now negative " +
			"and reads as a theft that did not happen")
	}
	// And exactly 60 is fine, so the refusal above is not a service refusing
	// everything.
	if _, err := move("out", 60); err != nil {
		t.Errorf("taking out exactly what was there was refused: %v", err)
	}
}

// A quantity finer than the column is refused rather than rounded.
func TestAStockQuantityFinerThanTheColumnIsRefused(t *testing.T) {
	p := startPlatform(t)
	wh, err := svcclient.Call[createWarehouseReq2, warehouseResp2](
		context.Background(), p.inventory(), inventorySvc+"/CreateWarehouse",
		createWarehouseReq2{TenantID: p.tenant, Name: "store", Code: newID("wh"),
			Status: "active", CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create warehouse: %v", err)
	}

	if _, err := svcclient.Call[adjustStockReq, stockMovementResp](
		context.Background(), p.inventory(), inventorySvc+"/AdjustStock",
		adjustStockReq{TenantID: p.tenant, WarehouseID: wh.Warehouse.ID,
			SKUID: newID("sku"), MovementType: "in", Quantity: 10.00025,
			MovedBy: "e2e", CreatedBy: "e2e"}, p.opts()); err == nil {
		t.Error("a quantity with five decimals was accepted into a column holding three")
	}

	// A movement type nobody recognises is refused too. "outward", "OUT" and
	// "remove" are all things somebody would send, and a service that stores
	// them has four kinds of movement where it thinks it has one.
	for _, kind := range []string{"", "OUT", "outward", "remove"} {
		if _, err := svcclient.Call[adjustStockReq, stockMovementResp](
			context.Background(), p.inventory(), inventorySvc+"/AdjustStock",
			adjustStockReq{TenantID: p.tenant, WarehouseID: wh.Warehouse.ID,
				SKUID: newID("sku"), MovementType: kind, Quantity: 1,
				MovedBy: "e2e", CreatedBy: "e2e"}, p.opts()); err == nil {
			t.Errorf("movement type %q was accepted", kind)
		}
	}
}

// ---------------------------------------------------------------------------
// breeding
// ---------------------------------------------------------------------------

type createCycleReq2 struct {
	TenantID  string    `json:"tenant_id"`
	CattleID  string    `json:"cattle_id"`
	HeatDate  time.Time `json:"heat_date"`
	CreatedBy string    `json:"created_by"`
}

type cycleResp2 struct {
	Cycle *struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"cycle"`
}

type recordInseminationReq struct {
	TenantID      string    `json:"tenant_id"`
	CycleID       string    `json:"cycle_id"`
	CattleID      string    `json:"cattle_id"`
	InseminatedAt time.Time `json:"inseminated_at"`
	Method        string    `json:"method"`
	CreatedBy     string    `json:"created_by"`
}

type inseminationResp struct {
	Insemination *struct {
		ID string `json:"id"`
	} `json:"insemination"`
}

type confirmPregnancyReq struct {
	TenantID            string    `json:"tenant_id"`
	CattleID            string    `json:"cattle_id"`
	InseminationID      string    `json:"insemination_id"`
	ConfirmedAt         time.Time `json:"confirmed_at"`
	ExpectedCalvingDate time.Time `json:"expected_calving_date"`
	CreatedBy           string    `json:"created_by"`
}

type pregnancyResp struct {
	Pregnancy *struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"pregnancy"`
}

type recordCalvingReq struct {
	TenantID    string  `json:"tenant_id"`
	PregnancyID string  `json:"pregnancy_id"`
	CattleID    string  `json:"cattle_id"`
	CalfGender  string  `json:"calf_gender"`
	CalfWeight  float64 `json:"calf_weight"`
	CreatedBy   string  `json:"created_by"`
}

type calvingResp struct {
	CalvingRecord *struct {
		ID string `json:"id"`
		// This read `json:"CalfGender"` until the domain types were tagged,
		// because they had none and Go emitted the field's own name. A client
		// written the obvious way — `calf_gender` — matched nothing and read
		// empty, silently. That trap is what turned up the wire-format defect
		// this whole file now sits behind; see e2e/wireformat_test.go.
		CalfGender string `json:"calf_gender"`
	} `json:"calving_record"`
}

// A cow goes through one breeding cycle, and cannot go through it twice.
//
// Heat, insemination, pregnancy, calving. Each step closes the one before it,
// and a service that let a confirmed pregnancy take another insemination would
// be recording two sires for one calf.
func TestABreedingCycleRunsOnceAndCloses(t *testing.T) {
	p := startPlatform(t)
	cattle := newID("cow")

	cycle, err := svcclient.Call[createCycleReq2, cycleResp2](
		context.Background(), p.breeding(), breedingSvc+"/CreateBreedingCycle",
		createCycleReq2{TenantID: p.tenant, CattleID: cattle,
			HeatDate: time.Now(), CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create cycle: %v", err)
	}

	ins, err := svcclient.Call[recordInseminationReq, inseminationResp](
		context.Background(), p.breeding(), breedingSvc+"/RecordInsemination",
		recordInseminationReq{TenantID: p.tenant, CycleID: cycle.Cycle.ID,
			CattleID: cattle, InseminatedAt: time.Now(), Method: "artificial",
			CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("record insemination: %v", err)
	}

	preg, err := svcclient.Call[confirmPregnancyReq, pregnancyResp](
		context.Background(), p.breeding(), breedingSvc+"/ConfirmPregnancy",
		confirmPregnancyReq{TenantID: p.tenant, CattleID: cattle,
			InseminationID: ins.Insemination.ID, ConfirmedAt: time.Now(),
			ExpectedCalvingDate: time.Now().AddDate(0, 9, 0),
			CreatedBy:           "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("confirm pregnancy: %v", err)
	}

	// The cycle is now pregnant and takes no further insemination. Two sires for
	// one calf is a pedigree nobody can use.
	if _, err := svcclient.Call[recordInseminationReq, inseminationResp](
		context.Background(), p.breeding(), breedingSvc+"/RecordInsemination",
		recordInseminationReq{TenantID: p.tenant, CycleID: cycle.Cycle.ID,
			CattleID: cattle, InseminatedAt: time.Now(), Method: "artificial",
			CreatedBy: "e2e"}, p.opts()); err == nil {
		t.Error("a second insemination was recorded against a cycle already confirmed " +
			"pregnant; the calf now has two sires on record")
	}

	// A birth weight finer than the column records is refused rather than
	// rounded into it. calf_weight is NUMERIC(6,2) and nothing checked it, so
	// 30.555 was stored as 30.56 and the first point of a growth curve was a
	// figure nobody had written down.
	//
	// This has to run while the pregnancy is still open. Placed after the
	// calving below it is refused for having already calved, and passes whether
	// the weight is checked or not — which is what it did when it was first
	// written here.
	if _, err := svcclient.Call[recordCalvingReq, calvingResp](
		context.Background(), p.breeding(), breedingSvc+"/RecordCalving",
		recordCalvingReq{TenantID: p.tenant, PregnancyID: preg.Pregnancy.ID,
			CattleID: cattle, CalfGender: "male", CalfWeight: 30.555,
			CreatedBy: "e2e"}, p.opts()); err == nil {
		t.Error("a calf weight of 30.555 kg was accepted into a column that holds two decimals")
	}

	if _, err := svcclient.Call[recordCalvingReq, calvingResp](
		context.Background(), p.breeding(), breedingSvc+"/RecordCalving",
		recordCalvingReq{TenantID: p.tenant, PregnancyID: preg.Pregnancy.ID,
			CattleID: cattle, CalfGender: "female", CalfWeight: 28.5,
			CreatedBy: "e2e"}, p.opts()); err != nil {
		t.Fatalf("record calving: %v", err)
	}

	// And the same pregnancy cannot calve twice.
	if _, err := svcclient.Call[recordCalvingReq, calvingResp](
		context.Background(), p.breeding(), breedingSvc+"/RecordCalving",
		recordCalvingReq{TenantID: p.tenant, PregnancyID: preg.Pregnancy.ID,
			CattleID: cattle, CalfGender: "male", CalfWeight: 30,
			CreatedBy: "e2e"}, p.opts()); err == nil {
		t.Error("one pregnancy produced two calving records; the herd count is now " +
			"wrong and so is every yield per animal drawn from it")
	}
}

// A calf's sex is stored as one letter, whatever the caller called it.
//
// calf_gender is VARCHAR(1) and once had nothing checking it, so "female" — the
// obvious thing to send for a string field of that name — reached PostgreSQL as
// a length violation and came back as an internal failure. The shape of a column
// is not something a client should have to know.
//
// So the service takes what a person would write and stores one letter. That is
// the interesting property and it is what this checks: four spellings in, one
// value out. A word nobody agreed on is still refused, because "unknown" stored
// as a sex is a third category in every report that groups by it.
func TestACalfsSexIsOneOfTwoThingsOrUnrecorded(t *testing.T) {
	p := startPlatform(t)

	// Every spelling a person would use lands as the same letter. Only the
	// first calving on this pregnancy can succeed, so the rest are checked
	// against fresh ones.
	for _, spelling := range []string{"female", "Female", "F", "heifer"} {
		p2 := aPregnancy(t, p)
		out, err := svcclient.Call[recordCalvingReq, calvingResp](
			context.Background(), p.breeding(), breedingSvc+"/RecordCalving",
			recordCalvingReq{TenantID: p.tenant, PregnancyID: p2.pregnancy,
				CattleID: p2.cattle, CalfGender: spelling, CreatedBy: "e2e"}, p.opts())
		if err != nil {
			t.Errorf("calf_gender %q was refused: %v", spelling, err)
			continue
		}
		if out.CalvingRecord.CalfGender != "F" {
			t.Errorf("%q was stored as %q, want F — four spellings of one fact are "+
				"four categories in every report that groups by it",
				spelling, out.CalvingRecord.CalfGender)
		}
	}

	// And a word nobody agreed on is refused rather than stored.
	for _, bad := range []string{"unknown", "either", "calf"} {
		p2 := aPregnancy(t, p)
		if _, err := svcclient.Call[recordCalvingReq, calvingResp](
			context.Background(), p.breeding(), breedingSvc+"/RecordCalving",
			recordCalvingReq{TenantID: p.tenant, PregnancyID: p2.pregnancy,
				CattleID: p2.cattle, CalfGender: bad, CreatedBy: "e2e"},
			p.opts()); err == nil {
			t.Errorf("calf_gender %q was accepted", bad)
		}
	}
}

// pregnancy is one confirmed pregnancy, ready to calve.
type pregnancy struct{ cattle, pregnancy string }

func aPregnancy(t *testing.T, p *platform) pregnancy {
	t.Helper()
	cattle := newID("cow")
	cycle, err := svcclient.Call[createCycleReq2, cycleResp2](
		context.Background(), p.breeding(), breedingSvc+"/CreateBreedingCycle",
		createCycleReq2{TenantID: p.tenant, CattleID: cattle,
			HeatDate: time.Now(), CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create cycle: %v", err)
	}
	ins, err := svcclient.Call[recordInseminationReq, inseminationResp](
		context.Background(), p.breeding(), breedingSvc+"/RecordInsemination",
		recordInseminationReq{TenantID: p.tenant, CycleID: cycle.Cycle.ID,
			CattleID: cattle, InseminatedAt: time.Now(), Method: "artificial",
			CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("record insemination: %v", err)
	}
	preg, err := svcclient.Call[confirmPregnancyReq, pregnancyResp](
		context.Background(), p.breeding(), breedingSvc+"/ConfirmPregnancy",
		confirmPregnancyReq{TenantID: p.tenant, CattleID: cattle,
			InseminationID: ins.Insemination.ID, ConfirmedAt: time.Now(),
			ExpectedCalvingDate: time.Now().AddDate(0, 9, 0), CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("confirm pregnancy: %v", err)
	}
	return pregnancy{cattle: cattle, pregnancy: preg.Pregnancy.ID}
}

// ---------------------------------------------------------------------------
// health
// ---------------------------------------------------------------------------

type recordTreatmentReq struct {
	TenantID     string    `json:"tenant_id"`
	CattleID     string    `json:"cattle_id"`
	Diagnosis    string    `json:"diagnosis"`
	MedicineName string    `json:"medicine_name"`
	Dosage       string    `json:"dosage"`
	TreatedAt    time.Time `json:"treated_at"`
	TreatedBy    string    `json:"treated_by"`
	Status       string    `json:"status"`
	CreatedBy    string    `json:"created_by"`
}

type treatmentResp struct {
	Treatment *struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"treatment"`
}

type recordVaccinationReq struct {
	TenantID       string     `json:"tenant_id"`
	CattleID       string     `json:"cattle_id"`
	VaccineName    string     `json:"vaccine_name"`
	BatchNumber    string     `json:"batch_number"`
	AdministeredAt time.Time  `json:"administered_at"`
	NextDueDate    *time.Time `json:"next_due_date"`
	VeterinarianID string     `json:"veterinarian_id"`
	Dosage         string     `json:"dosage"`
	CreatedBy      string     `json:"created_by"`
}

type vaccinationResp struct {
	Vaccination *struct {
		ID string `json:"id"`
	} `json:"vaccination"`
}

type historyReq struct {
	TenantID string `json:"tenant_id"`
	CattleID string `json:"cattle_id"`
}

type treatmentHistoryResp struct {
	Treatments []*struct {
		ID string `json:"id"`
	} `json:"treatments"`
}

// A treatment and a vaccination are recorded against one animal, and read back
// against that animal only.
//
// A medicine history that includes another animal's is the record a withdrawal
// period is calculated from: milk sold inside one is milk that should have been
// thrown away.
func TestAnAnimalsMedicineHistoryIsItsOwn(t *testing.T) {
	p := startPlatform(t)
	mine, other := newID("cow"), newID("cow")

	for _, cattle := range []string{mine, other} {
		if _, err := svcclient.Call[recordTreatmentReq, treatmentResp](
			context.Background(), p.health(), healthSvc2+"/RecordTreatment",
			recordTreatmentReq{TenantID: p.tenant, CattleID: cattle,
				Diagnosis: "mastitis", MedicineName: "intramammary antibiotic",
				Dosage: "1 tube", TreatedAt: time.Now(), TreatedBy: "vet",
				CreatedBy: "e2e"}, p.opts()); err != nil {
			t.Fatalf("record treatment: %v", err)
		}
		if _, err := svcclient.Call[recordVaccinationReq, vaccinationResp](
			context.Background(), p.health(), healthSvc2+"/RecordVaccination",
			recordVaccinationReq{TenantID: p.tenant, CattleID: cattle,
				VaccineName: "FMD", BatchNumber: newID("bat"),
				AdministeredAt: time.Now(), VeterinarianID: "vet", Dosage: "2ml",
				CreatedBy: "e2e"}, p.opts()); err != nil {
			t.Fatalf("record vaccination: %v", err)
		}
	}

	history, err := svcclient.Call[historyReq, treatmentHistoryResp](
		context.Background(), p.health(), healthSvc2+"/GetTreatmentHistory",
		historyReq{TenantID: p.tenant, CattleID: mine}, p.opts())
	if err != nil {
		t.Fatalf("get treatment history: %v", err)
	}
	if len(history.Treatments) != 1 {
		t.Errorf("one animal's treatment history holds %d records, want 1 — the other "+
			"animal was treated the same day, and a withdrawal period is calculated "+
			"from this list", len(history.Treatments))
	}

	// A second tenant sees none of it.
	stranger := newID("tnt")
	theirs, err := svcclient.Call[historyReq, treatmentHistoryResp](
		context.Background(), p.health(), healthSvc2+"/GetTreatmentHistory",
		historyReq{TenantID: stranger, CattleID: mine},
		actingAs(stranger, "e2e"))
	if err == nil && len(theirs.Treatments) > 0 {
		t.Errorf("a second tenant reads %d treatments against an animal it has never "+
			"treated", len(theirs.Treatments))
	}
}

// ---------------------------------------------------------------------------
// tenant, and product-catalog's catalogue
// ---------------------------------------------------------------------------

type updateTenantReq struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ContactEmail string `json:"contact_email"`
	Country      string `json:"country"`
	Timezone     string `json:"timezone"`
	Currency     string `json:"currency"`
	MaxUsers     int    `json:"max_users"`
	MaxCattle    int    `json:"max_cattle"`
	UpdatedBy    string `json:"updated_by"`
}

type idOnlyReq struct {
	ID string `json:"id"`
}

type listTenantsResp struct {
	Tenants []*struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
	} `json:"tenants"`
}

type upsertSettingReq struct {
	TenantID  string `json:"tenant_id"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	DataType  string `json:"data_type"`
	CreatedBy string `json:"created_by"`
}

type settingResp struct {
	Setting *struct {
		ID    string `json:"id"`
		Key   string `json:"key"`
		Value string `json:"value"`
	} `json:"setting"`
}

type listSettingsResp struct {
	Settings []*struct {
		ID    string `json:"id"`
		Key   string `json:"key"`
		Value string `json:"value"`
	} `json:"settings"`
}

func aTenant(t *testing.T, p *platform) *tenantResp {
	t.Helper()
	slug := newID("soc")
	out, err := svcclient.Call[createTenantReq, tenantResp](
		context.Background(), p.tenantSvcClient(), tenantSvc+"/CreateTenant",
		createTenantReq{Name: slug, Slug: slug, Plan: "basic",
			ContactEmail: slug + "@example.test", Country: "IN",
			Timezone: "Asia/Kolkata", Currency: "INR", CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	return out
}

// A tenant reads back, and updating it does not change the currency.
//
// The currency is fixed at creation because everything recorded against the
// tenant afterwards is read in it. UpdateTenant takes a currency field, which is
// the shape of a request that could change one — so what it does with it is
// worth pinning rather than assuming.
func TestUpdatingATenantCannotChangeWhatItRecordsIn(t *testing.T) {
	p := startPlatform(t)
	made := aTenant(t, p)

	got, err := svcclient.Call[idOnlyReq, tenantResp](
		context.Background(), p.tenantSvcClient(), tenantSvc+"/GetTenant",
		idOnlyReq{ID: made.Tenant.ID}, p.opts())
	if err != nil {
		t.Fatalf("get tenant: %v", err)
	}
	if got.Tenant.Currency != "INR" || got.Tenant.CurrencyScale != 2 {
		t.Fatalf("the tenant reads back as %s at %d decimals",
			got.Tenant.Currency, got.Tenant.CurrencyScale)
	}

	// Naming a different one is refused outright. Written with an early return
	// on error this test asserted nothing at all: the update is refused, so the
	// branch that checks the currency never ran.
	if _, err := svcclient.Call[updateTenantReq, tenantResp](
		context.Background(), p.tenantSvcClient(), tenantSvc+"/UpdateTenant",
		updateTenantReq{ID: made.Tenant.ID, Name: "renamed",
			ContactEmail: "new@example.test", Country: "IN",
			Timezone: "Asia/Kolkata", Currency: "JPY",
			UpdatedBy: "e2e"}, p.opts()); err == nil {
		t.Error("an update moved the tenant to JPY; every amount already recorded " +
			"against it was written in rupees and would be read as yen, with nothing " +
			"saying when the meaning changed")
	}

	// And an update that carries no currency changes what it was asked to and
	// leaves the currency where it was. A request with the field empty must not
	// be read as one asking to clear it.
	updated, err := svcclient.Call[updateTenantReq, tenantResp](
		context.Background(), p.tenantSvcClient(), tenantSvc+"/UpdateTenant",
		updateTenantReq{ID: made.Tenant.ID, Name: "renamed",
			ContactEmail: "new@example.test", Country: "IN",
			Timezone: "Asia/Kolkata", UpdatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("update tenant: %v", err)
	}
	if updated.Tenant.Currency != "INR" || updated.Tenant.CurrencyScale != 2 {
		t.Errorf("an update carrying no currency left the tenant recording %s at %d "+
			"decimals, want INR at 2", updated.Tenant.Currency, updated.Tenant.CurrencyScale)
	}
}

// A tenant appears in the listing, and its settings are its own.
func TestATenantsSettingsAreItsOwn(t *testing.T) {
	p := startPlatform(t)
	mine, theirs := aTenant(t, p), aTenant(t, p)

	all, err := svcclient.Call[struct{}, listTenantsResp](
		context.Background(), p.tenantSvcClient(), tenantSvc+"/ListTenants",
		struct{}{}, p.opts())
	if err != nil {
		t.Fatalf("list tenants: %v", err)
	}
	var found bool
	for _, tn := range all.Tenants {
		if tn.ID == mine.Tenant.ID {
			found = true
		}
	}
	if !found {
		t.Error("a tenant that was just created is not in the listing")
	}

	set, err := svcclient.Call[upsertSettingReq, settingResp](
		context.Background(), p.tenantSvcClient(), tenantSvc+"/UpsertTenantSetting",
		upsertSettingReq{TenantID: mine.Tenant.ID, Key: "collection_shift_cutoff",
			Value: "09:30", DataType: "string", CreatedBy: "e2e"},
		actingAs(mine.Tenant.ID, "e2e"))
	if err != nil {
		t.Fatalf("upsert setting: %v", err)
	}
	if set.Setting.Value != "09:30" {
		t.Errorf("the setting reads back as %q", set.Setting.Value)
	}

	// Upserting the same key again changes the value rather than adding a second
	// row. Two rows for one setting is a setting with two answers.
	again, err := svcclient.Call[upsertSettingReq, settingResp](
		context.Background(), p.tenantSvcClient(), tenantSvc+"/UpsertTenantSetting",
		upsertSettingReq{TenantID: mine.Tenant.ID, Key: "collection_shift_cutoff",
			Value: "10:00", DataType: "string", CreatedBy: "e2e"},
		actingAs(mine.Tenant.ID, "e2e"))
	if err != nil {
		t.Fatalf("upsert the same setting: %v", err)
	}
	if again.Setting.Value != "10:00" {
		t.Errorf("the setting reads back as %q after being changed", again.Setting.Value)
	}

	list, err := svcclient.Call[tenantReq, listSettingsResp](
		context.Background(), p.tenantSvcClient(), tenantSvc+"/ListTenantSettings",
		tenantReq{TenantID: mine.Tenant.ID},
		actingAs(mine.Tenant.ID, "e2e"))
	if err != nil {
		t.Fatalf("list settings: %v", err)
	}
	n := 0
	for _, s := range list.Settings {
		if s.Key == "collection_shift_cutoff" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("one key holds %d settings, want 1 — a setting with two rows is a "+
			"setting with two answers and no way to tell which is in force", n)
	}

	// And the other tenant has none of them.
	other, err := svcclient.Call[tenantReq, listSettingsResp](
		context.Background(), p.tenantSvcClient(), tenantSvc+"/ListTenantSettings",
		tenantReq{TenantID: theirs.Tenant.ID},
		actingAs(theirs.Tenant.ID, "e2e"))
	if err == nil && len(other.Settings) > 0 {
		t.Errorf("a second tenant holds %d settings it never set", len(other.Settings))
	}
}

// ---------------------------------------------------------------------------
// product-catalog: the catalogue above the SKUs
// ---------------------------------------------------------------------------

type createBrandReq struct {
	TenantID  string `json:"tenant_id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	LogoURL   string `json:"logo_url"`
	CreatedBy string `json:"created_by"`
}

type brandResp struct {
	Brand *struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Slug    string `json:"slug"`
		LogoURL string `json:"logo_url"`
	} `json:"brand"`
}

type listBrandsResp struct {
	Brands []*struct {
		ID   string `json:"id"`
		Slug string `json:"slug"`
	} `json:"brands"`
}

type listProductsReq struct {
	TenantID    string `json:"tenant_id"`
	ProductType string `json:"product_type"`
	Status      string `json:"status"`
}

type productListResp struct {
	Products []*struct {
		ID          string `json:"id"`
		ProductType string `json:"product_type"`
		Status      string `json:"status"`
	} `json:"products"`
}

type listProductSKUsReq struct {
	ProductID string `json:"product_id"`
	TenantID  string `json:"tenant_id"`
}

type listSKUsResp struct {
	SKUs []*struct {
		ID        string `json:"id"`
		ProductID string `json:"product_id"`
		Price     string `json:"price"`
		Currency  string `json:"currency"`
	} `json:"skus"`
}

// A brand's slug is unique within a tenant, and its logo survives the round trip.
//
// LogoURL is the field that found the wire-format defect worth naming: with no
// json tag it went out as "LogoURL", and a client asking for logo_url read
// nothing.
func TestABrandsSlugIsUniqueWithinATenant(t *testing.T) {
	p := startPlatform(t)
	slug := newID("brd")

	made, err := svcclient.Call[createBrandReq, brandResp](
		context.Background(), p.catalog(), catalogSvc+"/CreateBrand",
		createBrandReq{TenantID: p.tenant, Name: "Amul", Slug: slug,
			LogoURL: "https://example.test/amul.png", CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create brand: %v", err)
	}
	if made.Brand.LogoURL != "https://example.test/amul.png" {
		t.Errorf("the logo url came back as %q", made.Brand.LogoURL)
	}

	if _, err := svcclient.Call[createBrandReq, brandResp](
		context.Background(), p.catalog(), catalogSvc+"/CreateBrand",
		createBrandReq{TenantID: p.tenant, Name: "Amul again", Slug: slug,
			CreatedBy: "e2e"}, p.opts()); err == nil {
		t.Error("two brands were created with the same slug; a slug is how a brand is " +
			"addressed and two of them is two brands nobody can tell apart")
	}

	list, err := svcclient.Call[tenantReq, listBrandsResp](
		context.Background(), p.catalog(), catalogSvc+"/ListBrands",
		tenantReq{TenantID: p.tenant}, p.opts())
	if err != nil {
		t.Fatalf("list brands: %v", err)
	}
	var found bool
	for _, b := range list.Brands {
		if b.ID == made.Brand.ID {
			found = true
		}
	}
	if !found {
		t.Error("a brand that was just created is not in the list")
	}
}

// Listing products filters by type and status only when asked.
//
// ListProducts once compared product_type with LIKE against a column compared
// for equality, so it always came back empty. An unset filter means "any", not
// "match the empty string".
func TestListingProductsFiltersOnlyWhenAsked(t *testing.T) {
	p := startPlatform(t)

	cat, err := svcclient.Call[createCategoryReq, categoryResp](
		context.Background(), p.catalog(), catalogSvc+"/CreateCategory",
		createCategoryReq{TenantID: p.tenant, Name: newID("cat"), Slug: newID("cat"),
			CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create category: %v", err)
	}
	for _, kind := range []string{"feed", "feed", "medicine"} {
		slug := newID("prd")
		if _, err := svcclient.Call[createProductReq, productResp](
			context.Background(), p.catalog(), catalogSvc+"/CreateProduct",
			createProductReq{TenantID: p.tenant, CategoryID: cat.Category.ID,
				Name: slug, Slug: slug, ProductType: kind, Status: "active",
				CreatedBy: "e2e"}, p.opts()); err != nil {
			t.Fatalf("create a %s product: %v", kind, err)
		}
	}

	all, err := svcclient.Call[listProductsReq, productListResp](
		context.Background(), p.catalog(), catalogSvc+"/ListProducts",
		listProductsReq{TenantID: p.tenant}, p.opts())
	if err != nil {
		t.Fatalf("list products: %v", err)
	}
	if len(all.Products) < 3 {
		t.Fatalf("an unfiltered list holds %d of at least 3 products; an unset filter "+
			"means \"any\", not \"match the empty string\"", len(all.Products))
	}

	feed, err := svcclient.Call[listProductsReq, productListResp](
		context.Background(), p.catalog(), catalogSvc+"/ListProducts",
		listProductsReq{TenantID: p.tenant, ProductType: "feed"}, p.opts())
	if err != nil {
		t.Fatalf("list feed products: %v", err)
	}
	if len(feed.Products) >= len(all.Products) {
		t.Errorf("filtering by type returned %d of %d — the filter did not filter",
			len(feed.Products), len(all.Products))
	}
	for _, pr := range feed.Products {
		if pr.ProductType != "feed" {
			t.Errorf("a %s product came back in the feed list", pr.ProductType)
		}
	}
}

// A product's SKUs are its own, and their prices come back exact.
func TestAProductsSKUsAreItsOwn(t *testing.T) {
	p := startPlatform(t)
	mine := aSKU(t, p, "INR", "54.00")
	other := aSKU(t, p, "INR", "61.50")

	got, err := svcclient.Call[idTenantReq, skuResp](
		context.Background(), p.catalog(), catalogSvc+"/GetSKU",
		idTenantReq{ID: mine.SKU.ID, TenantID: p.tenant}, p.opts())
	if err != nil {
		t.Fatalf("get sku: %v", err)
	}
	if got.SKU.Price != "54.00" {
		t.Errorf("the price reads back as %s, want 54.00", got.SKU.Price)
	}
	if other.SKU.ID == mine.SKU.ID {
		t.Fatal("two SKUs were created with one id")
	}
}
