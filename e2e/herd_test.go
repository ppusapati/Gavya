//go:build e2e

// The animal's own record: what it eats, what it is, what it carries, what it
// has been given.
//
// feed, cattle, breeding and health each had a handful of routes nobody had
// called. Between them they are one animal's file — the nutrition plan, the
// breed, the breeding history, the vaccination history — plus the two standing
// queues a farm works from: which cows are in calf, and which vaccinations fall
// due this week.
//
// Both of those queues are the interesting ones, and they fail in the same
// direction. ListActivePregnancies filters on status='active'; without it,
// every pregnancy that ever ended is still listed as ongoing. And
// ListUpcomingVaccinations takes a window on both sides —
//
//	next_due_date >= NOW() AND next_due_date <= NOW() + INTERVAL '7 days'
//
// — so dropping the lower bound fills this week's round with doses that were
// missed months ago, and dropping the upper fills it with next year's. Each
// still contains the ones genuinely due, so each looks like a working list.
package e2e

import (
	"context"
	"github.com/ppusapati/gavya/libs/integrity/exact"
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

const (
	feedSvc      = "feed.v1.FeedService"
	cattleSvc    = "cattle.v1.CattleService"
	breedingSvc2 = "breeding.v1.BreedingService"
)

type createNutritionPlanReq struct {
	TenantID        string     `json:"tenant_id"`
	CattleID        string     `json:"cattle_id"`
	FeedTypeID      string     `json:"feed_type_id"`
	DailyQuantityKg float64    `json:"daily_quantity_kg"`
	StartDate       time.Time  `json:"start_date"`
	EndDate         *time.Time `json:"end_date"`
	Notes           string     `json:"notes"`
	CreatedBy       string     `json:"created_by"`
}

type nutritionPlanProto struct {
	ID              string      `json:"id"`
	TenantID        string      `json:"tenant_id"`
	CattleID        string      `json:"cattle_id"`
	FeedTypeID      string      `json:"feed_type_id"`
	DailyQuantityKg exact.Fixed `json:"daily_quantity_kg"`
	Notes           string      `json:"notes"`
}

type nutritionPlanResp struct {
	NutritionPlan *nutritionPlanProto `json:"nutrition_plan"`
}

type getNutritionPlanReq struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

type recordFeedConsumptionReq struct {
	TenantID   string    `json:"tenant_id"`
	CattleID   string    `json:"cattle_id"`
	FeedTypeID string    `json:"feed_type_id"`
	QuantityKg float64   `json:"quantity_kg"`
	FedAt      time.Time `json:"fed_at"`
	FedBy      string    `json:"fed_by"`
	CreatedBy  string    `json:"created_by"`
}

type feedConsumptionResp struct {
	Consumption *struct {
		ID string `json:"id"`
	} `json:"consumption"`
}

type feedReportReq struct {
	TenantID string    `json:"tenant_id"`
	CattleID string    `json:"cattle_id"`
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
}

type feedReportEntry struct {
	CattleID   string      `json:"cattle_id"`
	FeedTypeID string      `json:"feed_type_id"`
	TotalKg    exact.Fixed `json:"total_kg"`
}

type feedReportResp struct {
	Entries []*feedReportEntry `json:"entries"`
}

type updateCattleReq struct {
	ID        string  `json:"id"`
	TenantID  string  `json:"tenant_id"`
	Status    string  `json:"status"`
	Weight    float64 `json:"weight"`
	UpdatedBy string  `json:"updated_by"`
}

type updateCattleResp struct {
	Cattle *struct {
		ID     string  `json:"id"`
		Status string  `json:"status"`
		Weight float64 `json:"weight"`
	} `json:"cattle"`
}

type createBreedReq struct {
	TenantID    string `json:"tenant_id"`
	Name        string `json:"name"`
	Origin      string `json:"origin"`
	Description string `json:"description"`
	CreatedBy   string `json:"created_by"`
}

type breedProto struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	Name        string `json:"name"`
	Origin      string `json:"origin"`
	Description string `json:"description"`
}

