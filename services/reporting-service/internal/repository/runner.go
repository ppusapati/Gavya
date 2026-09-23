package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/audit"

	"github.com/ppusapati/gavya/libs/integrity/tenantdb"

	"github.com/ppusapati/gavya/services/reporting-service/internal/domain"
)

// The runner's side of the table.
//
// Three properties run through all of it.
//
// The sweeps read across tenants, because a sweep has no single tenant and the
// isolation policies refuse a read with no tenant set. They go through the
// definer-rights functions in schema.sql, which limit that to these tables and
// these questions rather than handing the sweep a way round the policies in
// general. Every write afterwards runs under the row's own tenant.
//
// A claim and its read are one transaction. The functions lock with FOR UPDATE
// SKIP LOCKED, and a lock only means anything while the transaction that took
// it is open — so reading in one transaction and marking in another would let
// two replicas claim the same report and produce it twice.
//
// Nothing is deleted on failure. A report that could not be produced stays in
// the table saying why, because the person who asked for it needs to see that
// it failed and what to correct.

// ReportContent fetches one report including its bytes.
func (r *repo) ReportContent(ctx context.Context, id, tenantID string) (*domain.Report, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+reportCols+` FROM reports WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID)
	return scanReport(row)
}

// ClaimReports takes up to limit pending reports and marks them running.
//
// The claim and the read are one transaction for the reason above. attempts is
// incremented here rather than on failure, so a report that kills the process
// mid-render has still used one — otherwise a crash loop retries it for ever
// and the count never moves.
func (r *repo) ClaimReports(ctx context.Context, limit, maxAttempts int) ([]*domain.Report, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	rows, err := tx.Query(ctx,
		`SELECT `+reportCols+` FROM gavya_reporting_reports_to_run($1,$2)`, limit, maxAttempts)
	if err != nil {
		return nil, err
	}
	var claimed []*domain.Report
	for rows.Next() {
		rep, err := scanReport(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		claimed = append(claimed, rep)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(claimed) == 0 {
		return nil, tx.Commit(ctx)
	}

	ids := make([]string, 0, len(claimed))
	for _, rep := range claimed {
		ids = append(ids, rep.ID)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE reports SET status='running', started_at=NOW(), attempts=attempts+1,
		        updated_at=NOW(), updated_by=$2
		  WHERE id = ANY($1)`, ids, serviceName); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	// Reflect what was just written, so the runner's copy says what the table
	// says rather than what it said a moment before the claim.
	for _, rep := range claimed {
		rep.Status = "running"
		rep.Attempts++
	}
	return claimed, nil
}

// ReportSucceeded stores what the render produced.
//
// file_path is set to a locator rather than left empty, because
// GetReportDownloadURL refuses a report whose path is blank and a completed
// report that cannot be asked for is one nobody can reach. What it says is
// where the bytes actually are, which is this row — it is not a URL and this
// platform has never had one to give.
func (r *repo) ReportSucceeded(ctx context.Context, rep *domain.Report) error {
	ctx = tenantdb.WithTenant(ctx, rep.TenantID)
	_, err := r.pool.Exec(ctx,
		`UPDATE reports
		    SET status='completed', completed_at=NOW(),
		        content=$3, content_type=$4, row_count=$5, truncated=$6,
		        file_path=$7, failure_reason=NULL,
		        updated_at=NOW(), updated_by=$8
		  WHERE id=$1 AND tenant_id=$2`,
		rep.ID, rep.TenantID,
		rep.Content, rep.ContentType, rep.RowCount, rep.Truncated,
		fmt.Sprintf("reporting_service.reports/%s#content", rep.ID),
		serviceName)
	return err
}

