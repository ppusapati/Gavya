package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/tenantctx"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"

	"github.com/ppusapati/gavya/services/milk-service/internal/domain"
)

// acting is a context carrying who is doing this, which the audit trail needs.
// The gateway would normally set it, and the audit write refuses an entry it
// cannot attribute — a record of a change that cannot say who made it is not a
// record anybody can use, so the refusal is the feature.
func acting(tenant string) context.Context {
	return tenantctx.WithActor(
		tenantdb.WithTenant(context.Background(), tenant),
		tenantctx.Actor{ID: "integration-test"})
}

// A day's yield falls on the tenant's day, whatever timezone the database is in.
//
// recorded_at is a TIMESTAMPTZ and casting one to a date uses the session's
// timezone, so the query used to answer a different question depending on where
// the database happened to be configured. Measured, not assumed: a collection at
// one in the morning Indian time reads as the 11th from a session in Kolkata and
// the 10th from one in UTC or Chicago.
//
// That is not a small difference at a fortnight boundary. The figure settlement
// is drawn from would move between one fortnight and the next on a setting
// nobody involved chose, and nothing anywhere would say so.
//
// The query now converts the instant to the tenant's own wall clock before
// taking the date — `recorded_at AT TIME ZONE $4` — and the tenant's zone is
// pinned on its first session, the same way the services holding money pin a
// currency. These tests run from sessions in three zones, one of them ahead of
// the tenant's and one behind, and the answer has to be the same in all three.
//
// They are repository tests rather than end-to-end ones because reproducing a
// session timezone needs control of the connection. The e2e harness opens its
// pool at startup, so a timezone set there changes nothing for a service already
// running — an e2e test written for this passes without exercising anything. One
// was written that way first, and did exactly that.
//
//	TEST_DATABASE_DSN="postgres://user@host:port/%s?sslmode=disable" go test ./...

const testDatabase = "milk_repo_test"

// sessionZones are what the database might be configured as: UTC, one ahead of
// the tenant and one behind. A query that read the session's timezone instead of
// the tenant's would agree with the tenant in exactly one of these, which is why
// one non-UTC zone would not be enough.
var sessionZones = []string{"UTC", "Asia/Tokyo", "America/Chicago"}

// The tenant is in India throughout, and the reading below is at one in the
// morning there — an instant that is the previous day in UTC, in Chicago, and in
// every zone west of it.
const (
	tenantZone   = "Asia/Kolkata"
	earlyMorning = "2026-09-11T01:00:00+05:30"
	indianDay    = "2026-09-11"
	utcDay       = "2026-09-10"
)

func TestAReadingFallsOnTheTenantsDayNotTheDatabasesTimezone(t *testing.T) {
	for _, zone := range sessionZones {
		t.Run("database in "+zone, func(t *testing.T) {
			pool := testPoolIn(t, zone)
			r := New(pool)
			tenant, cattle := newTestID("tnt"), newTestID("cow")
			ctx := acting(tenant)

			if err := r.PinTenantTimezone(ctx, tenant, tenantZone); err != nil {
				t.Fatalf("pin timezone: %v", err)
			}
			session := newTestID("ses")
			mustSession(t, r, tenant, cattle, session)

			at, err := time.Parse(time.RFC3339, earlyMorning)
			if err != nil {
				t.Fatal(err)
			}
			mustRecordAt(t, r, tenant, session, cattle, 6.250, at)
			mustRecordAt(t, r, tenant, session, cattle, 5.750, at)

			// The Indian day holds both.
			onDay, err := r.GetDailyYield(ctx, tenant, cattle, mustDay(t, indianDay), tenantZone)
			if err != nil {
				t.Fatalf("get daily yield: %v", err)
			}
			if onDay != 12.0 {
				t.Errorf("from a database in %s, %s holds %v litres, want 12\n"+
					"The readings were taken at one in the morning on %s in %s, which is "+
					"where the tenant is. Reading them into the previous day would move a "+
					"morning's milk from one fortnight into the other.",
					zone, indianDay, onDay, indianDay, tenantZone)
			}

			// And the UTC day holds neither. Without this, a query that matched
			// everything would satisfy the check above.
			dayBefore, err := r.GetDailyYield(ctx, tenant, cattle, mustDay(t, utcDay), tenantZone)
			if err != nil {
				t.Fatalf("get the previous day's yield: %v", err)
			}
			if dayBefore != 0 {
				t.Errorf("from a database in %s, %s holds %v litres — that is the day these "+
					"readings fall on in UTC, not the day they fall on where they were taken",
					zone, utcDay, dayBefore)
			}
		})
	}
}