type createBreedResp struct {
	Breed *breedProto `json:"breed"`
}

type listBreedsResp struct {
	Breeds []*breedProto `json:"breeds"`
}

type breedingCycleProto struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	CattleID string `json:"cattle_id"`
	Status   string `json:"status"`
	Notes    string `json:"notes"`
}

// fullBreedingHistoryResp reads the cow each cycle belongs to, which
// erp_test's breedingHistoryResp does not carry — and which is the whole
// question when two cows are served in the same moment.
type fullBreedingHistoryResp struct {
	Cycles []*breedingCycleProto `json:"cycles"`
}

type pregnancyProto struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	CattleID string `json:"cattle_id"`
	Status   string `json:"status"`
}

type listPregnanciesResp struct {
	Pregnancies []*pregnancyProto `json:"pregnancies"`
}

type vaccinationProto struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	CattleID    string     `json:"cattle_id"`
	VaccineName string     `json:"vaccine_name"`
	NextDueDate *time.Time `json:"next_due_date"`
}

type listVaccinationsResp struct {
	Vaccinations []*vaccinationProto `json:"vaccinations"`
}

// What a cow is fed, and what it actually ate.
//
// The plan is the intention and the consumption is the record, and the report
// adds the record up per feed type over a window. A report is the only one of
// the three anybody reads regularly — it is what a ration cost is argued from —
// and its window is what makes it a month's figure rather than the animal's
// whole life.
func TestAFeedReportAddsUpOneAnimalsWindow(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	tenant := newID("ten")
	opts := actingAs(tenant, "e2e")

	feedType := func(name string) string {
		t.Helper()
		out, err := svcclient.Call[createFeedTypeReq, feedTypeResp](ctx, p.feed(),
			feedSvc+"/CreateFeedType", createFeedTypeReq{
				TenantID: tenant, Name: name, Category: "concentrate",
				Unit: "kg", CreatedBy: "e2e",
			}, opts)
		if err != nil {
			t.Fatalf("CreateFeedType %s: %v", name, err)
		}
		return out.FeedType.ID
	}

	cake := feedType("cottonseed cake " + newID("f"))
	silage := feedType("maize silage " + newID("f"))
	cow, otherCow := newID("cow"), newID("cow")

	plan, err := svcclient.Call[createNutritionPlanReq, nutritionPlanResp](ctx, p.feed(),
		feedSvc+"/CreateNutritionPlan", createNutritionPlanReq{
			TenantID: tenant, CattleID: cow, FeedTypeID: cake,
			DailyQuantityKg: 4.5, StartDate: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			Notes: "high yielder, second lactation", CreatedBy: "e2e",
		}, opts)
	if err != nil {
		t.Fatalf("CreateNutritionPlan: %v", err)
	}

	got, err := svcclient.Call[getNutritionPlanReq, nutritionPlanResp](ctx, p.feed(),
		feedSvc+"/GetNutritionPlan", getNutritionPlanReq{
			ID: plan.NutritionPlan.ID, TenantID: tenant,
		}, opts)
	if err != nil {
		t.Fatalf("GetNutritionPlan: %v", err)
	}
	if got.NutritionPlan.CattleID != cow || got.NutritionPlan.FeedTypeID != cake {
		t.Errorf("the plan reads back for cow %s on feed %s, want %s on %s",
			got.NutritionPlan.CattleID, got.NutritionPlan.FeedTypeID, cow, cake)
	}
	if got.NutritionPlan.DailyQuantityKg != exact.MustFixed("4.500", 3) {
		t.Errorf("the plan reads back as %v kg a day, want 4.5", got.NutritionPlan.DailyQuantityKg)
	}
	if got.NutritionPlan.Notes != "high yielder, second lactation" {
		t.Errorf("the plan's note reads back as %q", got.NutritionPlan.Notes)
	}

	feed := func(cattle, kind string, kg float64, when time.Time) {
		t.Helper()
		if _, err := svcclient.Call[recordFeedConsumptionReq, feedConsumptionResp](ctx, p.feed(),
			feedSvc+"/RecordFeedConsumption", recordFeedConsumptionReq{
				TenantID: tenant, CattleID: cattle, FeedTypeID: kind,
				QuantityKg: kg, FedAt: when, FedBy: "stockman", CreatedBy: "e2e",
			}, opts); err != nil {
			t.Fatalf("RecordFeedConsumption: %v", err)
		}
	}

	april := time.Date(2026, 4, 10, 6, 0, 0, 0, time.UTC)
	// Inside the window: two feeds of cake and one of silage.
	feed(cow, cake, 4.0, april)
	feed(cow, cake, 4.5, april.AddDate(0, 0, 1))
	feed(cow, silage, 12.0, april)
	// Outside it: the month before, and another animal on the same day.
	feed(cow, cake, 99.0, april.AddDate(0, -1, 0))
	feed(otherCow, cake, 77.0, april)

	report, err := svcclient.Call[feedReportReq, feedReportResp](ctx, p.feed(),
		feedSvc+"/GetFeedConsumptionReport", feedReportReq{
			TenantID: tenant, CattleID: cow,
			From: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
			To:   time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		}, opts)
	if err != nil {
		t.Fatalf("GetFeedConsumptionReport: %v", err)
	}

	byFeed := map[string]exact.Fixed{}
	for _, e := range report.Entries {
		byFeed[e.FeedTypeID] = e.TotalKg
		if e.CattleID != cow {
			t.Errorf("the report carries a line for cow %s and was asked about %s",
				e.CattleID, cow)
		}
	}
	if byFeed[cake] != exact.MustFixed("8.500", 3) {
		t.Errorf("the cake total is %v kg, want 8.5 — 4.0 and 4.5 inside the "+
			"window, and 99.0 the month before which is not this month's ration",
			byFeed[cake])
	}
	if byFeed[silage] != exact.MustFixed("12.000", 3) {
		t.Errorf("the silage total is %v kg, want 12.0", byFeed[silage])
	}
	if len(report.Entries) != 2 {
		t.Errorf("the report has %d lines, want one per feed type — another "+
			"animal was fed the same day and its ration is not this one's",
			len(report.Entries))
	}
}

