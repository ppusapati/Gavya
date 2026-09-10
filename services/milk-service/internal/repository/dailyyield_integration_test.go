package repository

import (
	"context"
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

// A day's yield has to be right in the timezone the database actually runs in.
//
// These tests exist because of a bug that turned out not to be there, and the
// hunt is worth recording. `recorded_at::date = $3` compares a date against a
// parameter; if that parameter were a TIMESTAMPTZ, PostgreSQL would promote the
// date to midnight in the session's timezone and compare it against midnight
// UTC, and outside UTC the two never meet. This platform is written for India,
// where every daily yield would then read zero litres — a number of the right
// magnitude in the right units and entirely wrong.
//
// It does not happen, because PostgreSQL infers the parameter's type from the
// comparison and types it `date`, so pgx encodes a date and nothing is promoted.
// The experiment that suggested otherwise had an explicit `::timestamptz`
// literal written by hand in psql, which is not what the driver sends — a check
// of a statement nobody runs.
//
// So the query was already right, and what it was missing was anything saying
// so. These run it in UTC, in a zone ahead of it and in a zone behind it, which
// turns a silent reliance on type inference into something that fails if the
// inference ever changes.
//
// They are repository tests rather than end-to-end ones because reproducing a
// timezone needs control of the connection. The e2e harness opens its pool once
// at startup, so setting a database's timezone afterwards changes nothing for a
// service already running — an e2e test written for this passes without
// exercising it, which is worse than not having one.
//
//	TEST_DATABASE_DSN="postgres://user@host:port/%s?sslmode=disable" go test ./...

const testDatabase = "milk_repo_test"

// zones covers UTC, a zone ahead of it and a zone behind it. The failure this
// guards against would be invisible in the first and would move the day in
// opposite directions in the other two, so one non-UTC zone would not be enough:
// a query that shifted everything by a day would still pass with only
// Asia/Kolkata.
var zones = []string{"UTC", "Asia/Kolkata", "America/Chicago"}

func TestADaysYieldIsTheSameDayInEveryTimezone(t *testing.T) {
	for _, zone := range zones {
		t.Run(zone, func(t *testing.T) {
			pool := testPoolIn(t, zone)
			r := New(pool)
			tenant, cattle := newTestID("tnt"), newTestID("cow")
			ctx := acting(tenant)

			session := newTestID("ses")
			mustSession(t, r, tenant, cattle, session)

			// Two readings, written now. "Now" falls on whichever day the
			// database reckons it to be, which is what a plant's operators
			// would call today.
			mustRecord(t, r, tenant, session, cattle, 6.250)
			mustRecord(t, r, tenant, session, cattle, 5.750)

			today := currentDate(t, pool)
			total, err := r.GetDailyYield(ctx, tenant, cattle, today)
			if err != nil {
				t.Fatalf("get daily yield: %v", err)
			}
			if total != 12.0 {
				t.Errorf("in %s the day's yield is %v litres, want 12\n"+
					"Two readings of 6.250 and 5.750 were written today (%s) and the sum "+
					"found none of them. recorded_at::date is evaluated in the session's "+
					"timezone, and this holds only while PostgreSQL infers the parameter as "+
					"a date; bound as a timestamp it would be compared against midnight UTC, "+
					"which outside UTC is a different instant. Every animal would then "+
					"report having given nothing, which is a number somebody acts on.",
					zone, total, today.Format("2006-01-02"))
			}

			// The day before holds none of them. Without this, a query that
			// matched everything would pass the check above.
			before, err := r.GetDailyYield(ctx, tenant, cattle, today.AddDate(0, 0, -1))
			if err != nil {
				t.Fatalf("get yesterday's yield: %v", err)
			}
			if before != 0 {
				t.Errorf("in %s yesterday reports %v litres, and nothing was recorded then",
					zone, before)
			}
		})
	}
}

// A yield is one animal's, in every timezone too. A sum scoped to the wrong
// thing is the defect that looks most like a correct answer.
func TestADaysYieldCountsOneAnimal(t *testing.T) {
	pool := testPoolIn(t, "Asia/Kolkata")
	r := New(pool)
	tenant := newTestID("tnt")
	ctx := acting(tenant)

	mine, theirs := newTestID("cow"), newTestID("cow")
	session := newTestID("ses")
	mustSession(t, r, tenant, mine, session)
	mustRecord(t, r, tenant, session, mine, 6.250)
	mustRecord(t, r, tenant, session, theirs, 40.000)

	total, err := r.GetDailyYield(ctx, tenant, mine, currentDate(t, pool))
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
		session := newTestID("ses")
		mustSession(t, r, tenant, cattle, session)
		mustRecord(t, r, tenant, session, cattle, 6.250)
	}

	total, err := r.GetDailyYield(ctx, mine, cattle, currentDate(t, pool))
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
	if _, err := r.CreateRecord(acting(tenant), &domain.MilkRecord{
		ID: newTestID("rec"), TenantID: tenant, SessionID: session, CattleID: cattle,
		QuantityLiters: litres, RecordedAt: time.Now(),
		CreatedBy: "test", UpdatedBy: "test",
	}); err != nil {
		t.Fatalf("record %v litres: %v", litres, err)
	}
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
