package runner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ppusapati/gavya/services/reporting-service/internal/domain"
	"github.com/ppusapati/gavya/services/reporting-service/internal/report"
)

// ---------------------------------------------------------------------------
// A store that behaves like the table, so the runner's decisions can be read
// off what it did rather than off what it was called with.
// ---------------------------------------------------------------------------

type store struct {
	mu sync.Mutex

	reports   map[string]*domain.Report
	schedules map[string]*domain.ReportSchedule

	created  []*domain.Report
	reclaims []time.Duration

	claimErr  error
	storeErr  error
	createErr error
}

func newStore() *store {
	return &store{
		reports:   map[string]*domain.Report{},
		schedules: map[string]*domain.ReportSchedule{},
	}
}

func (s *store) add(r *domain.Report) *domain.Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reports[r.ID] = r
	return r
}

func (s *store) get(id string) *domain.Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reports[id]
}

func (s *store) schedule(sc *domain.ReportSchedule) *domain.ReportSchedule {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.schedules[sc.ID] = sc
	return sc
}

func (s *store) ClaimReports(_ context.Context, limit, maxAttempts int) ([]*domain.Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	var out []*domain.Report
	for _, r := range s.reports {
		if r.Status != "pending" || int(r.Attempts) >= maxAttempts {
			continue
		}
		r.Status = "running"
		r.Attempts++
		now := time.Now()
		r.StartedAt = &now
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *store) ReportSucceeded(_ context.Context, r *domain.Report) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.storeErr != nil {
		return s.storeErr
	}
	held := s.reports[r.ID]
	held.Status = "completed"
	held.Content = r.Content
	held.ContentType = r.ContentType
	held.RowCount = r.RowCount
	held.Truncated = r.Truncated
	held.FailureReason = ""
	return nil
}

func (s *store) ReportFailed(_ context.Context, id, reason string, permanent bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	held := s.reports[id]
	held.FailureReason = reason
	if permanent || int(held.Attempts) >= MaxAttempts {
		held.Status = "failed"
	} else {
		held.Status = "pending"
	}
	return nil
}

func (s *store) ReclaimAbandonedReports(_ context.Context, olderThan time.Duration, _, maxAttempts int) (int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reclaims = append(s.reclaims, olderThan)
	requeued, failed := 0, 0
	for _, r := range s.reports {
		if r.Status != "running" || r.StartedAt == nil {
			continue
		}
		if time.Since(*r.StartedAt) < olderThan {
			continue
		}
		if int(r.Attempts) >= maxAttempts {
			r.Status = "failed"
			failed++
		} else {
			r.Status = "pending"
			r.StartedAt = nil
			requeued++
		}
	}
	return requeued, failed, nil
}

func (s *store) DueSchedules(_ context.Context, limit int) ([]*domain.ReportSchedule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*domain.ReportSchedule
	for _, sc := range s.schedules {
		if !sc.IsActive {
			continue
		}
		if sc.NextRunAt != nil && sc.NextRunAt.After(time.Now()) {
			continue
		}
		out = append(out, sc)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *store) ScheduleFired(_ context.Context, id string, firedAt time.Time, nextRun *time.Time, lastError string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sc := s.schedules[id]
	sc.LastRunAt = &firedAt
	sc.LastError = lastError
	if nextRun == nil {
		sc.IsActive = false
		return nil
	}
	sc.NextRunAt = nextRun
	return nil
}

func (s *store) CreateReport(_ context.Context, r *domain.Report) (*domain.Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createErr != nil {
		return nil, s.createErr
	}
	s.reports[r.ID] = r
	s.created = append(s.created, r)
	return r, nil
}

type testLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *testLog) Infof(f string, a ...any)  { l.write("INFO "+f, a...) }
func (l *testLog) Errorf(f string, a ...any) { l.write("ERROR "+f, a...) }
func (l *testLog) write(f string, a ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(f, a...))
}
func (l *testLog) all() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

type seqIDs struct {
	mu sync.Mutex
	n  int
}

func (s *seqIDs) NewID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.n++
	return fmt.Sprintf("RPT_TEST_%09d", s.n)
}

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

type rows struct {
	list []report.Collection
	err  error
}

func (f *rows) Collections(_ context.Context, _ string, _, _ time.Time, _ int) ([]report.Collection, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.list, nil
}