// An animal's weight and status are edited, and the breeds are a tenant's own.
//
// The weight is the figure a dose is calculated from and the status is what
// says whether an animal is milking, dry or gone. Both are ordinary edits, and
// both are read back here from the response rather than assumed — an update
// that returned the row as it was before is a silent no-op.
func TestAnAnimalIsUpdatedAndTheBreedListIsTheTenantsOwn(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	tenant := newID("ten")
	opts := actingAs(tenant, "e2e")

	made, err := svcclient.Call[createCattleReq, cattleResp](ctx, p.cattle(),
		cattleSvc+"/CreateCattle", createCattleReq{
			TenantID: tenant, TagNumber: newID("tag"), Name: "Lakshmi",
			Gender: "F", Weight: 380.0, CreatedBy: "e2e",
		}, opts)
	if err != nil {
		t.Fatalf("CreateCattle: %v", err)
	}

	updated, err := svcclient.Call[updateCattleReq, updateCattleResp](ctx, p.cattle(),
		cattleSvc+"/UpdateCattle", updateCattleReq{
			ID: made.Cattle.ID, TenantID: tenant,
			Status: "dry", Weight: 412.5, UpdatedBy: "e2e",
		}, opts)
	if err != nil {
		t.Fatalf("UpdateCattle: %v", err)
	}
	if updated.Cattle.Weight != 412.5 {
		t.Errorf("the animal weighs %v after being updated to 412.5; an update "+
			"that answers with the row as it was is a silent no-op",
			updated.Cattle.Weight)
	}
	if updated.Cattle.Status != "dry" {
		t.Errorf("the animal's status is %q after being set to dry", updated.Cattle.Status)
	}

	stranger := newID("ten")
	if _, err := svcclient.Call[updateCattleReq, updateCattleResp](ctx, p.cattle(),
		cattleSvc+"/UpdateCattle", updateCattleReq{
			ID: made.Cattle.ID, TenantID: stranger, Status: "sold", Weight: 1,
			UpdatedBy: "e2e",
		}, actingAs(stranger, "e2e")); err == nil {
		t.Error("another tenant edited this animal's weight and status")
	}

	// Breeds are per tenant: a society that keeps Gir and one that keeps
	// Holstein Friesian should not see each other's list.
	breed, err := svcclient.Call[createBreedReq, createBreedResp](ctx, p.cattle(),
		cattleSvc+"/CreateBreed", createBreedReq{
			TenantID: tenant, Name: "Gir " + newID("b"), Origin: "Gujarat",
			Description: "indigenous, heat tolerant", CreatedBy: "e2e",
		}, opts)
	if err != nil {
		t.Fatalf("CreateBreed: %v", err)
	}

	listed, err := svcclient.Call[tenantOnlyReq, listBreedsResp](ctx, p.cattle(),
		cattleSvc+"/ListBreeds", tenantOnlyReq{TenantID: tenant}, opts)
	if err != nil {
		t.Fatalf("ListBreeds: %v", err)
	}
	found := false
	for _, b := range listed.Breeds {
		if b.ID == breed.Breed.ID {
			found = true
			if b.Origin != "Gujarat" || b.Description == "" {
				t.Errorf("the breed reads back as %+v, losing its origin or description", b)
			}
		}
		if b.TenantID != tenant {
			t.Errorf("breed %q belongs to tenant %s and this listing was asked of %s",
				b.Name, b.TenantID, tenant)
		}
	}
	if !found {
		t.Error("the breed just created is missing from the tenant's breed list")
	}

	// A second tenant's list does not carry it.
	theirs, err := svcclient.Call[tenantOnlyReq, listBreedsResp](ctx, p.cattle(),
		cattleSvc+"/ListBreeds", tenantOnlyReq{TenantID: stranger},
		actingAs(stranger, "e2e"))
	if err != nil {
		t.Fatalf("ListBreeds for another tenant: %v", err)
	}
	for _, b := range theirs.Breeds {
		if b.ID == breed.Breed.ID {
			t.Error("another tenant's breed list carries this tenant's breed")
		}
	}
}

