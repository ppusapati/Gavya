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
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

const milkSvc = "milk.v1.MilkService"

type createMilkSessionReq struct {
	TenantID  string `json:"tenant_id"`
	CattleID  string `json:"cattle_id"`
	ShiftType string `json:"shift_type"`
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
			ShiftType: "morning", CreatedBy: "e2e"}, p.opts())
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
	today := dayIn(t, "e2e_milk")
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
			ShiftType: "morning", CreatedBy: "e2e"}, p.opts())
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

	today := dayIn(t, "e2e_milk")
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
	today := dayIn(t, "e2e_milk")

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
		svcclient.CallOptions{Tenant: other, Actor: "e2e"})
	if err == nil && theirs.TotalLiters != 0 {
		t.Errorf("a second tenant reads %v litres against an animal it has never "+
			"recorded, through milk-service", theirs.TotalLiters)
	}
}

// dayIn is today as the given database reckons it, which is the day a reading
// written now is recorded under.
func dayIn(t *testing.T, database string) string {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), dsn(t, database))
	if err != nil {
		t.Fatalf("connect to %s: %v", database, err)
	}
	defer conn.Close(context.Background())
	var day time.Time
	if err := conn.QueryRow(context.Background(), "SELECT CURRENT_DATE").Scan(&day); err != nil {
		t.Fatalf("read current date: %v", err)
	}
	return day.Format("2006-01-02")
}