func build(t *testing.T, s *store, src *report.Sources, at time.Time) (*Runner, *testLog) {
	t.Helper()
	log := &testLog{}
	return New(Config{
		Store: s, Sources: src, Log: log, IDs: &seqIDs{}, Clock: fixedClock{at: at},
	}), log
}

func pending(id, kind, params string) *domain.Report {
	return &domain.Report{
		ID: id, TenantID: "TEN_1", Name: id, ReportType: kind,
		Parameters: params, Status: "pending", FileFormat: "csv",
	}
}

// ---------------------------------------------------------------------------

// The thing this package exists to do.
func TestAPendingReportIsProducedAndStored(t *testing.T) {
	s := newStore()
	s.add(pending("RPT_1", "collections", `{"from":"2026-09-01","to":"2026-09-02"}`))
	src := &report.Sources{Collections: &rows{list: []report.Collection{
		{ProducerRef: "PRD_1", CollectedOn: "2026-09-01", Quantity: "6.250", Amount: "242.19"},
	}}}
	r, _ := build(t, s, src, time.Now())

	produced, failed := r.SweepReports(context.Background())
	if produced != 1 || failed != 0 {
		t.Fatalf("produced %d, failed %d; want 1 and 0", produced, failed)
	}

	got := s.get("RPT_1")
	if got.Status != "completed" {
		t.Errorf("status is %q, want completed", got.Status)
	}
	if len(got.Content) == 0 {
		t.Error("the report was marked completed with no content stored")
	}
	if got.RowCount == nil || *got.RowCount != 1 {
		t.Errorf("row_count is %v, want 1", got.RowCount)
	}
	if !strings.Contains(string(got.Content), "242.19") {
		t.Errorf("the figure did not reach the file: %s", got.Content)
	}
	if got.FailureReason != "" {
		t.Errorf("a completed report carries a failure reason: %q", got.FailureReason)
	}
}

// A failure no retry can fix stops immediately, and says why.
//
// Retrying an unknown report type on a ten-second timer produces nothing but
// identical log lines, which is how the one line that matters gets lost.
func TestAPermanentFailureIsNotRetried(t *testing.T) {
	for _, tc := range []struct{ name, kind, params, expect string }{
		{"unknown type", "daily_yield", `{"from":"2026-09-01"}`, "unknown report type"},
		{"bad parameters", "collections", `not json`, "parameters"},
		{"missing source", "divergences", `{"from":"2026-09-01","to":"2026-09-02"}`, "SHADOW_SETTLEMENT_URL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore()
			s.add(pending("RPT_1", tc.kind, tc.params))
			// Collections is configured; the others are not, on purpose.
			r, _ := build(t, s, &report.Sources{Collections: &rows{}}, time.Now())

			r.SweepReports(context.Background())

			got := s.get("RPT_1")
			if got.Status != "failed" {
				t.Errorf("status is %q; a failure nothing can fix should not go back to the "+
					"queue to fail identically twice more", got.Status)
			}
			if got.FailureReason == "" {
				t.Fatal("it failed and recorded no reason, so the person who asked has " +
					"nothing to correct")
			}
			if !strings.Contains(got.FailureReason, tc.expect) {
				t.Errorf("the reason does not mention %q: %s", tc.expect, got.FailureReason)
			}
			if got.Attempts != 1 {
				t.Errorf("attempts is %d; a permanent failure should have used exactly one",
					got.Attempts)
			}
		})
	}
}

// A failure that might pass next time goes back to the queue, until it has had
// its attempts.
func TestATransientFailureIsRetriedAndThenGivesUp(t *testing.T) {
	s := newStore()
	s.add(pending("RPT_1", "collections", `{"from":"2026-09-01","to":"2026-09-02"}`))
	src := &report.Sources{Collections: &rows{err: errors.New("procurement is restarting")}}
	r, _ := build(t, s, src, time.Now())

	for i := 1; i <= MaxAttempts; i++ {
		r.SweepReports(context.Background())
		got := s.get("RPT_1")
		if got.Attempts != int32(i) {
			t.Fatalf("after sweep %d attempts is %d", i, got.Attempts)
		}
		want := "pending"
		if i == MaxAttempts {
			want = "failed"
		}
		if got.Status != want {
			t.Fatalf("after attempt %d of %d the status is %q, want %q",
				i, MaxAttempts, got.Status, want)
		}
	}

	// And it is not picked up again.
	produced, failed := r.SweepReports(context.Background())
	if produced+failed != 0 {
		t.Errorf("a report that has used every attempt was picked up again")
	}
	if !strings.Contains(s.get("RPT_1").FailureReason, "restarting") {
		t.Errorf("the last reason was lost: %q", s.get("RPT_1").FailureReason)
	}
}

