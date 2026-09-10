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
		ID            string `json:"ID"`
		Slug          string `json:"Slug"`
		Status        string `json:"Status"`
		Currency      string `json:"Currency"`
		CurrencyScale int32  `json:"CurrencyScale"`
		Timezone      string `json:"Timezone"`
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
		ID       string  `json:"id"`
		Quantity float64 `json:"quantity"`
	} `json:"movement"`
	Item *struct {
		Quantity float64 `json:"quantity"`
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
		// domain.CalvingRecord carries no json tags, so this is emitted as
		// "CalfGender". Go's decoder ignores case but not the underscore, so
		// `json:"calf_gender"` matches nothing and reads as empty. Written that
		// way this failed saying the value was "" — which is worth knowing,
		// because a field only ever compared against another empty string would
		// have passed and checked nothing.
		CalfGender string `json:"CalfGender"`
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
		svcclient.CallOptions{Tenant: stranger, Actor: "e2e"})
	if err == nil && len(theirs.Treatments) > 0 {
		t.Errorf("a second tenant reads %d treatments against an animal it has never "+
			"treated", len(theirs.Treatments))
	}
}