// A tenant's timezone is stated once and cannot be changed by a later session.
//
// The same argument as the currency pin. What is stored is an instant; the
// disagreement is about how to read it, so two reckonings of the same evening
// are indistinguishable afterwards.
func TestATenantsTimezoneIsFixedByItsFirstSession(t *testing.T) {
	pool := testPoolIn(t, "UTC")
	r := New(pool)
	tenant := newTestID("tnt")
	ctx := acting(tenant)

	if err := r.PinTenantTimezone(ctx, tenant, "Asia/Kolkata"); err != nil {
		t.Fatalf("pin: %v", err)
	}
	// The same zone again is fine; a device retrying, or a second session on the
	// same day, must not be refused.
	if err := r.PinTenantTimezone(ctx, tenant, "Asia/Kolkata"); err != nil {
		t.Errorf("pinning the same timezone twice was refused: %v", err)
	}
	if err := r.PinTenantTimezone(ctx, tenant, "America/Chicago"); !errors.Is(err, ErrTimezoneMismatch) {
		t.Errorf("a second timezone was accepted (err = %v); the readings already "+
			"recorded were filed under the first, and nothing would say which is which", err)
	}

	got, err := r.TenantTimezone(ctx, tenant)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got != "Asia/Kolkata" {
		t.Errorf("timezone reads back as %s, want Asia/Kolkata", got)
	}
}

// A zone this binary cannot load is refused rather than stored.
func TestAnUnknownTimezoneIsRefused(t *testing.T) {
	pool := testPoolIn(t, "UTC")
	r := New(pool)
	tenant := newTestID("tnt")
	for _, bad := range []string{"", "IST", "Asia/Kothapalli", "+05:30"} {
		if err := r.PinTenantTimezone(acting(tenant), tenant, bad); !errors.Is(err, ErrUnknownTimezone) {
			t.Errorf("timezone %q was accepted (err = %v); a zone the database holds and "+
				"the code cannot load fails later, somewhere further from the cause", bad, err)
		}
	}
}

// A tenant that has recorded nothing has not said which days it reckons by.
func TestATenantWithNoTimezoneIsSaidToHaveNone(t *testing.T) {
	pool := testPoolIn(t, "UTC")
	r := New(pool)
	tenant := newTestID("tnt")
	if _, err := r.TenantTimezone(acting(tenant), tenant); !errors.Is(err, ErrTimezoneUnset) {
		t.Errorf("err = %v, want ErrTimezoneUnset — answering anyway would mean picking "+
			"a timezone on the tenant's behalf", err)
	}
}

// A yield is one animal's, in every timezone too. A sum scoped to the wrong
// thing is the defect that looks most like a correct answer.
func TestADaysYieldCountsOneAnimal(t *testing.T) {
	pool := testPoolIn(t, "Asia/Kolkata")
	r := New(pool)
	tenant := newTestID("tnt")
	ctx := acting(tenant)
	if err := r.PinTenantTimezone(ctx, tenant, tenantZone); err != nil {
		t.Fatalf("pin timezone: %v", err)
	}

	mine, theirs := newTestID("cow"), newTestID("cow")
	session := newTestID("ses")
	mustSession(t, r, tenant, mine, session)
	mustRecord(t, r, tenant, session, mine, 6.250)
	mustRecord(t, r, tenant, session, theirs, 40.000)

	total, err := r.GetDailyYield(ctx, tenant, mine, currentDate(t, pool), tenantZone)
	if err != nil {
		t.Fatalf("get daily yield: %v", err)
	}
	if total != 6.250 {
		t.Errorf("one animal's yield is %v litres, want 6.250; the other gave 40 the "+
			"same day", total)
	}
}

