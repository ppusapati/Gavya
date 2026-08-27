// Package service records what a plant made from what, and answers the two
// questions a recall asks of that record.
package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/libs/integrity/quantity"

	"github.com/ppusapati/gavya/services/production-service/internal/domain"
	"github.com/ppusapati/gavya/services/production-service/internal/repository"
)

type IDs interface{ New() string }

type Clock interface{ Now() time.Time }

type Logger interface {
	Infof(string, ...any)
	Errorf(string, ...any)
}

type Service struct {
	repo  repository.Repository
	ids   IDs
	clock Clock
	log   Logger
}

func New(r repository.Repository, ids IDs, clock Clock, log Logger) *Service {
	return &Service{repo: r, ids: ids, clock: clock, log: log}
}

func (s *Service) CreateBatch(ctx context.Context, b *domain.Batch) (*domain.Batch, error) {
	return s.repo.CreateBatch(ctx, b)
}

func (s *Service) GetBatch(ctx context.Context, tenantID, id string) (*domain.Batch, error) {
	return s.repo.GetBatch(ctx, tenantID, id)
}

func (s *Service) GetBatchByCode(ctx context.Context, tenantID, code string) (*domain.Batch, error) {
	return s.repo.GetBatchByCode(ctx, tenantID, code)
}

func (s *Service) ListBatches(ctx context.Context, tenantID string, from, to time.Time, limit int) ([]*domain.Batch, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	if from.IsZero() {
		return nil, errors.New("a period has to be given: listing every batch a plant has ever " +
			"made is not a question anybody is asking")
	}
	if to.IsZero() {
		to = s.clock.Now()
	}
	if to.Before(from) {
		return nil, errors.New("the period ends before it starts")
	}
	return s.repo.ListBatches(ctx, tenantID, from, to, limit)
}

func (s *Service) SetStatus(ctx context.Context, tenantID, id string, status domain.Status, reason, actor string) (*domain.Batch, error) {
	return s.repo.SetStatus(ctx, tenantID, id, status, reason, actor)
}

// RecordInput says a lot went into a batch.
//
// The held check happens here as well as in the database, and the two are not
// redundant. The database is what makes it true — it holds under concurrency,
// under a direct connection, under a migration script. This one exists so the
// refusal names the batch and its reason before a transaction is opened, which
// is what somebody standing at a terminal needs to read.
func (s *Service) RecordInput(ctx context.Context, in *domain.Input, actor string) (*domain.Input, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}
	input, err := s.repo.GetBatch(ctx, in.TenantID, in.InputBatchID)
	if err != nil {
		return nil, fmt.Errorf("the batch being consumed: %w", err)
	}
	if input.Status.Held() {
		return nil, fmt.Errorf("%w: batch %s is %s — %s",
			domain.ErrHeldInput, input.Code, input.Status, input.StatusReason)
	}
	return s.repo.RecordInput(ctx, in, actor)
}

func (s *Service) Inputs(ctx context.Context, tenantID, batchID string) ([]domain.Input, error) {
	return s.repo.Inputs(ctx, tenantID, batchID)
}

func (s *Service) Remaining(ctx context.Context, tenantID, batchID string) (quantity.Quantity, error) {
	return s.repo.Remaining(ctx, tenantID, batchID)
}

// ---------------------------------------------------------------------------
// The recall
// ---------------------------------------------------------------------------

// Affected is one batch a recall reached, with enough about it to act on.
type Affected struct {
	Batch *domain.Batch
	Depth int
	Via   string
}