// A render that worked and a store that did not is worth another try.
func TestAStoreFailureLeavesTheReportRetryable(t *testing.T) {
	s := newStore()
	s.add(pending("RPT_1", "collections", `{"from":"2026-09-01","to":"2026-09-02"}`))
	s.storeErr = errors.New("the database went away")
	r, _ := build(t, s, &report.Sources{Collections: &rows{}}, time.Now())

	r.SweepReports(context.Background())
	got := s.get("RPT_1")
	if got.Status != "pending" {
		t.Errorf("status is %q; the report rendered and only the write failed, which is "+
			"worth trying again", got.Status)
	}
	if !strings.Contains(got.FailureReason, "could not be stored") {
		t.Errorf("the reason does not distinguish a failed write from a failed render: %q",
			got.FailureReason)
	}
}

// A report left behind by a process that died is put back.
//
// Without this the queue quietly loses whatever a restart was holding: the
// pending sweep cannot see a row in 'running', nothing else touches it, and the
// person who asked watches a report being worked on by a process that no longer
// exists.
func TestAnAbandonedRunIsReclaimed(t *testing.T) {
	s := newStore()
	long := time.Now().Add(-2 * AbandonedAfter)
	s.add(&domain.Report{
		ID: "RPT_STUCK", TenantID: "TEN_1", ReportType: "collections",
		Parameters: `{"from":"2026-09-01","to":"2026-09-02"}`,
		Status:     "running", StartedAt: &long, Attempts: 1,
	})
	r, log := build(t, s, &report.Sources{Collections: &rows{}}, time.Now())

	r.SweepReports(context.Background())

	// Put back and then produced in the same sweep, because the reclaim runs
	// before the claim.
	if got := s.get("RPT_STUCK").Status; got != "completed" {
		t.Errorf("status is %q; the abandoned run should have been put back and produced", got)
	}
	if len(s.reclaims) == 0 {
		t.Fatal("the sweep never looked for abandoned runs")
	}
	if s.reclaims[0] != AbandonedAfter {
		t.Errorf("looked for runs older than %s, want %s", s.reclaims[0], AbandonedAfter)
	}
	if !strings.Contains(log.all(), "left behind") {
		t.Errorf("nothing was logged about the reclaim:\n%s", log.all())
	}
}

// A truncated report is announced, not just flagged.
func TestATruncatedReportIsSaidOutLoud(t *testing.T) {
	s := newStore()
	s.add(pending("RPT_1", "collections", `{"from":"2026-09-01","to":"2026-09-02"}`))
	big := make([]report.Collection, report.MaxRows+1)
	r, log := build(t, s, &report.Sources{Collections: &rows{list: big}}, time.Now())

	r.SweepReports(context.Background())

	got := s.get("RPT_1")
	if !got.Truncated {
		t.Error("the stored report does not say it stopped short")
	}
	if !strings.Contains(log.all(), "STOPPED AT THE CEILING") {
		t.Errorf("a truncated report was logged like any other:\n%s", log.all())
	}
}

// ---------------------------------------------------------------------------
// Schedules
// ---------------------------------------------------------------------------

func active(id, kind, expr, tz, params string, next *time.Time) *domain.ReportSchedule {
	return &domain.ReportSchedule{
		ID: id, TenantID: "TEN_1", ReportType: kind, Schedule: expr,
		Timezone: tz, Parameters: params, IsActive: true, NextRunAt: next,
	}
}

