package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/mlclient"
	ulidpkg "p9e.in/samavaya/packages/ulid"

	"github.com/ppusapati/gavya/services/balance-service/internal/domain"
	"github.com/ppusapati/gavya/services/balance-service/internal/repository"
)

var (
	// ErrWindowClosed means the window has an accepted run and no longer admits
	// flows or further reconciliation.
	ErrWindowClosed = errors.New("the window is closed by an accepted run")
	// ErrRunDidNotConverge means a run cannot be accepted as a period's close.
	ErrRunDidNotConverge = errors.New("a run that did not converge cannot close a period")
)

// mlCallTimeout bounds the reconciler call independently of the caller's
// deadline. An advisory answer is not worth holding an investigation open for.
const mlCallTimeout = 5 * time.Second

type CreateWindowInput struct {
	TenantID    string
	RouteRef    string
	PeriodStart time.Time
	PeriodEnd   time.Time
	Unit        domain.Unit
	Actor       string
}

func (s *Service) CreateWindow(ctx context.Context, in CreateWindowInput) (*domain.BalanceWindow, error) {
	switch {
	case in.TenantID == "":
		return nil, errors.New("tenant_id is required")
	case in.RouteRef == "":
		return nil, errors.New("route_ref is required")
	case in.Actor == "":
		return nil, errors.New("actor is required")
	case in.PeriodStart.IsZero() || in.PeriodEnd.IsZero():
		return nil, errors.New("period_start and period_end are required")
	case !in.PeriodEnd.After(in.PeriodStart):
		return nil, errors.New("period_end must be after period_start")
	}
	switch in.Unit {
	case domain.UnitLitres, domain.UnitKG:
	default:
		return nil, fmt.Errorf("unit %q is not recognised", in.Unit)
	}

	return s.repo.CreateWindow(ctx, &domain.BalanceWindow{
		ID:          ulidpkg.New().String(),
		TenantID:    in.TenantID,
		RouteRef:    in.RouteRef,
		PeriodStart: in.PeriodStart,
		PeriodEnd:   in.PeriodEnd,
		Unit:        in.Unit,
		CreatedBy:   in.Actor,
	})
}

func (s *Service) GetWindow(ctx context.Context, tenantID, id string) (*domain.BalanceWindow, error) {
	return s.repo.GetWindow(ctx, tenantID, id)
}

func (s *Service) ListWindows(ctx context.Context, tenantID string, status domain.WindowStatus, limit, offset int) ([]*domain.BalanceWindow, error) {
	return s.repo.ListWindows(ctx, tenantID, status, clampLimit(limit), clampOffset(offset))
}

type AddFlowInput struct {
	TenantID            string
	WindowID            string
	FlowID              string
	From                domain.BalanceNode
	To                  domain.BalanceNode
	Measured            string
	StandardUncertainty string
	Unmeasured          bool
	ObservationRef      string
	Actor               string
}

func (s *Service) AddFlow(ctx context.Context, in AddFlowInput) (*domain.FlowMeasurement, error) {
	switch {
	case in.TenantID == "":
		return nil, errors.New("tenant_id is required")
	case in.WindowID == "":
		return nil, errors.New("window_id is required")
	case in.Actor == "":
		return nil, errors.New("actor is required")
	}

	window, err := s.repo.GetWindow(ctx, in.TenantID, in.WindowID)
	if err != nil {
		return nil, err
	}
	if window.Status == domain.WindowAccepted {
		return nil, ErrWindowClosed
	}

	flow := &domain.FlowMeasurement{
		ID:                  ulidpkg.New().String(),
		TenantID:            in.TenantID,
		WindowID:            in.WindowID,
		FlowID:              in.FlowID,
		From:                in.From,
		To:                  in.To,
		Measured:            in.Measured,
		StandardUncertainty: in.StandardUncertainty,
		Unmeasured:          in.Unmeasured,
		ObservationRef:      in.ObservationRef,
		CreatedBy:           in.Actor,
	}
	if flow.Unmeasured {
		// An unmeasured leg states no quantity; the column still needs a value
		// and zero is the only one that leaves the node balances untouched.
		flow.Measured = "0.000"
		flow.StandardUncertainty = ""
	}
	if err := domain.ValidateFlows([]domain.FlowMeasurement{*flow}); err != nil {
		return nil, err
	}
	return s.repo.AddFlow(ctx, flow)
}

