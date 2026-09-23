// Package runner produces the reports somebody asked for, and fires the
// schedules somebody wrote down.
//
// Until this existed, reporting-service recorded intentions and acted on none
// of them. RequestReport wrote a row with status 'pending' and nothing in the
// platform ever picked one up; report_schedules had a next_run_at column that
// nothing computed and an is_active flag that nothing read. The console had to
// say so on the page, because a Generate button with a spinner would have been
// a control that reports success while doing nothing.
//
// # The two sweeps
//
// Reports and schedules are swept separately, on separate intervals, because
// they fail differently and at different rates. A schedule fires at most once a
// minute and its failure is a schedule that stops producing; a report runs when
// somebody asks and its failure is one person waiting. Sharing a loop would
// mean a slow render delaying every schedule in the tenant.
//
// # What is deliberately not here
//
// There is no retry with a growing backoff. A report is tried three times on
// the ordinary interval and then stops, because the faults that survive three
// tries are faults a person has to look at, and a report retried for an hour is
// one whose reason is buried under sixty identical log lines.
//
// There is no priority. Reports run oldest first. A queue that lets one caller
// jump it is a queue somebody games.
package runner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/observe"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"

	"github.com/ppusapati/gavya/services/reporting-service/internal/cron"
	"github.com/ppusapati/gavya/services/reporting-service/internal/domain"
	"github.com/ppusapati/gavya/services/reporting-service/internal/report"
)

// Store is what the runner needs of the table.
type Store interface {
	ClaimReports(ctx context.Context, limit, maxAttempts int) ([]*domain.Report, error)
	ReportSucceeded(ctx context.Context, r *domain.Report) error
	ReportFailed(ctx context.Context, id, reason string, permanent bool) error
	ReclaimAbandonedReports(ctx context.Context, olderThan time.Duration, limit, maxAttempts int) (int, int, error)

	DueSchedules(ctx context.Context, limit int) ([]*domain.ReportSchedule, error)
	ScheduleFired(ctx context.Context, id string, firedAt time.Time, nextRun *time.Time, lastError string) error
	CreateReport(ctx context.Context, r *domain.Report) (*domain.Report, error)
}

// Logger is what this package writes.
type Logger interface {
	Infof(string, ...any)
	Errorf(string, ...any)
}

// IDs makes an identifier for a report a schedule creates.
type IDs interface{ NewID() string }

// Clock is the current time, so a test can decide what "now" is.
type Clock interface{ Now() time.Time }

// Batch is how many rows one sweep takes.
//
// Small, because a report is rendered in memory and a sweep holds every report
// it claimed. Five at a time keeps the high-water mark to five reports rather
// than to however many somebody requested at once.
const Batch = 5

// ScheduleBatch is how many schedules one sweep fires.
const ScheduleBatch = 50

// Runner is both sweeps.
type Runner struct {
	store   Store
	sources *report.Sources
	log     Logger
	ids     IDs
	clock   Clock

	reportEvery   time.Duration
	scheduleEvery time.Duration

	// kick asks for a report sweep now rather than at the next tick, so a
	// report requested from the console is produced in about as long as it
	// takes to render rather than within the interval.
	kick chan struct{}

	// What the sweeps have done, for the gauges below.
	produced, failed, fired atomic.Int64
	lastReportSweep         atomic.Int64
	lastScheduleSweep       atomic.Int64
	pendingSeen             atomic.Int64
}

// Config is what New needs.
type Config struct {
	Store         Store
	Sources       *report.Sources
	Log           Logger
	IDs           IDs
	Clock         Clock
	ReportEvery   time.Duration
	ScheduleEvery time.Duration
}

// New builds a runner.
//
// Sources may hold nils, for a deployment that was not told how to reach a
// service. The runner still runs: a report whose source is missing fails with
// the setting named, which is how somebody finds out that the setting is
// missing. Refusing to start instead would take down the procedures that do
// work — listing reports, writing a schedule — to protect the one that does
// not.
func New(cfg Config) *Runner {
	if cfg.ReportEvery <= 0 {
		cfg.ReportEvery = 10 * time.Second
	}
	if cfg.ScheduleEvery <= 0 {
		// Once a minute, because a cron expression's finest resolution is a
		// minute. Sweeping faster cannot fire anything sooner and only costs
		// queries.
		cfg.ScheduleEvery = time.Minute
	}
	if cfg.Sources == nil {
		cfg.Sources = &report.Sources{}
	}
	return &Runner{
		store: cfg.Store, sources: cfg.Sources, log: cfg.Log,
		ids: cfg.IDs, clock: cfg.Clock,
		reportEvery: cfg.ReportEvery, scheduleEvery: cfg.ScheduleEvery,
		kick: make(chan struct{}, 1),
	}
}

