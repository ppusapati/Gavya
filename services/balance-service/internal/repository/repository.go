package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/services/balance-service/internal/domain"

	"github.com/ppusapati/gavya/libs/integrity/audit"
)

var (
	ErrNotFound = errors.New("not found")
	// ErrDuplicateWindow means a window already covers that route and period.
	ErrDuplicateWindow = errors.New("a balance window already covers that route and period")
	// ErrDuplicateFlow means the flow id is already used in that window.
	ErrDuplicateFlow = errors.New("that flow id is already recorded in this window")
	// ErrAlreadyAccepted means the window already has an accepted run.
	ErrAlreadyAccepted = errors.New("this window already has an accepted run")
)

type Repository interface {
	CreateWindow(ctx context.Context, w *domain.BalanceWindow) (*domain.BalanceWindow, error)
	GetWindow(ctx context.Context, tenantID, id string) (*domain.BalanceWindow, error)
	ListWindows(ctx context.Context, tenantID string, status domain.WindowStatus, limit, offset int) ([]*domain.BalanceWindow, error)

	AddFlow(ctx context.Context, f *domain.FlowMeasurement) (*domain.FlowMeasurement, error)
	ListFlows(ctx context.Context, tenantID, windowID string) ([]domain.FlowMeasurement, error)

	CreateRun(ctx context.Context, run *domain.ReconciliationRun) (*domain.ReconciliationRun, error)
	GetRun(ctx context.Context, tenantID, id string) (*domain.ReconciliationRun, error)
	ListRuns(ctx context.Context, tenantID, windowID string, limit, offset int) ([]*domain.ReconciliationRun, error)
	AcceptRun(ctx context.Context, tenantID, runID, actor string) (*domain.ReconciliationRun, error)
}

// serviceName is what this service's audit entries are attributed to.
const serviceName = "balance-service"

type repo struct {
	db  *pgxpool.Pool
	ids audit.IDs
}

func New(db *pgxpool.Pool, ids audit.IDs) Repository { return &repo{db: db, ids: ids} }

const windowCols = `id,tenant_id,route_ref,period_start,period_end,unit,status,
	created_at,updated_at,created_by,updated_by`

func (r *repo) CreateWindow(ctx context.Context, w *domain.BalanceWindow) (*domain.BalanceWindow, error) {
	const q = `INSERT INTO balance_windows
		(id,tenant_id,route_ref,period_start,period_end,unit,status,created_by,updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,'OPEN',$7,$7)
		RETURNING ` + windowCols

	out, err := scanWindow(r.db.QueryRow(ctx, q,
		w.ID, w.TenantID, w.RouteRef, w.PeriodStart, w.PeriodEnd, string(w.Unit), w.CreatedBy))
	if isUniqueViolation(err, "uq_window_period") {
		return nil, ErrDuplicateWindow
	}
	return out, err
}

func (r *repo) GetWindow(ctx context.Context, tenantID, id string) (*domain.BalanceWindow, error) {
	const q = `SELECT ` + windowCols + ` FROM balance_windows WHERE tenant_id=$1 AND id=$2`
	return scanWindow(r.db.QueryRow(ctx, q, tenantID, id))
}