func (s *Service) ListFlows(ctx context.Context, tenantID, windowID string) ([]domain.FlowMeasurement, error) {
	return s.repo.ListFlows(ctx, tenantID, windowID)
}

type ReconcileInput struct {
	TenantID string
	WindowID string
	// GrossErrorThreshold is the test statistic above which a flow is reported
	// as carrying a gross error. Zero leaves it to the reconciler's default.
	GrossErrorThreshold float64
	RequestID           string
	Actor               string
}

// Reconcile closes a window: it validates the network, computes the imbalance
// itself, asks the reconciler for the adjustments, and records the run.
//
// The run is persisted whatever the reconciler does. A window that does not
// close is a finding in its own right, and the locally computed residual is the
// part of that finding no model is needed for.
func (s *Service) Reconcile(ctx context.Context, in ReconcileInput) (*domain.ReconciliationRun, error) {
	if in.Actor == "" {
		return nil, errors.New("actor is required")
	}

	window, err := s.repo.GetWindow(ctx, in.TenantID, in.WindowID)
	if err != nil {
		return nil, err
	}
	if window.Status == domain.WindowAccepted {
		return nil, ErrWindowClosed
	}

	flows, err := s.repo.ListFlows(ctx, in.TenantID, in.WindowID)
	if err != nil {
		return nil, err
	}
	if err := domain.ValidateFlows(flows); err != nil {
		return nil, err
	}

	residuals, err := domain.ResidualByNode(flows)
	if err != nil {
		return nil, err
	}
	before, err := domain.TotalResidual(residuals)
	if err != nil {
		return nil, err
	}

	run := &domain.ReconciliationRun{
		ID:                  ulidpkg.New().String(),
		TenantID:            in.TenantID,
		WindowID:            in.WindowID,
		Converged:           false,
		ResidualBefore:      before.String(),
		GrossErrorThreshold: in.GrossErrorThreshold,
		CreatedBy:           in.Actor,
	}

	resp, reason := s.askReconciler(ctx, flows, in)
	if resp != nil {
		if err := adoptModelResult(run, flows, resp); err != nil {
			s.log.Warnf("window %s: discarding the reconciler's answer: %v", in.WindowID, err)
			resp, reason = nil, fmt.Sprintf("the reconciler's answer did not describe this window: %v", err)
		}
	}
	if resp == nil {
		run.Reason = reason
		s.log.Warnf("window %s: reconciled locally only, imbalance %s %s: %s",
			in.WindowID, run.ResidualBefore, window.Unit, reason)
	}

	return s.repo.CreateRun(ctx, run)
}

// askReconciler consults the Rust reconciler. A failure is logged and swallowed:
// the caller gets a run carrying the locally computed imbalance instead of an
// error, because a settlement investigation must never be blocked by a model
// being down.
func (s *Service) askReconciler(ctx context.Context, flows []domain.FlowMeasurement, in ReconcileInput) (*mlclient.ReconcileMassBalanceResponse, string) {
	if s.ml == nil {
		return nil, "no reconciler is configured; the imbalance was computed locally"
	}

	wire, err := toWireFlows(flows)
	if err != nil {
		return nil, fmt.Sprintf("the window could not be expressed for the reconciler: %v", err)
	}

	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), mlCallTimeout)
	defer cancel()

	resp, err := s.ml.Reconcile(callCtx, &mlclient.ReconcileMassBalanceRequest{
		TenantID:            in.TenantID,
		BalanceWindow:       in.WindowID,
		Flows:               wire,
		GrossErrorThreshold: in.GrossErrorThreshold,
	}, mlclient.CallOptions{TenantID: in.TenantID, RequestID: in.RequestID})
	if err != nil {
		return nil, fmt.Sprintf("the reconciler did not answer: %v", err)
	}
	return resp, ""
}