// A due schedule creates its report and moves on.
func TestADueScheduleFiresAndAdvances(t *testing.T) {
	india, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	// 07:00 on 23 September in India.
	at := time.Date(2026, 9, 23, 7, 0, 0, 0, india)
	past := at.Add(-time.Minute)

	s := newStore()
	s.schedule(active("RSC_1", "collections", "0 7 * * *", "Asia/Kolkata",
		`{"window":"yesterday"}`, &past))
	r, _ := build(t, s, &report.Sources{Collections: &rows{}}, at)

	if fired := r.SweepSchedules(context.Background()); fired != 1 {
		t.Fatalf("fired %d schedules, want 1", fired)
	}

	if len(s.created) != 1 {
		t.Fatalf("created %d reports, want 1", len(s.created))
	}
	made := s.created[0]
	if made.ReportType != "collections" || made.Status != "pending" {
		t.Errorf("created %+v", made)
	}
	// Yesterday in India on the 23rd is the 22nd. Read in UTC — where 07:00
	// India is 01:30 the same morning — it would also be the 22nd, so the
	// window test in the report package is what pins the zone; this pins that
	// the resolved dates reached the row.
	if !strings.Contains(made.Parameters, `"from":"2026-09-22"`) {
		t.Errorf("the created report does not carry the resolved period: %s", made.Parameters)
	}
	if !strings.Contains(made.Parameters, `"schedule_id":"RSC_1"`) {
		t.Errorf("the created report does not say which schedule asked for it: %s", made.Parameters)
	}
	if made.RequestedBy != "RSC_1" {
		t.Errorf("requested_by is %q; a schedule's report is asked for by the schedule, not "+
			"by whoever wrote it two years ago", made.RequestedBy)
	}

	sc := s.schedules["RSC_1"]
	if sc.NextRunAt == nil || !sc.NextRunAt.After(at) {
		t.Fatalf("next_run_at is %v; it has to move past the firing or this fires again "+
			"on the next sweep", sc.NextRunAt)
	}
	if h, m, _ := sc.NextRunAt.In(india).Clock(); h != 7 || m != 0 {
		t.Errorf("the next firing is at %02d:%02d in India, not 07:00", h, m)
	}
	if sc.NextRunAt.Day() != 24 {
		t.Errorf("the next daily firing after the 23rd is the 24th, not the %d", sc.NextRunAt.Day())
	}
	if sc.LastError != "" {
		t.Errorf("a schedule that worked carries an error: %q", sc.LastError)
	}
}

// A schedule that cannot produce anything still moves on.
//
// Leaving next_run_at where it is when something fails makes a broken schedule
// fire on every sweep for ever, which turns one missing report into a hundred
// failed ones an hour and buries the reason.
func TestABrokenScheduleStillAdvancesAndRecordsWhy(t *testing.T) {
	at := time.Date(2026, 9, 23, 7, 0, 0, 0, time.UTC)
	past := at.Add(-time.Minute)

	for _, tc := range []struct {
		name, kind, expr, tz, params, expect string
	}{
		{"unknown type", "daily_yield", "0 7 * * *", "UTC", `{"window":"yesterday"}`,
			"unknown report type"},
		{"not schedulable", "settlement_summary", "0 7 * * *", "UTC", `{"window":"yesterday"}`,
			"a schedule cannot supply"},
		{"no window", "collections", "0 7 * * *", "UTC", `{}`, "needs a window"},
		{"unknown window", "collections", "0 7 * * *", "UTC", `{"window":"last_fortnight"}`,
			"not one this platform resolves"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore()
			next := past
			s.schedule(active("RSC_1", tc.kind, tc.expr, tc.tz, tc.params, &next))
			r, _ := build(t, s, &report.Sources{Collections: &rows{}}, at)

			r.SweepSchedules(context.Background())

			sc := s.schedules["RSC_1"]
			if sc.LastError == "" {
				t.Fatal("the schedule failed and recorded no reason")
			}
			if !strings.Contains(sc.LastError, tc.expect) {
				t.Errorf("the reason does not mention %q: %s", tc.expect, sc.LastError)
			}
			if sc.NextRunAt == nil || !sc.NextRunAt.After(at) {
				t.Errorf("next_run_at is %v; a schedule that failed still has to move on, or "+
					"it fails again every sweep", sc.NextRunAt)
			}
			if !sc.IsActive {
				t.Error("the schedule was deactivated; its expression is readable and only " +
					"this firing failed, so it should try again next time")
			}
			if len(s.created) != 0 {
				t.Errorf("a schedule that could not produce a report created %d", len(s.created))
			}
		})
	}
}