// The breeding history of a cow is that cow's, and the pregnancy queue holds
// only the ongoing ones.
//
// Two cows taken through a cycle in the same moment. The history is what a vet
// reads before deciding whether a cow is worth serving again, so a history
// carrying another animal's cycles is worse than none.
//
// The queue is the other shape. It filters on status='active', and without that
// every pregnancy that ever ended is still listed as ongoing — a calving list
// that grows for ever and is eventually ignored.
func TestABreedingHistoryIsOneCowsAndThePregnancyQueueIsOngoingOnly(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	tenant := newID("ten")
	opts := actingAs(tenant, "e2e")

	// carry takes one cow from heat to a confirmed pregnancy and returns the
	// cycle and the pregnancy.
	carry := func(cattle string) (cycleID, pregID string) {
		t.Helper()
		cycle, err := svcclient.Call[createCycleReq2, cycleResp2](ctx, p.breeding(),
			breedingSvc2+"/CreateBreedingCycle", createCycleReq2{
				TenantID: tenant, CattleID: cattle, HeatDate: time.Now(), CreatedBy: "e2e",
			}, opts)
		if err != nil {
			t.Fatalf("CreateBreedingCycle: %v", err)
		}
		ins, err := svcclient.Call[recordInseminationReq, inseminationResp](ctx, p.breeding(),
			breedingSvc2+"/RecordInsemination", recordInseminationReq{
				TenantID: tenant, CycleID: cycle.Cycle.ID, CattleID: cattle,
				InseminatedAt: time.Now(), Method: "artificial", CreatedBy: "e2e",
			}, opts)
		if err != nil {
			t.Fatalf("RecordInsemination: %v", err)
		}
		preg, err := svcclient.Call[confirmPregnancyReq, pregnancyResp](ctx, p.breeding(),
			breedingSvc2+"/ConfirmPregnancy", confirmPregnancyReq{
				TenantID: tenant, CattleID: cattle, InseminationID: ins.Insemination.ID,
				ConfirmedAt: time.Now(), ExpectedCalvingDate: time.Now().AddDate(0, 9, 0),
				CreatedBy: "e2e",
			}, opts)
		if err != nil {
			t.Fatalf("ConfirmPregnancy: %v", err)
		}
		return cycle.Cycle.ID, preg.Pregnancy.ID
	}

	mine, theirs := newID("cow"), newID("cow")
	myCycle, myPreg := carry(mine)
	theirCycle, theirPreg := carry(theirs)

	history, err := svcclient.Call[breedingHistoryReq, fullBreedingHistoryResp](ctx, p.breeding(),
		breedingSvc2+"/GetBreedingHistory", breedingHistoryReq{
			TenantID: tenant, CattleID: mine,
		}, opts)
	if err != nil {
		t.Fatalf("GetBreedingHistory: %v", err)
	}
	if len(history.Cycles) != 1 {
		t.Fatalf("this cow's history holds %d cycles, want 1 — another cow was "+
			"served in the same moment and its cycle is not this cow's",
			len(history.Cycles))
	}
	if history.Cycles[0].ID != myCycle {
		t.Errorf("the history holds cycle %s, want %s", history.Cycles[0].ID, myCycle)
	}
	if history.Cycles[0].CattleID != mine {
		t.Errorf("the history holds a cycle for cow %s and was asked about %s",
			history.Cycles[0].CattleID, mine)
	}
	_ = theirCycle

	// Both pregnancies are in the queue while both are ongoing.
	queue, err := svcclient.Call[tenantOnlyReq, listPregnanciesResp](ctx, p.breeding(),
		breedingSvc2+"/ListActivePregnancies", tenantOnlyReq{TenantID: tenant}, opts)
	if err != nil {
		t.Fatalf("ListActivePregnancies: %v", err)
	}
	inQueue := map[string]bool{}
	for _, x := range queue.Pregnancies {
		inQueue[x.ID] = true
		if x.Status != "active" {
			t.Errorf("a pregnancy with status %q is in the active queue", x.Status)
		}
	}
	if !inQueue[myPreg] || !inQueue[theirPreg] {
		t.Fatalf("two cows are in calf and %d pregnancies are in the queue",
			len(queue.Pregnancies))
	}

	// One calves, and leaves the queue. The other stays.
	if _, err := svcclient.Call[recordCalvingReq, calvingResp](ctx, p.breeding(),
		breedingSvc2+"/RecordCalving", recordCalvingReq{
			TenantID: tenant, PregnancyID: myPreg, CattleID: mine,
			CalfGender: "female", CalfWeight: 28.5, CreatedBy: "e2e",
		}, opts); err != nil {
		t.Fatalf("RecordCalving: %v", err)
	}

	after, err := svcclient.Call[tenantOnlyReq, listPregnanciesResp](ctx, p.breeding(),
		breedingSvc2+"/ListActivePregnancies", tenantOnlyReq{TenantID: tenant}, opts)
	if err != nil {
		t.Fatalf("ListActivePregnancies after calving: %v", err)
	}
	still := map[string]bool{}
	for _, x := range after.Pregnancies {
		still[x.ID] = true
	}
	if still[myPreg] {
		t.Error("a cow that has calved is still listed as in calf\n" +
			"Without the status filter this queue grows for ever, and a " +
			"calving list nobody can trust is one nobody reads.")
	}
	if !still[theirPreg] {
		t.Error("the cow still in calf dropped out of the queue when another calved")
	}
}

