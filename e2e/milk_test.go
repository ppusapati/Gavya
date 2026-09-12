//go:build e2e

// Milk capture, through the real binary.
//
// milk-service had no end-to-end coverage of any kind — nine routes, none of
// them called by a test. It is the thinnest service in the platform and also the
// one holding the reading a producer is paid for.
//
// GetDailyYield is the only endpoint here that computes anything, and it had a
// defect: it discarded the error from parsing its date, so a request nobody
// could read came back as zero litres rather than as a complaint. Everything
// else is a row in and a row out, so the rest of these are about what goes wrong
// in row-in-row-out services: a filter that silently matches nothing, a guard
// that does not fire, and a tenant seeing another's readings.
package e2e

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

const milkSvc = "milk.v1.MilkService"

// tenantTimezone is where these tenants are. It is stated rather than defaulted,
// which is the point: a shift is "morning" somewhere, and which day a reading
// falls on is read out of that later.
const tenantTimezone = "Asia/Kolkata"

type createMilkSessionReq struct {
	TenantID  string `json:"tenant_id"`
	CattleID  string `json:"cattle_id"`
	ShiftType string `json:"shift_type"`
	Timezone  string `json:"timezone"`
	CreatedBy string `json:"created_by"`
}

type milkSessionResp struct {
	Session *struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"session"`
}

type recordMilkReq struct {
	TenantID       string  `json:"tenant_id"`
	SessionID      string  `json:"session_id"`
	CattleID       string  `json:"cattle_id"`
	QuantityLiters float64 `json:"quantity_liters"`
	CreatedBy      string  `json:"created_by"`
}

type milkRecordResp struct {
	Record *struct {
		ID             string  `json:"id"`
		QuantityLiters float64 `json:"quantity_liters"`
	} `json:"record"`
}

type dailyYieldReq struct {
	TenantID string `json:"tenant_id"`
	CattleID string `json:"cattle_id"`
	Date     string `json:"date"`
}

type dailyYieldResp struct {
	TotalLiters float64 `json:"total_liters"`
}

// aMilking records one session's worth of readings for one animal and returns
// the animal's id.
func aMilking(t *testing.T, p *platform, litres ...float64) string {
	t.Helper()
	cattle := newID("cow")
	sess, err := svcclient.Call[createMilkSessionReq, milkSessionResp](
		context.Background(), p.milk(), milkSvc+"/CreateSession",
		createMilkSessionReq{TenantID: p.tenant, CattleID: cattle,
			ShiftType: "morning", Timezone: tenantTimezone, CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, l := range litres {
		if _, err := svcclient.Call[recordMilkReq, milkRecordResp](
			context.Background(), p.milk(), milkSvc+"/RecordMilk",
			recordMilkReq{TenantID: p.tenant, SessionID: sess.Session.ID,
				CattleID: cattle, QuantityLiters: l, CreatedBy: "e2e"}, p.opts()); err != nil {
			t.Fatalf("record %v litres: %v", l, err)
		}
	}
	return cattle
}

// A day's yield is the sum of that day's readings.
//
// The timezone question this looks like it is asking is asked properly in
// milk-service's own repository tests, which can set a connection's timezone;
// the e2e harness opens its pool at startup, so a timezone set here would change
// nothing for a service already running, and a test that sets one is checking
// the timezone it meant to avoid. Setting it anyway and asserting is how a check
// comes to report success while doing nothing.
//
// What this asserts is the part that belongs at this level: two readings written
// through the real service add up through the real service.
func TestADaysYieldIsTheSumOfThatDaysReadings(t *testing.T) {
	p := startPlatform(t)

	cattle := aMilking(t, p, 6.250, 5.750)

	// The day as the database sees it, which is the day the readings were
	// written under.
	today := today(t)
	got, err := svcclient.Call[dailyYieldReq, dailyYieldResp](
		context.Background(), p.milk(), milkSvc+"/GetDailyYield",
		dailyYieldReq{TenantID: p.tenant, CattleID: cattle, Date: today}, p.opts())
	if err != nil {
		t.Fatalf("get daily yield: %v", err)
	}
	if got.TotalLiters != 12.0 {
		t.Errorf("the day's yield is %v litres, want 12 — two readings of 6.250 and "+
			"5.750 were written today (%s) and the sum found none of them",
			got.TotalLiters, today)
	}
}

// A date nobody can parse is an error, not zero litres.
//
// The handler discarded the error from time.Parse. A malformed date became the
// zero time — the first of January, year one — and the query summed the readings
// recorded that day, of which there are none. The caller was told the animal gave
// nothing, which is a fact somebody acts on, rather than that the request was
// malformed, which is a fact they can fix.
func TestAnUnparseableDateIsRefusedRatherThanAnsweredWithZero(t *testing.T) {
	p := startPlatform(t)
	cattle := aMilking(t, p, 6.250)

	for _, bad := range []string{"", "10-09-2026", "2026-09-10T06:00:00Z", "yesterday"} {
		out, err := svcclient.Call[dailyYieldReq, dailyYieldResp](
			context.Background(), p.milk(), milkSvc+"/GetDailyYield",
			dailyYieldReq{TenantID: p.tenant, CattleID: cattle, Date: bad}, p.opts())
		if err == nil {
			t.Errorf("date %q was accepted and answered %v litres; a caller who mistyped "+
				"a date is told the animal gave nothing, and goes to look at a cow that "+
				"is fine", bad, out.TotalLiters)
		}
	}
}

// A reading finer than the column is refused rather than rounded.
//
// quantity_liters is NUMERIC(8,3): litres to the millilitre. A fourth decimal is
// a value PostgreSQL would round on the way in without saying so, and the figure
// stored would not be the figure sent.
func TestAReadingFinerThanTheColumnIsRefused(t *testing.T) {
	p := startPlatform(t)
	cattle := newID("cow")
	sess, err := svcclient.Call[createMilkSessionReq, milkSessionResp](
		context.Background(), p.milk(), milkSvc+"/CreateSession",
		createMilkSessionReq{TenantID: p.tenant, CattleID: cattle,
			ShiftType: "morning", Timezone: tenantTimezone, CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if _, err := svcclient.Call[recordMilkReq, milkRecordResp](
		context.Background(), p.milk(), milkSvc+"/RecordMilk",
		recordMilkReq{TenantID: p.tenant, SessionID: sess.Session.ID, CattleID: cattle,
			QuantityLiters: 6.2505, CreatedBy: "e2e"}, p.opts()); err == nil {
		t.Error("6.2505 litres was accepted into a column that holds three decimals")
	}

	// And a reading it can hold is accepted and comes back unchanged, so the
	// refusal above is not a service that refuses everything.
	ok, err := svcclient.Call[recordMilkReq, milkRecordResp](
		context.Background(), p.milk(), milkSvc+"/RecordMilk",
		recordMilkReq{TenantID: p.tenant, SessionID: sess.Session.ID, CattleID: cattle,
			QuantityLiters: 6.250, CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("an ordinary reading was refused: %v", err)
	}
	if ok.Record.QuantityLiters != 6.250 {
		t.Errorf("6.250 litres came back as %v", ok.Record.QuantityLiters)
	}
}

// A yield is one animal's, on one day.
//
// A sum scoped to the wrong thing is the defect that looks most like a correct
// answer: it is a number, of the right magnitude, in the right units.
func TestADaysYieldCountsOneAnimalAndOneDay(t *testing.T) {
	p := startPlatform(t)

	mine := aMilking(t, p, 6.250)
	other := aMilking(t, p, 40.000)

	today := today(t)
	got, err := svcclient.Call[dailyYieldReq, dailyYieldResp](
		context.Background(), p.milk(), milkSvc+"/GetDailyYield",
		dailyYieldReq{TenantID: p.tenant, CattleID: mine, Date: today}, p.opts())
	if err != nil {
		t.Fatalf("get daily yield: %v", err)
	}
	if got.TotalLiters != 6.250 {
		t.Errorf("one animal's yield is %v litres, want 6.250; the other animal gave "+
			"40 that day and %s", got.TotalLiters, other)
	}

	// And a day with no readings is nothing, rather than every day's readings.
	empty, err := svcclient.Call[dailyYieldReq, dailyYieldResp](
		context.Background(), p.milk(), milkSvc+"/GetDailyYield",
		dailyYieldReq{TenantID: p.tenant, CattleID: mine, Date: "2001-01-01"}, p.opts())
	if err != nil {
		t.Fatalf("get daily yield for an empty day: %v", err)
	}
	if empty.TotalLiters != 0 {
		t.Errorf("a day with no readings reports %v litres", empty.TotalLiters)
	}
}

// One tenant cannot read another's milk records.
func TestMilkKeepsTenantsApart(t *testing.T) {
	p := startPlatform(t)
	cattle := aMilking(t, p, 6.250)
	today := today(t)

	mine, err := svcclient.Call[dailyYieldReq, dailyYieldResp](
		context.Background(), p.milk(), milkSvc+"/GetDailyYield",
		dailyYieldReq{TenantID: p.tenant, CattleID: cattle, Date: today}, p.opts())
	if err != nil {
		t.Fatalf("get daily yield as the owning tenant: %v", err)
	}
	if mine.TotalLiters == 0 {
		t.Fatal("the tenant that recorded the milk sees none of it, so the check below " +
			"would pass against a query that returns nothing to anybody")
	}

	other := newID("tnt")
	theirs, err := svcclient.Call[dailyYieldReq, dailyYieldResp](
		context.Background(), p.milk(), milkSvc+"/GetDailyYield",
		dailyYieldReq{TenantID: other, CattleID: cattle, Date: today},
		actingAs(other, "e2e"))
	if err == nil && theirs.TotalLiters != 0 {
		t.Errorf("a second tenant reads %v litres against an animal it has never "+
			"recorded, through milk-service", theirs.TotalLiters)
	}
}

// today is the current day where these tenants are, which is the day a reading
// written now is filed under.
//
// It used to ask the database what day it was. That is the question the service
// stopped asking: which day a reading falls on is a fact about the tenant, and a
// test that reads it from the database agrees with the service only while the
// two happen to be in the same zone.
func today(t *testing.T) string {
	t.Helper()
	loc, err := time.LoadLocation(tenantTimezone)
	if err != nil {
		t.Fatalf("load %s: %v", tenantTimezone, err)
	}
	return time.Now().In(loc).Format("2006-01-02")
}

// A tenant's timezone is stated on its first session and fixed from then on.
//
// tenant-service has recorded a Timezone per tenant since it was written and
// nothing read it, while the daily yield took its day boundary from whatever
// timezone the database was configured in. Two things claiming to say when a
// day begins is the same shape as two things claiming what currency an amount
// is in, and it goes wrong the same way: silently, and only for whoever is
// furthest from the assumption.
//
// So milk-service pins it where the readings are, on the same shape the services
// holding money use for currency. What that buys is checked properly in
// milk-service's own repository tests, which can put a reading either side of a
// day boundary; this is the part that belongs at this level — that a caller has
// to say, and cannot change its mind.
func TestATenantsTimezoneIsStatedOnceAndThenFixed(t *testing.T) {
	p := startPlatform(t)

	// Stated on the first session.
	if _, err := svcclient.Call[createMilkSessionReq, milkSessionResp](
		context.Background(), p.milk(), milkSvc+"/CreateSession",
		createMilkSessionReq{TenantID: p.tenant, CattleID: newID("cow"),
			ShiftType: "morning", Timezone: tenantTimezone, CreatedBy: "e2e"},
		p.opts()); err != nil {
		t.Fatalf("open the first session: %v", err)
	}

	// A second session in the same zone is fine — most days are the second
	// session, and refusing them would make the pin unusable.
	if _, err := svcclient.Call[createMilkSessionReq, milkSessionResp](
		context.Background(), p.milk(), milkSvc+"/CreateSession",
		createMilkSessionReq{TenantID: p.tenant, CattleID: newID("cow"),
			ShiftType: "evening", Timezone: tenantTimezone, CreatedBy: "e2e"},
		p.opts()); err != nil {
		t.Errorf("a second session in the same timezone was refused: %v", err)
	}

	// A different one is refused. The readings already taken were filed under
	// the first, and what is stored is an instant — so after the change nothing
	// would say which evening is which.
	if _, err := svcclient.Call[createMilkSessionReq, milkSessionResp](
		context.Background(), p.milk(), milkSvc+"/CreateSession",
		createMilkSessionReq{TenantID: p.tenant, CattleID: newID("cow"),
			ShiftType: "morning", Timezone: "America/Chicago", CreatedBy: "e2e"},
		p.opts()); err == nil {
		t.Error("a session in a second timezone was accepted; every reading already " +
			"recorded was filed under the first one")
	}

	// And a zone nobody can load is refused rather than stored, in a fresh
	// tenant so the refusal is about the name and not about the pin.
	fresh := newID("tnt")
	if _, err := svcclient.Call[createMilkSessionReq, milkSessionResp](
		context.Background(), p.milk(), milkSvc+"/CreateSession",
		createMilkSessionReq{TenantID: fresh, CattleID: newID("cow"),
			ShiftType: "morning", Timezone: "IST", CreatedBy: "e2e"},
		actingAs(fresh, "e2e")); err == nil {
		t.Error(`"IST" was accepted as a timezone; it names two different zones five ` +
			"and a half hours apart, and neither is what the database would store")
	}

	// An omitted one too. There is no default: a shift is "morning" somewhere,
	// and time.LoadLocation("") succeeds — it returns UTC — so refusing an empty
	// name has to be its own check rather than a consequence of loading it.
	//
	// The tenant here is fresh and the call options name the same one. Written
	// with a fresh id in the body and the shared tenant in the header, this
	// passed on the mismatch rather than on the empty zone: removing the
	// emptiness check left it green.
	blank := newID("tnt")
	if _, err := svcclient.Call[createMilkSessionReq, milkSessionResp](
		context.Background(), p.milk(), milkSvc+"/CreateSession",
		createMilkSessionReq{TenantID: blank, CattleID: newID("cow"),
			ShiftType: "morning", CreatedBy: "e2e"},
		actingAs(blank, "e2e")); err == nil {
		t.Error("a session with no timezone was accepted, so its tenant's days begin " +
			"wherever the database happens to be configured")
	}
}

// A caller is told whose problem it is.
//
// milk-service had no classifier. CreateSession reported every failure as an
// invalid argument, so a database that was down looked like a malformed request;
// GetDailyYield reported every failure as internal, so a tenant that had simply
// never recorded any milk was told to retry something that could not succeed
// until it did. Both are the same mistake in opposite directions — a code
// chosen without looking at the error.
func TestMilkSaysWhoseProblemAFailureIs(t *testing.T) {
	p := startPlatform(t)

	// A tenant that has recorded nothing has not said which timezone its days
	// are reckoned in. That is its state, not its request.
	fresh := newID("tnt")
	_, err := svcclient.Call[dailyYieldReq, dailyYieldResp](
		context.Background(), p.milk(), milkSvc+"/GetDailyYield",
		dailyYieldReq{TenantID: fresh, CattleID: newID("cow"), Date: today(t)},
		actingAs(fresh, "e2e"))
	if err == nil {
		t.Fatal("a yield was answered for a tenant that has recorded no milk")
	}
	if code := codeOf(t, err); code != connect.CodeFailedPrecondition {
		t.Errorf("a tenant with no readings gets %s, want failed_precondition — "+
			"internal tells a client to retry a call that cannot succeed until it "+
			"opens a session (err = %v)", code, err)
	}

	// A missing field is the caller's.
	_, err = svcclient.Call[createMilkSessionReq, milkSessionResp](
		context.Background(), p.milk(), milkSvc+"/CreateSession",
		createMilkSessionReq{TenantID: p.tenant, ShiftType: "morning",
			Timezone: tenantTimezone, CreatedBy: "e2e"}, p.opts())
	if err == nil {
		t.Fatal("a session with no cattle_id was opened")
	}
	if code := codeOf(t, err); code != connect.CodeInvalidArgument {
		t.Errorf("a missing cattle_id gets %s, want invalid_argument (err = %v)", code, err)
	}
}

// codeOf reads the Connect code off a service error, failing the test if the
// error is not one — a transport failure answering a question about
// classification would otherwise read as the wrong classification.
func codeOf(t *testing.T, err error) connect.Code {
	t.Helper()
	var svcErr *svcclient.Error
	if !errors.As(err, &svcErr) {
		t.Fatalf("not a service error: %v", err)
	}
	return svcErr.Code
}

// The reads, and the quality reading that hangs off a milk record.
//
// These are the six routes milk-service had left. They are reads and one write,
// and the assertions are the three things that go wrong in a service shaped like
// this: a row that comes back as something other than what went in, a list
// scoped to the wrong thing, and a figure rounded on its way into a column.

type getByIDReq struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

type listSessionsReq struct {
	TenantID string `json:"tenant_id"`
	Limit    int32  `json:"limit"`
	Offset   int32  `json:"offset"`
}

type listSessionsResp struct {
	Sessions []*struct {
		ID       string `json:"id"`
		CattleID string `json:"cattle_id"`
		Status   string `json:"status"`
	} `json:"sessions"`
}

type listRecordsReq struct {
	SessionID string `json:"session_id"`
	TenantID  string `json:"tenant_id"`
}

type listRecordsResp struct {
	Records []*struct {
		ID             string  `json:"id"`
		SessionID      string  `json:"session_id"`
		QuantityLiters float64 `json:"quantity_liters"`
	} `json:"records"`
}

type updateSessionStatusReq struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	Status    string `json:"status"`
	UpdatedBy string `json:"updated_by"`
}

type recordQualityReq struct {
	TenantID   string  `json:"tenant_id"`
	RecordID   string  `json:"record_id"`
	FatPercent float64 `json:"fat_percent"`
	SNFPercent float64 `json:"snf_percent"`
	Lactose    float64 `json:"lactose"`
	CreatedBy  string  `json:"created_by"`
}

type qualityResp struct {
	Quality *struct {
		ID         string  `json:"id"`
		RecordID   string  `json:"record_id"`
		FatPercent float64 `json:"fat_percent"`
		SNFPercent float64 `json:"snf_percent"`
		Lactose    float64 `json:"lactose"`
	} `json:"quality"`
}

// A session and its records read back as what was written, and belong to the
// session they were written against.
func TestAMilkSessionAndItsRecordsReadBack(t *testing.T) {
	p := startPlatform(t)
	cattle := newID("cow")

	sess, err := svcclient.Call[createMilkSessionReq, milkSessionResp](
		context.Background(), p.milk(), milkSvc+"/CreateSession",
		createMilkSessionReq{TenantID: p.tenant, CattleID: cattle,
			ShiftType: "evening", Timezone: tenantTimezone, CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	got, err := svcclient.Call[getByIDReq, listSessionsResp](
		context.Background(), p.milk(), milkSvc+"/GetSession",
		getByIDReq{ID: sess.Session.ID, TenantID: p.tenant}, p.opts())
	_ = got
	if err != nil {
		t.Fatalf("get session: %v", err)
	}

	// Two records in this session, one in another, so the list has something to
	// exclude. A list scoped to the wrong thing is a number of the right
	// magnitude in the right units.
	for _, l := range []float64{6.250, 5.750} {
		if _, err := svcclient.Call[recordMilkReq, milkRecordResp](
			context.Background(), p.milk(), milkSvc+"/RecordMilk",
			recordMilkReq{TenantID: p.tenant, SessionID: sess.Session.ID,
				CattleID: cattle, QuantityLiters: l, CreatedBy: "e2e"}, p.opts()); err != nil {
			t.Fatalf("record %v: %v", l, err)
		}
	}
	aMilking(t, p, 40.000)

	list, err := svcclient.Call[listRecordsReq, listRecordsResp](
		context.Background(), p.milk(), milkSvc+"/ListSessionRecords",
		listRecordsReq{SessionID: sess.Session.ID, TenantID: p.tenant}, p.opts())
	if err != nil {
		t.Fatalf("list session records: %v", err)
	}
	if len(list.Records) != 2 {
		t.Fatalf("the session holds %d records, want 2 — another session was milked "+
			"the same day and its reading is not this one's", len(list.Records))
	}
	for _, r := range list.Records {
		if r.SessionID != sess.Session.ID {
			t.Errorf("a record from session %s came back in session %s's list",
				r.SessionID, sess.Session.ID)
		}
	}

	// And one of them reads back on its own, with the figure it was written with.
	one, err := svcclient.Call[getByIDReq, milkRecordResp](
		context.Background(), p.milk(), milkSvc+"/GetRecord",
		getByIDReq{ID: list.Records[0].ID, TenantID: p.tenant}, p.opts())
	if err != nil {
		t.Fatalf("get record: %v", err)
	}
	if one.Record.QuantityLiters != list.Records[0].QuantityLiters {
		t.Errorf("the record reads %v litres on its own and %v in the list",
			one.Record.QuantityLiters, list.Records[0].QuantityLiters)
	}

	// A session listing holds this tenant's sessions and no other tenant's.
	mine, err := svcclient.Call[listSessionsReq, listSessionsResp](
		context.Background(), p.milk(), milkSvc+"/ListSessions",
		listSessionsReq{TenantID: p.tenant, Limit: 100}, p.opts())
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(mine.Sessions) == 0 {
		t.Fatal("the tenant that opened a session sees none")
	}
	other := newID("tnt")
	theirs, err := svcclient.Call[listSessionsReq, listSessionsResp](
		context.Background(), p.milk(), milkSvc+"/ListSessions",
		listSessionsReq{TenantID: other, Limit: 100},
		actingAs(other, "e2e"))
	if err == nil && len(theirs.Sessions) > 0 {
		t.Errorf("a second tenant sees %d milk sessions it never opened", len(theirs.Sessions))
	}
}

// A session's status moves, and only this tenant can move it.
func TestAMilkSessionsStatusMoves(t *testing.T) {
	p := startPlatform(t)
	sess, err := svcclient.Call[createMilkSessionReq, milkSessionResp](
		context.Background(), p.milk(), milkSvc+"/CreateSession",
		createMilkSessionReq{TenantID: p.tenant, CattleID: newID("cow"),
			ShiftType: "morning", Timezone: tenantTimezone, CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if sess.Session.Status != "pending" {
		t.Fatalf("a new session is %s, want pending", sess.Session.Status)
	}

	moved, err := svcclient.Call[updateSessionStatusReq, milkSessionResp](
		context.Background(), p.milk(), milkSvc+"/UpdateSessionStatus",
		updateSessionStatusReq{ID: sess.Session.ID, TenantID: p.tenant,
			Status: "completed", UpdatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("update status: %v", err)
	}
	if moved.Session.Status != "completed" {
		t.Errorf("status = %s, want completed", moved.Session.Status)
	}

	// A second tenant cannot move it. Without the tenant clause this would
	// succeed and read as a status somebody else set.
	other := newID("tnt")
	if _, err := svcclient.Call[updateSessionStatusReq, milkSessionResp](
		context.Background(), p.milk(), milkSvc+"/UpdateSessionStatus",
		updateSessionStatusReq{ID: sess.Session.ID, TenantID: other,
			Status: "cancelled", UpdatedBy: "e2e"},
		actingAs(other, "e2e")); err == nil {
		t.Error("a second tenant moved the status of a session it did not open")
	}
}

// A quality reading is held to two decimals, and refused finer.
//
// fat_percent, snf_percent and lactose are NUMERIC(5,2). A third decimal is a
// value PostgreSQL would round on the way in without saying so, and fat is what
// a producer is paid on.
func TestAQualityReadingIsHeldToItsColumn(t *testing.T) {
	p := startPlatform(t)
	cattle := newID("cow")
	sess, err := svcclient.Call[createMilkSessionReq, milkSessionResp](
		context.Background(), p.milk(), milkSvc+"/CreateSession",
		createMilkSessionReq{TenantID: p.tenant, CattleID: cattle,
			ShiftType: "morning", Timezone: tenantTimezone, CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	rec, err := svcclient.Call[recordMilkReq, milkRecordResp](
		context.Background(), p.milk(), milkSvc+"/RecordMilk",
		recordMilkReq{TenantID: p.tenant, SessionID: sess.Session.ID,
			CattleID: cattle, QuantityLiters: 6.250, CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("record milk: %v", err)
	}

	got, err := svcclient.Call[recordQualityReq, qualityResp](
		context.Background(), p.milk(), milkSvc+"/RecordQuality",
		recordQualityReq{TenantID: p.tenant, RecordID: rec.Record.ID,
			FatPercent: 4.10, SNFPercent: 8.55, Lactose: 4.80,
			CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("record quality: %v", err)
	}
	if got.Quality.FatPercent != 4.10 || got.Quality.SNFPercent != 8.55 {
		t.Errorf("the reading came back as fat %v snf %v, want 4.10 and 8.55",
			got.Quality.FatPercent, got.Quality.SNFPercent)
	}
	if got.Quality.RecordID != rec.Record.ID {
		t.Errorf("the reading is against record %s, not the one it was taken on",
			got.Quality.RecordID)
	}

	// A third decimal on any of the three is refused rather than rounded.
	for _, finer := range []recordQualityReq{
		{FatPercent: 4.105, SNFPercent: 8.55, Lactose: 4.80},
		{FatPercent: 4.10, SNFPercent: 8.555, Lactose: 4.80},
		{FatPercent: 4.10, SNFPercent: 8.55, Lactose: 4.805},
	} {
		finer.TenantID, finer.RecordID, finer.CreatedBy = p.tenant, rec.Record.ID, "e2e"
		if _, err := svcclient.Call[recordQualityReq, qualityResp](
			context.Background(), p.milk(), milkSvc+"/RecordQuality", finer,
			p.opts()); err == nil {
			t.Errorf("a three-decimal reading was accepted (fat %v snf %v lactose %v) "+
				"into columns that hold two", finer.FatPercent, finer.SNFPercent, finer.Lactose)
		}
	}
}