// A schedule whose expression or zone cannot be read has no next firing, so it
// is deactivated rather than left to be found due on every sweep for ever.
func TestAnUnreadableScheduleIsDeactivatedWithTheReason(t *testing.T) {
	at := time.Date(2026, 9, 23, 7, 0, 0, 0, time.UTC)
	past := at.Add(-time.Minute)

	for _, tc := range []struct{ name, expr, tz, expect string }{
		{"bad cron", "not a cron", "UTC", "a schedule has five"},
		{"no zone", "0 7 * * *", "", "no timezone"},
		{"unknown zone", "0 7 * * *", "Mars/Olympus", "not one this platform knows"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore()
			next := past
			s.schedule(active("RSC_1", "collections", tc.expr, tc.tz, `{"window":"yesterday"}`, &next))
			r, _ := build(t, s, &report.Sources{Collections: &rows{}}, at)

			r.SweepSchedules(context.Background())

			sc := s.schedules["RSC_1"]
			if sc.IsActive {
				t.Error("a schedule with no next firing was left active, so it will be found " +
					"due on every sweep and fail identically for ever")
			}
			if !strings.Contains(sc.LastError, tc.expect) {
				t.Errorf("the reason does not mention %q: %s", tc.expect, sc.LastError)
			}
		})
	}
}

// A schedule not yet due is left alone.
func TestAScheduleNotYetDueDoesNotFire(t *testing.T) {
	at := time.Date(2026, 9, 23, 7, 0, 0, 0, time.UTC)
	future := time.Now().Add(time.Hour)

	s := newStore()
	s.schedule(active("RSC_1", "collections", "0 7 * * *", "UTC", `{"window":"yesterday"}`, &future))
	r, _ := build(t, s, &report.Sources{Collections: &rows{}}, at)

	if fired := r.SweepSchedules(context.Background()); fired != 0 {
		t.Errorf("fired %d schedules that are not due", fired)
	}
	if len(s.created) != 0 {
		t.Errorf("created %d reports from a schedule that is not due", len(s.created))
	}
}

// An inactive schedule does not fire, which is what the flag is for — and until
// this package existed, nothing read it.
func TestAnInactiveScheduleDoesNotFire(t *testing.T) {
	at := time.Date(2026, 9, 23, 7, 0, 0, 0, time.UTC)
	past := at.Add(-time.Hour)

	s := newStore()
	sc := active("RSC_1", "collections", "0 7 * * *", "UTC", `{"window":"yesterday"}`, &past)
	sc.IsActive = false
	s.schedule(sc)
	r, _ := build(t, s, &report.Sources{Collections: &rows{}}, at)

	if fired := r.SweepSchedules(context.Background()); fired != 0 {
		t.Errorf("an inactive schedule fired")
	}
}

// A schedule that has never been given a next firing is adopted rather than
// left dormant — which is the state every row written before this existed is in.
func TestAScheduleWithNoNextRunIsAdopted(t *testing.T) {
	at := time.Date(2026, 9, 23, 7, 0, 0, 0, time.UTC)

	s := newStore()
	s.schedule(active("RSC_1", "collections", "0 7 * * *", "UTC", `{"window":"yesterday"}`, nil))
	r, _ := build(t, s, &report.Sources{Collections: &rows{}}, at)

	if fired := r.SweepSchedules(context.Background()); fired != 1 {
		t.Fatal("a schedule with no next_run_at was left dormant")
	}
	if s.schedules["RSC_1"].NextRunAt == nil {
		t.Error("it fired and still has no next firing")
	}
}

// Firing kicks the report sweep, so a scheduled report is produced promptly
// rather than within the report interval.
func TestFiringAsksForASweep(t *testing.T) {
	at := time.Date(2026, 9, 23, 7, 0, 0, 0, time.UTC)
	past := at.Add(-time.Minute)

	s := newStore()
	s.schedule(active("RSC_1", "collections", "0 7 * * *", "UTC", `{"window":"yesterday"}`, &past))
	r, _ := build(t, s, &report.Sources{Collections: &rows{}}, at)

	r.SweepSchedules(context.Background())
	select {
	case <-r.kick:
	default:
		t.Error("firing a schedule did not ask for a report sweep, so the report waits for " +
			"the next tick")
	}
}