// One animal's vaccination history, and the doses actually due this week.
//
// The history is per animal and the queue is per farm, and they fail
// differently. A history that carried another animal's doses would have
// somebody skip a vaccination because the record says it was given. The queue
// is bounded on both sides: a dose due months ago is not this week's round, and
// neither is one due next year.
func TestAVaccinationHistoryIsOneAnimalsAndTheQueueIsThisWeeks(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	tenant := newID("ten")
	opts := actingAs(tenant, "e2e")
	mine, theirs := newID("cow"), newID("cow")

	when := func(d time.Duration) *time.Time { u := time.Now().UTC().Add(d); return &u }
	give := func(cattle, vaccine string, next *time.Time) string {
		t.Helper()
		out, err := svcclient.Call[recordVaccinationReq, vaccinationResp](ctx, p.health(),
			healthSvc+"/RecordVaccination", recordVaccinationReq{
				TenantID: tenant, CattleID: cattle, VaccineName: vaccine,
				BatchNumber: newID("bn"), AdministeredAt: time.Now().UTC(),
				NextDueDate: next, VeterinarianID: "vet-1", Dosage: "2ml",
				CreatedBy: "e2e",
			}, opts)
		if err != nil {
			t.Fatalf("RecordVaccination %s: %v", vaccine, err)
		}
		return out.Vaccination.ID
	}

	// Due in three days, due next year, overdue by two months — and one for
	// another animal, due in three days.
	soon := give(mine, "FMD", when(3*24*time.Hour))
	distant := give(mine, "brucellosis", when(365*24*time.Hour))
	overdue := give(mine, "HS", when(-60*24*time.Hour))
	other := give(theirs, "FMD", when(3*24*time.Hour))

	history, err := svcclient.Call[breedingHistoryReq, listVaccinationsResp](ctx, p.health(),
		healthSvc+"/GetVaccinationHistory", breedingHistoryReq{
			TenantID: tenant, CattleID: mine,
		}, opts)
	if err != nil {
		t.Fatalf("GetVaccinationHistory: %v", err)
	}
	inHistory := map[string]bool{}
	for _, v := range history.Vaccinations {
		inHistory[v.ID] = true
		if v.CattleID != mine {
			t.Errorf("a dose given to cow %s is in cow %s's history", v.CattleID, mine)
		}
	}
	if !inHistory[soon] || !inHistory[distant] || !inHistory[overdue] {
		t.Errorf("this animal has had three doses and %d are in its history",
			len(history.Vaccinations))
	}
	if inHistory[other] {
		t.Error("another animal's dose is in this animal's history\n" +
			"A vaccination somebody skips because the record says it was given " +
			"is the failure this record exists to prevent.")
	}

	queue, err := svcclient.Call[tenantOnlyReq, listVaccinationsResp](ctx, p.health(),
		healthSvc+"/ListUpcomingVaccinations", tenantOnlyReq{TenantID: tenant}, opts)
	if err != nil {
		t.Fatalf("ListUpcomingVaccinations: %v", err)
	}
	due := map[string]bool{}
	for _, v := range queue.Vaccinations {
		due[v.ID] = true
	}
	if !due[soon] {
		t.Errorf("a dose due in three days is not in this week's round; %d are",
			len(queue.Vaccinations))
	}
	if due[distant] {
		t.Error("a dose due next year is in this week's round")
	}
	if due[overdue] {
		t.Error("a dose two months overdue is in this week's round\n" +
			"It does need chasing, and not from a list headed 'due this week': " +
			"a round that mixes the two is a round nobody can work from.")
	}
}