// And one tenant's. The readings are the figure a producer is paid against.
func TestADaysYieldCountsOneTenant(t *testing.T) {
	pool := testPoolIn(t, "Asia/Kolkata")
	r := New(pool)
	cattle := newTestID("cow")
	mine, theirs := newTestID("tnt"), newTestID("tnt")
	ctx := acting(mine)
	for _, tenant := range []string{mine, theirs} {
		if err := r.PinTenantTimezone(acting(tenant), tenant, tenantZone); err != nil {
			t.Fatalf("pin timezone: %v", err)
		}
		session := newTestID("ses")
		mustSession(t, r, tenant, cattle, session)
		mustRecord(t, r, tenant, session, cattle, 6.250)
	}

	total, err := r.GetDailyYield(ctx, mine, cattle, currentDate(t, pool), tenantZone)
	if err != nil {
		t.Fatalf("get daily yield: %v", err)
	}
	if total != 6.250 {
		t.Errorf("one tenant's yield is %v litres, want 6.250; another tenant recorded "+
			"the same animal id on the same day", total)
	}
}

func mustSession(t *testing.T, r Repository, tenant, cattle, id string) {
	t.Helper()
	if _, err := r.CreateSession(acting(tenant), &domain.MilkSession{
		ID: id, TenantID: tenant, CattleID: cattle, SessionDate: time.Now(),
		ShiftType: "morning", Status: "pending",
		CreatedBy: "test", UpdatedBy: "test",
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
}

func mustRecord(t *testing.T, r Repository, tenant, session, cattle string, litres float64) {
	t.Helper()
	mustRecordAt(t, r, tenant, session, cattle, litres, time.Now())
}

// mustRecordAt writes a reading at a stated instant, which is what lets a test
// put one either side of a day boundary on purpose.
func mustRecordAt(t *testing.T, r Repository, tenant, session, cattle string, litres float64, at time.Time) {
	t.Helper()
	if _, err := r.CreateRecord(acting(tenant), &domain.MilkRecord{
		ID: newTestID("rec"), TenantID: tenant, SessionID: session, CattleID: cattle,
		QuantityLiters: litres, RecordedAt: at,
		CreatedBy: "test", UpdatedBy: "test",
	}); err != nil {
		t.Fatalf("record %v litres: %v", litres, err)
	}
}

// mustDay reads a calendar day written as YYYY-MM-DD.
func mustDay(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("parse day %q: %v", s, err)
	}
	return d
}

// currentDate is today as this connection reckons it, which is the day a reading
// written now is recorded under.
func currentDate(t *testing.T, pool *pgxpool.Pool) time.Time {
	t.Helper()
	var day time.Time
	if err := pool.QueryRow(context.Background(), "SELECT CURRENT_DATE").Scan(&day); err != nil {
		t.Fatalf("read current date: %v", err)
	}
	return day
}

// testPoolIn opens a pool whose connections run in the given timezone, which is
// what a deployment in a country has.
func testPoolIn(t *testing.T, zone string) *pgxpool.Pool {
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

	cfg, err := pgxpool.ParseConfig(fmt.Sprintf(tpl, testDatabase))
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	// Set on the connection rather than on the database, because ALTER DATABASE
	// takes effect only for connections opened afterwards — which is exactly why
	// this cannot be tested through the e2e harness, whose pool is already open.
	cfg.ConnConfig.RuntimeParams["timezone"] = zone

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect to %s: %v", testDatabase, err)
	}
	t.Cleanup(pool.Close)

	var got string
	if err := pool.QueryRow(ctx, "SHOW timezone").Scan(&got); err != nil {
		t.Fatalf("read timezone: %v", err)
	}
	if got != zone {
		t.Fatalf("the connection is in %s, not %s; this test would then be checking "+
			"the timezone it was written to avoid", got, zone)
	}

	// This service's own schema, and the audit trail's.
	//
	// The trail is written inside this service's transaction, so audit_logs has
	// to be reachable from its connection. That is a real deployment constraint
	// — the platform runs one database for all services — and applying it here
	// is what makes this test reflect it rather than contradict it.
	for _, rel := range [][]string{
		{"..", "db", "schema.sql"},
		{"..", "..", "..", "audit-service", "internal", "db", "schema.sql"},
	} {
		schema, err := os.ReadFile(filepath.Join(rel...))
		if err != nil {
			t.Fatalf("read %s: %v", filepath.Join(rel...), err)
		}
		if _, err := pool.Exec(ctx, string(schema)); err != nil {
			t.Fatalf("apply %s: %v", filepath.Join(rel...), err)
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