// Kick asks for a report sweep now.
//
// Non-blocking and collapsing: a dozen requests in a second are one sweep,
// which picks all of them up.
func (r *Runner) Kick() {
	select {
	case r.kick <- struct{}{}:
	default:
	}
}

func (r *Runner) now() time.Time {
	if r.clock == nil {
		return time.Now()
	}
	return r.clock.Now()
}

// Run starts both sweeps and returns. They stop when ctx ends.
func (r *Runner) Run(ctx context.Context) {
	r.publishGauges()
	go r.runReports(ctx)
	go r.runSchedules(ctx)
}

func (r *Runner) runReports(ctx context.Context) {
	t := time.NewTicker(r.reportEvery)
	defer t.Stop()
	for {
		r.SweepReports(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-r.kick:
		}
	}
}

func (r *Runner) runSchedules(ctx context.Context) {
	t := time.NewTicker(r.scheduleEvery)
	defer t.Stop()
	for {
		r.SweepSchedules(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// SweepReports claims what is pending, produces it, and records what happened.
func (r *Runner) SweepReports(ctx context.Context) (produced, failed int) {
	defer func() { r.lastReportSweep.Store(r.now().Unix()) }()

	// Abandoned rows first. A report left in 'running' by a process that died
	// is invisible to the claim below, so without this the queue quietly loses
	// whatever a restart was holding.
	if requeued, gaveUp, err := r.store.ReclaimAbandonedReports(
		ctx, AbandonedAfter, Batch, MaxAttempts); err != nil {
		r.log.Errorf("reports: looking for abandoned runs: %v", err)
	} else if requeued+gaveUp > 0 {
		r.log.Infof("reports: %d run(s) were left behind by a process that stopped; "+
			"%d put back and %d given up on", requeued+gaveUp, requeued, gaveUp)
	}

	claimed, err := r.store.ClaimReports(ctx, Batch, MaxAttempts)
	if err != nil {
		r.log.Errorf("reports: claiming work: %v", err)
		return 0, 0
	}
	r.pendingSeen.Store(int64(len(claimed)))
	if len(claimed) == 0 {
		return 0, 0
	}

	for _, rep := range claimed {
		if r.produce(ctx, rep) {
			produced++
		} else {
			failed++
		}
	}
	r.produced.Add(int64(produced))
	r.failed.Add(int64(failed))
	return produced, failed
}

// produce renders one report and records the outcome. It reports success.
func (r *Runner) produce(ctx context.Context, rep *domain.Report) bool {
	// Under the row's own tenant from here on. The claim read across tenants
	// through a definer-rights function; everything after it is an ordinary
	// tenant-scoped operation, and the services this calls are handed the same
	// tenant so their policies apply too.
	tctx := tenantdb.WithTenant(ctx, rep.TenantID)

	params, err := report.ParseParams(rep.Parameters)
	if err != nil {
		return r.recordFailure(tctx, rep, err)
	}

	out, err := report.Render(tctx, r.sources, rep.ReportType, rep.TenantID, params)
	if err != nil {
		return r.recordFailure(tctx, rep, err)
	}

	rows := out.Rows
	rep.Content, rep.ContentType, rep.RowCount, rep.Truncated =
		out.Content, out.ContentType, &rows, out.Truncated

	if err := r.store.ReportSucceeded(tctx, rep); err != nil {
		// The render worked and the write did not, so this is worth another
		// try: the report is put back rather than failed.
		r.log.Errorf("reports: %s rendered and could not be stored: %v", rep.ID, err)
		if ferr := r.store.ReportFailed(tctx, rep.ID,
			"the report was produced and could not be stored: "+err.Error(), false); ferr != nil {
			r.log.Errorf("reports: %s: recording that failure also failed: %v", rep.ID, ferr)
		}
		return false
	}

	if out.Truncated {
		// Said in the log as well as in the row, because a truncated report is
		// a total somebody will act on.
		r.log.Infof("reports: %s (%s) produced %d rows and STOPPED AT THE CEILING; "+
			"the figures in it are not the whole period", rep.ID, rep.ReportType, out.Rows)
	} else {
		r.log.Infof("reports: %s (%s) produced %d rows", rep.ID, rep.ReportType, out.Rows)
	}
	return true
}

// recordFailure writes why, and decides whether it is worth trying again.
func (r *Runner) recordFailure(ctx context.Context, rep *domain.Report, cause error) bool {
	permanent := isPermanent(cause)
	reason := cause.Error()

	if permanent {
		r.log.Errorf("reports: %s (%s) cannot be produced and will not be retried: %v",
			rep.ID, rep.ReportType, cause)
	} else {
		r.log.Errorf("reports: %s (%s) failed on attempt %d of %d: %v",
			rep.ID, rep.ReportType, rep.Attempts, MaxAttempts, cause)
	}
	if err := r.store.ReportFailed(ctx, rep.ID, reason, permanent); err != nil {
		r.log.Errorf("reports: %s: recording that failure also failed: %v", rep.ID, err)
	}
	return false
}

// isPermanent says whether retrying could ever help.
//
// An unknown report type stays unknown, parameters that do not parse do not
// start parsing, and a source this deployment was never told how to reach stays
// unreachable until somebody sets the URL and restarts. Retrying any of those
// on a ten-second timer produces nothing but identical log lines, which is how
// the one line that matters gets lost.
func isPermanent(err error) bool {
	return errors.Is(err, report.ErrUnknownKind) ||
		errors.Is(err, report.ErrBadParameters) ||
		errors.Is(err, report.ErrNoSource)
}

// MaxAttempts and AbandonedAfter mirror the repository's, which is where the
// SQL that enforces them lives. Declared here too so this package reads
// without following the interface into another one.
const (
	MaxAttempts    = 3
	AbandonedAfter = 10 * time.Minute
)

// SweepSchedules fires every schedule whose time has come.
func (r *Runner) SweepSchedules(ctx context.Context) (fired int) {
	defer func() { r.lastScheduleSweep.Store(r.now().Unix()) }()

	due, err := r.store.DueSchedules(ctx, ScheduleBatch)
	if err != nil {
		r.log.Errorf("schedules: reading what is due: %v", err)
		return 0
	}
	for _, s := range due {
		if r.fire(ctx, s) {
			fired++
		}
	}
	r.fired.Add(int64(fired))
	return fired
}

// fire creates the report one schedule asks for and moves it on.
//
// A schedule always moves on, whether or not the report could be created. The
// alternative — leaving next_run_at where it is when something fails — makes a
// broken schedule fire on every sweep for ever, which turns one missing report
// into a hundred failed ones an hour and buries the reason.
func (r *Runner) fire(ctx context.Context, s *domain.ReportSchedule) bool {
	at := r.now()
	tctx := tenantdb.WithTenant(ctx, s.TenantID)

	created, problem := r.createFrom(tctx, s, at)
	if problem != "" {
		r.log.Errorf("schedules: %s (%s, %s) did not produce a report: %s",
			s.ID, s.ReportType, s.Schedule, problem)
	}

	next, nextErr := r.nextFiring(s, at)
	if nextErr != "" {
		// A schedule whose expression or zone cannot be read has no next
		// firing, so it is deactivated rather than left to be found due on
		// every sweep. The reason is on the row, and reactivating it after a
		// correction is one call.
		r.log.Errorf("schedules: %s has been deactivated: %s", s.ID, nextErr)
		if problem == "" {
			problem = nextErr
		} else {
			problem += "; " + nextErr
		}
	}

	if err := r.store.ScheduleFired(tctx, s.ID, at, next, problem); err != nil {
		r.log.Errorf("schedules: %s fired and recording it failed: %v", s.ID, err)
		return false
	}
	return created
}

// createFrom builds the report a schedule asks for.
//
// The second return is a reason, empty when it worked. It is a string rather
// than an error because it is going into a column somebody reads, and every
// caller of this does the same thing with it.
func (r *Runner) createFrom(ctx context.Context, s *domain.ReportSchedule, at time.Time) (bool, string) {
	kind, err := report.Lookup(s.ReportType)
	if err != nil {
		return false, err.Error()
	}
	if !kind.Schedulable {
		return false, fmt.Sprintf("%s names something a schedule cannot supply, so it can only "+
			"be asked for one report at a time: %s", s.ReportType, kind.Summary)
	}

	loc, locErr := loadZone(s.Timezone)
	if locErr != "" {
		return false, locErr
	}

	declared, err := report.ParseParams(s.Parameters)
	if err != nil {
		return false, err.Error()
	}

	window, err := report.ResolveWindow(declared["window"], at, loc)
	if err != nil {
		return false, err.Error()
	}

	// The resolved period is what the report is asked for, and the window
	// keyword is kept beside it so the row says which rule produced these
	// dates. A reader looking at two of these a fortnight apart can see they
	// are the same schedule rather than two different requests.
	params := fmt.Sprintf(`{"from":%q,"to":%q,"window":%q,"schedule_id":%q}`,
		window["from"], window["to"], declared["window"], s.ID)

	name := fmt.Sprintf("%s, %s to %s", s.ReportType, window["from"], window["to"])
	format := "csv"

	if _, err := r.store.CreateReport(ctx, &domain.Report{
		ID:         r.ids.NewID(),
		TenantID:   s.TenantID,
		Name:       name,
		ReportType: s.ReportType,
		Parameters: params,
		Status:     "pending",
		FileFormat: format,
		// Attributed to the schedule rather than to whoever wrote it. The
		// person who set a schedule up two years ago did not ask for this
		// morning's report, and putting their name on it would say they did.
		RequestedBy: s.ID,
		CreatedBy:   "reporting-service",
		UpdatedBy:   "reporting-service",
	}); err != nil {
		return false, "the report could not be created: " + err.Error()
	}

	r.log.Infof("schedules: %s fired and asked for %s covering %s to %s",
		s.ID, s.ReportType, window["from"], window["to"])
	// Produce it now rather than within the interval.
	r.Kick()
	return true, ""
}

// nextFiring works out when this schedule fires next.
func (r *Runner) nextFiring(s *domain.ReportSchedule, after time.Time) (*time.Time, string) {
	loc, locErr := loadZone(s.Timezone)
	if locErr != "" {
		return nil, locErr
	}
	expr, err := cron.Parse(s.Schedule)
	if err != nil {
		return nil, err.Error()
	}
	next, err := expr.Next(after, loc)
	if err != nil {
		return nil, err.Error()
	}
	return &next, ""
}

// loadZone reads a schedule's timezone.
//
// An empty zone is refused rather than defaulted. UTC is not the co-operative's
// zone and picking it here would be this platform deciding what time a society
// starts work — quietly, and differently from what they typed.
func loadZone(name string) (*time.Location, string) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return nil, "this schedule has no timezone, and a time of day means nothing without " +
			"one; set it to the society's own zone, such as Asia/Kolkata"
	}
	loc, err := time.LoadLocation(trimmed)
	if err != nil {
		return nil, fmt.Sprintf("timezone %q is not one this platform knows: %v", trimmed, err)
	}
	return loc, ""
}

// publishGauges makes both sweeps watchable.
//
// The failure these guard is the one this whole package was written to remove:
// work that is recorded and never done. A runner that has stopped leaves every
// request correctly written down and nobody producing any of them, and nothing
// else in the platform looks wrong — which is exactly the state reporting-
// service was in before.
//
// So the numbers that matter are the staleness ones. A sweep count rising is
// reassuring and a sweep count that has stopped rising is indistinguishable
// from a quiet fortnight; "how long since a sweep ran at all" is not.
func (r *Runner) publishGauges() {
	since := func(v *atomic.Int64) float64 {
		at := v.Load()
		if at == 0 {
			// -1 until the first sweep. Zero would read as "swept just now",
			// which is the reassuring answer and one nobody has established.
			return -1
		}
		return time.Since(time.Unix(at, 0)).Seconds()
	}

	observe.Publish(observe.Gauge{
		Name: "gavya_reporting_report_sweep_seconds_ago",
		Help: "How long since the report runner last swept. Rises without limit if it stops. " +
			"-1 before the first sweep.",
		Read: func() float64 { return since(&r.lastReportSweep) },
	})
	observe.Publish(observe.Gauge{
		Name: "gavya_reporting_schedule_sweep_seconds_ago",
		Help: "How long since the schedule runner last swept. Rises without limit if it stops. " +
			"-1 before the first sweep.",
		Read: func() float64 { return since(&r.lastScheduleSweep) },
	})
	observe.Publish(observe.Gauge{
		Name: "gavya_reporting_reports_produced_total",
		Help: "Reports this process has produced since it started.",
		Read: func() float64 { return float64(r.produced.Load()) },
	})
	observe.Publish(observe.Gauge{
		Name: "gavya_reporting_reports_failed_total",
		Help: "Reports this process has failed to produce since it started.",
		Read: func() float64 { return float64(r.failed.Load()) },
	})
	observe.Publish(observe.Gauge{
		Name: "gavya_reporting_schedules_fired_total",
		Help: "Schedules this process has fired since it started.",
		Read: func() float64 { return float64(r.fired.Load()) },
	})
	observe.Publish(observe.Gauge{
		Name: "gavya_reporting_reports_claimed_last_sweep",
		Help: "How many pending reports the last sweep took. At the batch ceiling on every " +
			"sweep, the queue is growing faster than it is drained.",
		Read: func() float64 { return float64(r.pendingSeen.Load()) },
	})
}