func (r *repo) ListWindows(ctx context.Context, tenantID string, status domain.WindowStatus, limit, offset int) ([]*domain.BalanceWindow, error) {
	const q = `SELECT ` + windowCols + ` FROM balance_windows
		WHERE tenant_id=$1 AND ($2='' OR status=$2)
		ORDER BY period_start DESC, route_ref LIMIT $3 OFFSET $4`

	rows, err := r.db.Query(ctx, q, tenantID, string(status), limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*domain.BalanceWindow, 0, limit)
	for rows.Next() {
		w, err := scanWindow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

const flowCols = `id,tenant_id,window_id,flow_id,from_node_id,from_node_kind,to_node_id,to_node_kind,
	measured::text,COALESCE(standard_uncertainty::text,''),unmeasured,observation_ref,created_at,created_by`

func (r *repo) AddFlow(ctx context.Context, f *domain.FlowMeasurement) (*domain.FlowMeasurement, error) {
	const q = `INSERT INTO flow_measurements
		(id,tenant_id,window_id,flow_id,from_node_id,from_node_kind,to_node_id,to_node_kind,
		 measured,standard_uncertainty,unmeasured,observation_ref,created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::text::numeric,$10::text::numeric,$11,$12,$13)
		RETURNING ` + flowCols

	out, err := scanFlow(r.db.QueryRow(ctx, q,
		f.ID, f.TenantID, f.WindowID, f.FlowID,
		f.From.ID, string(f.From.Kind), f.To.ID, string(f.To.Kind),
		f.Measured, nullable(f.StandardUncertainty), f.Unmeasured, f.ObservationRef, f.CreatedBy))
	if isUniqueViolation(err, "uq_flow_in_window") {
		return nil, ErrDuplicateFlow
	}
	return out, err
}

func (r *repo) ListFlows(ctx context.Context, tenantID, windowID string) ([]domain.FlowMeasurement, error) {
	const q = `SELECT ` + flowCols + ` FROM flow_measurements
		WHERE tenant_id=$1 AND window_id=$2 ORDER BY flow_id`

	rows, err := r.db.Query(ctx, q, tenantID, windowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.FlowMeasurement
	for rows.Next() {
		f, err := scanFlow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

const runCols = `id,tenant_id,window_id,converged,residual_before::text,residual_after::text,
	model_version,gross_error_threshold,reason,accepted_at,COALESCE(accepted_by,''),created_at,created_by`

const reconciledFlowCols = `run_id,tenant_id,flow_id,measured::text,reconciled::text,adjustment::text,
	test_statistic,gross_error,unmeasured`

// CreateRun writes the run, its per-flow verdicts and the window's new status in
// one transaction. A run whose flow rows only partly landed would nominate a
// culprit out of a network nobody could reconstruct.
func (r *repo) CreateRun(ctx context.Context, run *domain.ReconciliationRun) (*domain.ReconciliationRun, error) {
	if err := domain.ValidateRun(run); err != nil {
		return nil, err
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	const insertRun = `INSERT INTO reconciliation_runs
		(id,tenant_id,window_id,converged,residual_before,residual_after,
		 model_version,gross_error_threshold,reason,created_by)
		VALUES ($1,$2,$3,$4,$5::text::numeric,$6::text::numeric,$7,$8,$9,$10)
		RETURNING ` + runCols

	out, err := scanRun(tx.QueryRow(ctx, insertRun,
		run.ID, run.TenantID, run.WindowID, run.Converged,
		run.ResidualBefore, run.ResidualAfter,
		run.ModelVersion, run.GrossErrorThreshold, run.Reason, run.CreatedBy))
	if err != nil {
		return nil, fmt.Errorf("insert run: %w", err)
	}

	const insertFlow = `INSERT INTO reconciled_flows
		(run_id,tenant_id,flow_id,measured,reconciled,adjustment,test_statistic,gross_error,unmeasured)
		VALUES ($1,$2,$3,$4::text::numeric,$5::text::numeric,$6::text::numeric,$7,$8,$9)`

	for _, f := range run.Flows {
		if _, err := tx.Exec(ctx, insertFlow,
			run.ID, run.TenantID, f.FlowID, f.Measured, f.Reconciled, f.Adjustment,
			f.TestStatistic, f.GrossError, f.Unmeasured); err != nil {
			return nil, fmt.Errorf("insert reconciled flow %q: %w", f.FlowID, err)
		}
		out.Flows = append(out.Flows, domain.ReconciledFlow{
			RunID: run.ID, TenantID: run.TenantID, FlowID: f.FlowID,
			Measured: f.Measured, Reconciled: f.Reconciled, Adjustment: f.Adjustment,
			TestStatistic: f.TestStatistic, GrossError: f.GrossError, Unmeasured: f.Unmeasured,
		})
	}

	// An accepted window is closed; a later run must not silently reopen it.
	const advance = `UPDATE balance_windows SET status='RECONCILED', updated_at=NOW(), updated_by=$3
		WHERE tenant_id=$1 AND id=$2 AND status <> 'ACCEPTED'`
	if _, err := tx.Exec(ctx, advance, run.TenantID, run.WindowID, run.CreatedBy); err != nil {
		return nil, fmt.Errorf("advance window: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return out, nil
}

func (r *repo) GetRun(ctx context.Context, tenantID, id string) (*domain.ReconciliationRun, error) {
	const q = `SELECT ` + runCols + ` FROM reconciliation_runs WHERE tenant_id=$1 AND id=$2`
	run, err := scanRun(r.db.QueryRow(ctx, q, tenantID, id))
	if err != nil {
		return nil, err
	}
	if run.Flows, err = r.reconciledFlows(ctx, tenantID, id); err != nil {
		return nil, err
	}
	return run, nil
}

func (r *repo) ListRuns(ctx context.Context, tenantID, windowID string, limit, offset int) ([]*domain.ReconciliationRun, error) {
	const q = `SELECT ` + runCols + ` FROM reconciliation_runs
		WHERE tenant_id=$1 AND ($2='' OR window_id=$2)
		ORDER BY created_at DESC LIMIT $3 OFFSET $4`

	rows, err := r.db.Query(ctx, q, tenantID, windowID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*domain.ReconciliationRun, 0, limit)
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, run := range out {
		if run.Flows, err = r.reconciledFlows(ctx, tenantID, run.ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// AcceptRun records that a human took this run as the period's close.
func (r *repo) AcceptRun(ctx context.Context, tenantID, runID, actor string) (*domain.ReconciliationRun, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	const accept = `UPDATE reconciliation_runs
		SET accepted_at=NOW(), accepted_by=$3
		WHERE tenant_id=$1 AND id=$2 AND accepted_at IS NULL
		RETURNING ` + runCols

	run, err := scanRun(tx.QueryRow(ctx, accept, tenantID, runID, actor))
	if isUniqueViolation(err, "uq_accepted_run_per_window") {
		return nil, ErrAlreadyAccepted
	}
	if err != nil {
		return nil, err
	}

	const closeWindow = `UPDATE balance_windows SET status='ACCEPTED', updated_at=NOW(), updated_by=$3
		WHERE tenant_id=$1 AND id=$2`
	if _, err := tx.Exec(ctx, closeWindow, tenantID, run.WindowID, actor); err != nil {
		return nil, fmt.Errorf("close window: %w", err)
	}

	if run.Flows, err = reconciledFlowsIn(ctx, tx, tenantID, runID); err != nil {
		return nil, err
	}

	// Somebody took this run as the period's close, which shuts the window and
	// fixes the imbalance it is closed at. The residual is recorded with it: an
	// entry saying only that a window was accepted answers nothing about what was
	// accepted, and the figure is the whole question a loss enquiry asks.
	after := map[string]any{
		"window_id":       run.WindowID,
		"converged":       run.Converged,
		"residual_before": run.ResidualBefore,
	}
	// Nil when no model answered, and left out rather than repeated from the
	// before value — which would read as a window that closed.
	if run.ResidualAfter != nil {
		after["residual_after"] = *run.ResidualAfter
	}
	if run.ModelVersion != "" {
		after["model_version"] = run.ModelVersion
	}
	if run.Reason != "" {
		after["reason"] = run.Reason
	}
	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "accept_reconciliation_run", ResourceType: "balance_window",
		ResourceID: run.WindowID, After: after, ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return run, nil
}

func (r *repo) reconciledFlows(ctx context.Context, tenantID, runID string) ([]domain.ReconciledFlow, error) {
	return reconciledFlowsIn(ctx, r.db, tenantID, runID)
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func reconciledFlowsIn(ctx context.Context, db querier, tenantID, runID string) ([]domain.ReconciledFlow, error) {
	const q = `SELECT ` + reconciledFlowCols + ` FROM reconciled_flows
		WHERE tenant_id=$1 AND run_id=$2 ORDER BY flow_id`

	rows, err := db.Query(ctx, q, tenantID, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ReconciledFlow
	for rows.Next() {
		var f domain.ReconciledFlow
		if err := rows.Scan(&f.RunID, &f.TenantID, &f.FlowID, &f.Measured, &f.Reconciled,
			&f.Adjustment, &f.TestStatistic, &f.GrossError, &f.Unmeasured); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanWindow(row scanner) (*domain.BalanceWindow, error) {
	var w domain.BalanceWindow
	var unit, status string
	if err := row.Scan(&w.ID, &w.TenantID, &w.RouteRef, &w.PeriodStart, &w.PeriodEnd,
		&unit, &status, &w.CreatedAt, &w.UpdatedAt, &w.CreatedBy, &w.UpdatedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	w.Unit = domain.Unit(unit)
	w.Status = domain.WindowStatus(status)
	return &w, nil
}

func scanFlow(row scanner) (*domain.FlowMeasurement, error) {
	var f domain.FlowMeasurement
	var fromKind, toKind string
	if err := row.Scan(&f.ID, &f.TenantID, &f.WindowID, &f.FlowID,
		&f.From.ID, &fromKind, &f.To.ID, &toKind,
		&f.Measured, &f.StandardUncertainty, &f.Unmeasured, &f.ObservationRef,
		&f.CreatedAt, &f.CreatedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	f.From.Kind = domain.NodeKind(fromKind)
	f.To.Kind = domain.NodeKind(toKind)
	return &f, nil
}

func scanRun(row scanner) (*domain.ReconciliationRun, error) {
	var run domain.ReconciliationRun
	if err := row.Scan(&run.ID, &run.TenantID, &run.WindowID, &run.Converged,
		&run.ResidualBefore, &run.ResidualAfter, &run.ModelVersion, &run.GrossErrorThreshold,
		&run.Reason, &run.AcceptedAt, &run.AcceptedBy, &run.CreatedAt, &run.CreatedBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &run, nil
}

// nullable keeps an absent decimal out of the column as NULL rather than as a
// zero, which for an uncertainty would mean infinitely trusted.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	// 23505 is unique_violation.
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}