// toWireFlows converts the stored decimals to the float64 the least-squares
// solve works in. The conversion happens here and nowhere else: what is stored,
// compared and settled against stays exact.
func toWireFlows(flows []domain.FlowMeasurement) ([]mlclient.FlowMeasurement, error) {
	out := make([]mlclient.FlowMeasurement, 0, len(flows))
	for _, f := range flows {
		measured, err := strconv.ParseFloat(f.Measured, 64)
		if err != nil {
			return nil, fmt.Errorf("flow %q measured %q: %w", f.FlowID, f.Measured, err)
		}
		var sigma float64
		if !f.Unmeasured {
			if sigma, err = strconv.ParseFloat(f.StandardUncertainty, 64); err != nil {
				return nil, fmt.Errorf("flow %q standard uncertainty %q: %w", f.FlowID, f.StandardUncertainty, err)
			}
		}
		out = append(out, mlclient.FlowMeasurement{
			FlowID:              f.FlowID,
			FromNode:            f.From.ID,
			ToNode:              f.To.ID,
			Measured:            measured,
			StandardUncertainty: sigma,
			Unmeasured:          f.Unmeasured,
		})
	}
	return out, nil
}

func adoptModelResult(run *domain.ReconciliationRun, flows []domain.FlowMeasurement, resp *mlclient.ReconcileMassBalanceResponse) error {
	known := make(map[string]bool, len(flows))
	for _, f := range flows {
		known[f.FlowID] = true
	}
	if len(resp.Flows) != len(flows) {
		return fmt.Errorf("the answer covers %d flows, the window has %d", len(resp.Flows), len(flows))
	}

	adopted := make([]domain.ReconciledFlow, 0, len(resp.Flows))
	for _, rf := range resp.Flows {
		if !known[rf.FlowID] {
			return fmt.Errorf("the answer names flow %q, which the window does not carry", rf.FlowID)
		}
		adopted = append(adopted, domain.ReconciledFlow{
			RunID:      run.ID,
			TenantID:   run.TenantID,
			FlowID:     rf.FlowID,
			Measured:   formatQuantity(rf.Measured),
			Reconciled: formatQuantity(rf.Reconciled),
			Adjustment: formatQuantity(rf.Adjustment),
			// An answer that did not converge has adjustments that close
			// nothing, so it is in no position to accuse a leg.
			TestStatistic: rf.TestStatistic,
			GrossError:    resp.Converged && rf.GrossError,
			Unmeasured:    rf.Unmeasured,
		})
	}

	after := formatQuantity(resp.ResidualAfter)
	run.Converged = resp.Converged
	run.ResidualAfter = &after
	run.ModelVersion = resp.ModelVersion
	run.Flows = adopted
	if !resp.Converged {
		run.Reason = "the network is under-determined; the adjustments are indicative only"
	}
	return nil
}

// formatQuantity narrows a solved float64 back to the three decimals the ledger
// keeps.
func formatQuantity(v float64) string { return strconv.FormatFloat(v, 'f', domain.QuantityScale, 64) }

func (s *Service) GetRun(ctx context.Context, tenantID, id string) (*domain.ReconciliationRun, error) {
	return s.repo.GetRun(ctx, tenantID, id)
}

func (s *Service) ListRuns(ctx context.Context, tenantID, windowID string, limit, offset int) ([]*domain.ReconciliationRun, error) {
	return s.repo.ListRuns(ctx, tenantID, windowID, clampLimit(limit), clampOffset(offset))
}

// AcceptRun records that a human took a run as the period's close.
func (s *Service) AcceptRun(ctx context.Context, tenantID, runID, actor string) (*domain.ReconciliationRun, error) {
	if actor == "" {
		return nil, errors.New("actor is required")
	}

	run, err := s.repo.GetRun(ctx, tenantID, runID)
	if err != nil {
		return nil, err
	}
	if run.IsAccepted() {
		return nil, repository.ErrAlreadyAccepted
	}
	// A period cannot be closed on an arithmetic that never closed. The run
	// stays on record as the evidence of that; accepting it would state the
	// opposite.
	if !run.Converged {
		return nil, fmt.Errorf("%w: run %s", ErrRunDidNotConverge, runID)
	}
	return s.repo.AcceptRun(ctx, tenantID, runID, actor)
}

func clampLimit(limit int) int {
	if limit <= 0 || limit > 500 {
		return 100
	}
	return limit
}

func clampOffset(offset int) int {
	if offset < 0 {
		return 0
	}
	return offset
}