// Recall is the answer to "this batch is bad, what else is".
type Recall struct {
	Origin *domain.Batch
	// Direction says which question was asked. Forward from a contaminated lot
	// finds what it became; backward from a returned carton finds what it was
	// made from and, through those, what else came out of the same silo.
	Direction domain.Direction

	Affected []Affected
	// Finished is the subset that left the plant, in the order a distributor
	// would work down. This is what the recall is for; the rest is the
	// explanation of how they got there.
	Finished []Affected

	// Complete is false when the walk stopped at a bound. A recall report that
	// stopped early and did not say so is acted on as though it were whole.
	Complete bool
	// Frontier is where somebody continuing by hand starts.
	Frontier []string
	// StoppedBecause is in words, for the report.
	StoppedBecause string

	// Unreadable names batches the walk reached and could not load. They are
	// listed rather than dropped: a batch missing from the report because a row
	// would not scan is indistinguishable from one that was never affected.
	Unreadable []string
}

// DefaultLimit is what a caller that has no view of its own gets.
//
// It is generous rather than typical, because the failure it guards against is
// a runaway query and the failure it must not cause is a truncated recall. A
// plant that genuinely exceeds it finds out from Complete, which is the whole
// point of that field.
var DefaultLimit = domain.Limit{MaxDepth: 50, MaxBatches: 20000}

// Trace answers a recall in one direction.
func (s *Service) Trace(ctx context.Context, tenantID, batchID string, dir domain.Direction, lim domain.Limit) (*Recall, error) {
	origin, err := s.repo.GetBatch(ctx, tenantID, batchID)
	if err != nil {
		return nil, err
	}
	if lim == (domain.Limit{}) {
		lim = DefaultLimit
	}

	next := func(ids []string) ([]domain.Edge, error) {
		return s.repo.Edges(ctx, tenantID, ids)
	}
	trace, err := domain.Walk(origin.ID, dir, next, lim)
	if err != nil {
		return nil, err
	}

	out := &Recall{
		Origin: origin, Direction: dir,
		Complete: trace.Complete, Frontier: trace.Frontier,
		StoppedBecause: trace.StoppedBecause,
	}
	for _, r := range trace.Reached {
		b, err := s.repo.GetBatch(ctx, tenantID, r.BatchID)
		if err != nil {
			// One row that will not load must not silently shrink the list.
			s.log.Errorf("recall from %s reached batch %s and could not load it: %v",
				origin.Code, r.BatchID, err)
			out.Unreadable = append(out.Unreadable, r.BatchID)
			out.Complete = false
			if out.StoppedBecause == "" {
				out.StoppedBecause = "one or more batches the walk reached could not be read"
			}
			continue
		}
		a := Affected{Batch: b, Depth: r.Depth, Via: r.Via}
		out.Affected = append(out.Affected, a)
		if b.Kind == domain.Finished {
			out.Finished = append(out.Finished, a)
		}
	}
	sort.SliceStable(out.Finished, func(i, j int) bool {
		return out.Finished[i].Batch.Code < out.Finished[j].Batch.Code
	})
	return out, nil
}

// Yield is what a batch gave against what went into it.
//
// The density is the caller's, always. Where a batch is weighed and its inputs
// were measured by volume there is no yield without one, and the report says so
// rather than quietly applying the 1.03 that everybody quotes and nobody
// measured — a three per cent error that would read as a process problem.
func (s *Service) Yield(ctx context.Context, tenantID, batchID string, d *quantity.Density, mode money.RoundingMode) (*domain.Yield, error) {
	// The rounding mode is required exactly when it is used — that is, when a
	// density has been supplied and something will be converted. Demanding it
	// on a yield that converts nothing would be ceremony; letting it default on
	// one that does would be the platform quietly choosing which way the
	// variance falls.
	if d != nil && mode == "" {
		return nil, errors.New("converting the inputs needs a rounding mode as well as a " +
			"density; the choice shows up in the variance and nobody would know it was made here")
	}
	b, err := s.repo.GetBatch(ctx, tenantID, batchID)
	if err != nil {
		return nil, err
	}
	inputs, err := s.repo.Inputs(ctx, tenantID, batchID)
	if err != nil {
		return nil, err
	}
	if d != nil {
		if err := d.Validate(); err != nil {
			return nil, err
		}
	}
	return domain.ComputeYield(b, inputs, d, mode)
}