// ReportFailed records why, and whether to try again.
//
// permanent is for the failures no amount of retrying fixes: a report type
// nobody wrote, parameters that do not parse, a source this deployment was
// never told how to reach. Those go straight to failed. Everything else goes
// back to pending and is picked up again until its attempts run out, because a
// procurement-service that was restarting is a reason to try once more.
func (r *repo) ReportFailed(ctx context.Context, id, reason string, permanent bool) error {
	status := "pending"
	if permanent {
		status = "failed"
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE reports
		    SET status = CASE
		                   WHEN $3::text = 'failed' THEN 'failed'
		                   -- Out of attempts is failed whatever the caller
		                   -- asked for: a report put back one more time than
		                   -- its budget allows is one that never settles.
		                   WHEN attempts >= $4 THEN 'failed'
		                   ELSE 'pending'
		                 END,
		        failure_reason=$2,
		        completed_at = CASE WHEN $3::text='failed' OR attempts >= $4 THEN NOW() ELSE NULL END,
		        updated_at=NOW(), updated_by=$5
		  WHERE id=$1`,
		id, reason, status, MaxAttempts, serviceName)
	return err
}

// MaxAttempts is how many times one report is tried.
//
// Three. A transient fault — a service restarting, a connection reset — is past
// within three sweeps; anything still failing on the third is a fault a person
// has to look at, and going on retrying it only buries the reason under
// identical log lines.
const MaxAttempts = 3

// AbandonedAfter is how long a claimed report may sit in 'running'.
//
// Ten minutes. There is no way out of that state except a runner finishing or a
// runner dying, so anything older than a generous render is the second. The
// value is generous because reclaiming a report that is still being produced
// makes two copies of the work.
const AbandonedAfter = 10 * time.Minute

// ReclaimAbandonedReports puts back what a dead process was holding.
//
// Without this a pod killed mid-render leaves its reports in 'running' for
// ever: the pending sweep does not see them, nothing else touches them, and the
// person who asked watches a report that says it is being worked on by a
// process that no longer exists.
func (r *repo) ReclaimAbandonedReports(ctx context.Context, olderThan time.Duration, limit, maxAttempts int) (int, int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	rows, err := tx.Query(ctx,
		`SELECT `+reportCols+` FROM gavya_reporting_reports_abandoned($1,$2)`, olderThan, limit)
	if err != nil {
		return 0, 0, err
	}
	var stuck []*domain.Report
	for rows.Next() {
		rep, err := scanReport(rows)
		if err != nil {
			rows.Close()
			return 0, 0, err
		}
		stuck = append(stuck, rep)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	if len(stuck) == 0 {
		return 0, 0, tx.Commit(ctx)
	}

	var requeue, giveUp []string
	for _, rep := range stuck {
		if int(rep.Attempts) >= maxAttempts {
			giveUp = append(giveUp, rep.ID)
		} else {
			requeue = append(requeue, rep.ID)
		}
	}

	const why = "the process producing this report stopped before it finished"
	if len(requeue) > 0 {
		if _, err := tx.Exec(ctx,
			`UPDATE reports SET status='pending', started_at=NULL, failure_reason=$2,
			        updated_at=NOW(), updated_by=$3
			  WHERE id = ANY($1)`, requeue, why, serviceName); err != nil {
			return 0, 0, err
		}
	}
	if len(giveUp) > 0 {
		if _, err := tx.Exec(ctx,
			`UPDATE reports SET status='failed', completed_at=NOW(), failure_reason=$2,
			        updated_at=NOW(), updated_by=$3
			  WHERE id = ANY($1)`, giveUp,
			why+", and it has had every attempt it is allowed; request it again once "+
				"whatever stopped the run has been dealt with", serviceName); err != nil {
			return 0, 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return len(requeue), len(giveUp), nil
}

// DueSchedules takes up to limit schedules whose firing has passed.
//
// Unlike ClaimReports this does not mark anything: a schedule is not claimed,
// it is fired, and ScheduleFired moves next_run_at forward in the same breath.
// The rows stay locked only for the length of this call, so two replicas
// sweeping at once will both see an unfired schedule — which is why the caller
// fires and advances one schedule at a time, and why advancing is conditional
// on next_run_at not having moved underneath it.
func (r *repo) DueSchedules(ctx context.Context, limit int) ([]*domain.ReportSchedule, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+scheduleCols+` FROM gavya_reporting_schedules_due($1)`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.ReportSchedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ScheduleFired records a firing and when the next one is.
//
// lastError is written whether or not it is empty, so a schedule that failed
// last week and worked today does not go on showing last week's reason.
//
// A nil nextRun means the schedule has no further firing — a cron this package
// could not read, or one whose expression cannot recur. next_run_at is left
// where it is in that case rather than cleared, because a NULL is what the
// sweep treats as "never computed" and would make the schedule due again
// immediately, failing the same way every few seconds for ever.
func (r *repo) ScheduleFired(ctx context.Context, id string, firedAt time.Time, nextRun *time.Time, lastError string) error {
	if nextRun == nil {
		// Deactivating is the one thing this function does that somebody needs
		// to be able to find afterwards. A schedule that has stopped producing
		// its report is noticed weeks later, when the report is missed, and by
		// then the row says only that it is inactive — not what it was, not
		// when it stopped, and not why. So this branch keeps the row as it
		// stood, in the same transaction as the change.
		//
		// The ordinary branch below writes no trail: moving next_run_at forward
		// is the runner's own bookkeeping, and a row per firing would bury this
		// one under a thousand of them.
		tx, err := r.pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

		before, err := scanSchedule(tx.QueryRow(ctx,
			`SELECT `+scheduleCols+` FROM report_schedules WHERE id=$1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE report_schedules
			    SET last_run_at=$2, last_error=NULLIF($3,''), is_active=false,
			        updated_at=NOW(), updated_by=$4
			  WHERE id=$1`,
			id, firedAt, lastError, serviceName); err != nil {
			return err
		}
		if err := audit.Write(ctx, tx, r.ids, audit.Entry{
			Action: "deactivate_report_schedule", ResourceType: "report_schedule", ResourceID: id,
			Before:      before,
			ServiceName: serviceName,
		}); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE report_schedules
		    SET last_run_at=$2, next_run_at=$3, last_error=NULLIF($4,''),
		        updated_at=NOW(), updated_by=$5
		  WHERE id=$1`,
		id, firedAt, *nextRun, lastError, serviceName)
	return err
}
