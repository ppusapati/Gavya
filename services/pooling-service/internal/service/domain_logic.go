package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/libs/integrity/origin"
	ulidpkg "p9e.in/samavaya/packages/ULID"

	"github.com/ppusapati/gavya/services/pooling-service/internal/domain"
	"github.com/ppusapati/gavya/services/pooling-service/internal/repository"
)

// ErrEscalated means a correction could not be decided from the tenant's
// retroactivity policy and is waiting on a person.
//
// It is an error rather than a quiet default because every default available
// here moves real money: applying the correction pays producers an amount
// nobody authorised, and dropping it silently keeps money the producers are
// owed. Refusing is the only outcome that cannot be wrong.
var ErrEscalated = errors.New("the correction must be decided by a person")

type CreatePoolInput struct {
	TenantID      string
	Name          string
	PeriodStart   time.Time
	PeriodEnd     time.Time
	Unit          string
	Currency      string
	Scale         int32
	RateCardID    string
	PolicyVersion string
	Actor         string
}

func (s *Service) CreatePool(ctx context.Context, in CreatePoolInput) (*domain.Pool, error) {
	switch {
	case in.TenantID == "":
		return nil, errors.New("tenant_id is required")
	case in.Name == "":
		return nil, errors.New("name is required")
	case in.Currency == "":
		return nil, errors.New("currency is required")
	case in.Actor == "":
		return nil, errors.New("actor is required")
	case in.PeriodStart.IsZero() || in.PeriodEnd.IsZero():
		return nil, errors.New("period_start and period_end are required")
	case !in.PeriodEnd.After(in.PeriodStart):
		return nil, errors.New("period_end must fall after period_start")
	case in.Scale < 0 || in.Scale > 9:
		return nil, fmt.Errorf("amount scale %d is outside 0..9", in.Scale)
	}

	unit := in.Unit
	if unit == "" {
		unit = "LITRE"
	}

	return s.repo.CreatePool(ctx, &domain.Pool{
		ID:            ulidpkg.New().String(),
		TenantID:      in.TenantID,
		Name:          in.Name,
		PeriodStart:   in.PeriodStart,
		PeriodEnd:     in.PeriodEnd,
		Unit:          unit,
		Currency:      in.Currency,
		Scale:         in.Scale,
		Status:        domain.PoolOpen,
		RateCardID:    in.RateCardID,
		PolicyVersion: in.PolicyVersion,
		CreatedBy:     in.Actor,
	})
}

func (s *Service) GetPool(ctx context.Context, tenantID, id string) (*domain.Pool, error) {
	return s.repo.GetPool(ctx, tenantID, id)
}

func (s *Service) ListPools(ctx context.Context, tenantID string, from, to time.Time, limit, offset int) ([]*domain.Pool, error) {
	return s.repo.ListPools(ctx, tenantID, from, to, clampLimit(limit), clampOffset(offset))
}

type AddProducerMilkInput struct {
	TenantID    string
	PoolID      string
	ProducerRef string
	Quantity    string
	Components  map[domain.ComponentKind]string
	SlotRefs    []string
	Origin      origin.Origin
	Actor       string
}

// AddProducerMilk records what one producer delivered into an open pool.
//
// Quantities are validated as decimal literals here rather than at valuation
// time: a malformed quantity discovered while valuing would fail the whole
// pool, in the period-end run, with no obvious owner.
func (s *Service) AddProducerMilk(ctx context.Context, in AddProducerMilkInput) (*domain.ProducerMilk, error) {
	switch {
	case in.TenantID == "":
		return nil, errors.New("tenant_id is required")
	case in.PoolID == "":
		return nil, errors.New("pool_id is required")
	case in.ProducerRef == "":
		return nil, errors.New("producer_ref is required")
	case in.Actor == "":
		return nil, errors.New("actor is required")
	}

	pool, err := s.repo.GetPool(ctx, in.TenantID, in.PoolID)
	if err != nil {
		return nil, err
	}
	if pool.Status != domain.PoolOpen && pool.Status != domain.PoolReopened {
		return nil, fmt.Errorf("pool %s is %s and no longer accepts milk", pool.ID, pool.Status)
	}

	if err := validateQuantity(in.Quantity, "quantity"); err != nil {
		return nil, err
	}
	for kind, q := range in.Components {
		if err := validateQuantity(q, "component "+string(kind)); err != nil {
			return nil, err
		}
	}

	o := in.Origin
	if o.Kind == "" {
		o = origin.NewNative()
	}
	if err := o.Validate(); err != nil {
		return nil, err
	}

	return s.repo.AddProducerMilk(ctx, &domain.ProducerMilk{
		ID:          ulidpkg.New().String(),
		TenantID:    in.TenantID,
		PoolID:      in.PoolID,
		ProducerRef: in.ProducerRef,
		Quantity:    in.Quantity,
		Components:  in.Components,
		SlotRefs:    in.SlotRefs,
		Origin:      o,
		CreatedBy:   in.Actor,
	})
}

func (s *Service) ListProducerMilk(ctx context.Context, tenantID, poolID string) ([]domain.ProducerMilk, error) {
	return s.repo.ListProducerMilk(ctx, tenantID, poolID)
}

type RecordUtilisationInput struct {
	TenantID string
	PoolID   string
	Class    domain.UtilisationClass
	Quantity string
	Price    money.Rate
	Actor    string
}

func (s *Service) RecordUtilisation(ctx context.Context, in RecordUtilisationInput) (*domain.ClassifiedUtilisation, error) {
	switch {
	case in.TenantID == "":
		return nil, errors.New("tenant_id is required")
	case in.PoolID == "":
		return nil, errors.New("pool_id is required")
	case in.Actor == "":
		return nil, errors.New("actor is required")
	}
	if !domain.ValidClass(in.Class) {
		return nil, fmt.Errorf("utilisation class %q is not recognised", in.Class)
	}
	if err := validateQuantity(in.Quantity, "quantity"); err != nil {
		return nil, err
	}

	if _, err := s.repo.GetPool(ctx, in.TenantID, in.PoolID); err != nil {
		return nil, err
	}

	return s.repo.AddUtilisation(ctx, &domain.ClassifiedUtilisation{
		ID:        ulidpkg.New().String(),
		TenantID:  in.TenantID,
		PoolID:    in.PoolID,
		Class:     in.Class,
		Quantity:  in.Quantity,
		Price:     in.Price,
		CreatedBy: in.Actor,
	})
}

func (s *Service) ListUtilisations(ctx context.Context, tenantID, poolID string) ([]domain.ClassifiedUtilisation, error) {
	return s.repo.ListUtilisations(ctx, tenantID, poolID)
}

// ValuationOutcome is a pool's stored value and every producer's share of it.
type ValuationOutcome struct {
	Pool        *domain.Pool
	Valuation   *domain.PoolValuation
	Allocations []domain.Allocation
}

type ValuePoolInput struct {
	TenantID string
	PoolID   string
	// ComponentPrices is the uniform rate card the pool is valued against.
	ComponentPrices []domain.ComponentPrice
	Actor           string
}

// ValuePool computes a pool's value, shares it out, and marks the pool valued.
func (s *Service) ValuePool(ctx context.Context, in ValuePoolInput) (*ValuationOutcome, error) {
	switch {
	case in.TenantID == "":
		return nil, errors.New("tenant_id is required")
	case in.PoolID == "":
		return nil, errors.New("pool_id is required")
	case in.Actor == "":
		return nil, errors.New("actor is required")
	}

	pool, err := s.repo.GetPool(ctx, in.TenantID, in.PoolID)
	if err != nil {
		return nil, err
	}
	// A settled pool has paid its producers. Re-valuing it in place would leave
	// the amounts on record disagreeing with the amounts that were paid, with no
	// row saying a correction happened; that path is ApplyCorrection's.
	if pool.Status == domain.PoolSettled {
		return nil, fmt.Errorf("pool %s is settled; a correction to it must go through the retroactivity policy", pool.ID)
	}

	result, err := s.compute(ctx, pool, in.ComponentPrices)
	if err != nil {
		return nil, err
	}

	valuation, allocations, err := s.persistValuation(ctx, pool, result, in.Actor)
	if err != nil {
		return nil, err
	}

	updated, err := s.repo.SetPoolStatus(ctx, pool.TenantID, pool.ID, domain.PoolValued, in.Actor)
	if err != nil {
		return nil, fmt.Errorf("mark pool valued: %w", err)
	}
	return &ValuationOutcome{Pool: updated, Valuation: valuation, Allocations: allocations}, nil
}

func (s *Service) GetValuation(ctx context.Context, tenantID, poolID string) (*domain.PoolValuation, error) {
	return s.repo.GetValuation(ctx, tenantID, poolID)
}

func (s *Service) ListAllocations(ctx context.Context, tenantID, poolID, valuationID string) ([]domain.Allocation, error) {
	return s.repo.ListAllocations(ctx, tenantID, poolID, valuationID)
}

// SettlementOutcome is what left the pool for its producers.
type SettlementOutcome struct {
	Pool   *domain.Pool
	Events []domain.ProducerEconomicEvent
}

// SettlePool raises one original payable per producer from the pool's live
// allocations and marks the pool settled.
func (s *Service) SettlePool(ctx context.Context, tenantID, poolID, actor string) (*SettlementOutcome, error) {
	if actor == "" {
		return nil, errors.New("actor is required")
	}

	pool, err := s.repo.GetPool(ctx, tenantID, poolID)
	if err != nil {
		return nil, err
	}
	// Settling twice would raise a second original payable for every producer,
	// and nothing in an ORIGINAL event says it corrects an earlier one.
	if pool.Status != domain.PoolValued {
		return nil, fmt.Errorf("pool %s is %s; only a valued pool can be settled", pool.ID, pool.Status)
	}

	valuation, err := s.repo.GetValuation(ctx, tenantID, poolID)
	if err != nil {
		return nil, fmt.Errorf("load the pool's valuation: %w", err)
	}
	allocations, err := s.repo.ListAllocations(ctx, tenantID, poolID, valuation.ID)
	if err != nil {
		return nil, err
	}
	if len(allocations) == 0 {
		return nil, fmt.Errorf("valuation %s has no allocations to settle", valuation.ID)
	}

	derivationID := ulidpkg.New().String()
	events := make([]domain.ProducerEconomicEvent, 0, len(allocations))
	for _, a := range allocations {
		events = append(events, domain.ProducerEconomicEvent{
			ID:           ulidpkg.New().String(),
			TenantID:     tenantID,
			PoolID:       poolID,
			AllocationID: a.ID,
			ProducerRef:  a.ProducerRef,
			Amount:       a.Total,
			Kind:         domain.EventOriginal,
			Origin:       origin.Origin{Kind: origin.Derived, DerivationID: derivationID},
			CreatedBy:    actor,
		})
	}

	stored, err := s.repo.CreateEconomicEvents(ctx, events)
	if err != nil {
		return nil, err
	}

	updated, err := s.repo.SetPoolStatus(ctx, tenantID, poolID, domain.PoolSettled, actor)
	if err != nil {
		return nil, fmt.Errorf("mark pool settled: %w", err)
	}
	return &SettlementOutcome{Pool: updated, Events: stored}, nil
}

func (s *Service) ListEconomicEvents(ctx context.Context, tenantID, poolID, producerRef string, limit, offset int) ([]domain.ProducerEconomicEvent, error) {
	return s.repo.ListEconomicEvents(ctx, tenantID, poolID, producerRef, clampLimit(limit), clampOffset(offset))
}

// CorrectionOutcome reports what a late correction was permitted to do.
type CorrectionOutcome struct {
	Decision domain.RetroactivityDecision
	// Adjustment is the change in the pool's value the correction produces.
	Adjustment  money.Money
	Pool        *domain.Pool
	Valuation   *domain.PoolValuation
	Allocations []domain.Allocation
	Events      []domain.ProducerEconomicEvent
}

type ApplyCorrectionInput struct {
	TenantID        string
	PoolID          string
	ComponentPrices []domain.ComponentPrice
	// At is the instant the correction is being applied, which selects the
	// policy in force and measures the pool's age against its lookback window.
	At    time.Time
	Actor string
}

// ApplyCorrection re-values a pool against its corrected inputs and does with
// the difference whatever the tenant's retroactivity policy permits.
func (s *Service) ApplyCorrection(ctx context.Context, in ApplyCorrectionInput) (*CorrectionOutcome, error) {
	switch {
	case in.TenantID == "":
		return nil, errors.New("tenant_id is required")
	case in.PoolID == "":
		return nil, errors.New("pool_id is required")
	case in.Actor == "":
		return nil, errors.New("actor is required")
	}
	at := in.At
	if at.IsZero() {
		at = time.Now().UTC()
	}

	pool, err := s.repo.GetPool(ctx, in.TenantID, in.PoolID)
	if err != nil {
		return nil, err
	}

	prior, err := s.repo.GetValuation(ctx, in.TenantID, in.PoolID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("pool %s has never been valued, so there is nothing to correct", in.PoolID)
		}
		return nil, err
	}
	priorAllocations, err := s.repo.ListAllocations(ctx, in.TenantID, in.PoolID, prior.ID)
	if err != nil {
		return nil, err
	}

	policy, err := s.repo.GetEffectiveRetroactivityPolicy(ctx, in.TenantID, at)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, domain.ErrNoRetroactivityPolicy
		}
		return nil, err
	}

	recomputed, err := s.compute(ctx, pool, in.ComponentPrices)
	if err != nil {
		return nil, err
	}

	adjustment, err := money.Sub(recomputed.ClassifiedValue, prior.ClassifiedValue)
	if err != nil {
		return nil, fmt.Errorf("measure the correction against the stored valuation: %w", err)
	}

	decision, err := domain.DecideRetroactivity(policy, pool, adjustment, at)
	if err != nil {
		return nil, err
	}

	outcome := &CorrectionOutcome{Decision: decision, Adjustment: adjustment, Pool: pool}

	switch decision.Outcome {
	case domain.RetroSuppress, domain.RetroApplyForward:
		return outcome, nil

	case domain.RetroEscalate:
		return nil, fmt.Errorf("%w: pool %s, adjustment %s: %s", ErrEscalated, pool.ID, adjustment, decision.Reason)

	case domain.RetroAdjust, domain.RetroRestate:
		valuation, allocations, err := s.persistValuation(ctx, pool, recomputed, in.Actor)
		if err != nil {
			return nil, err
		}
		outcome.Valuation, outcome.Allocations = valuation, allocations

		// A pool that had settled nothing raises nothing: the decision's kind is
		// ORIGINAL there, and the payables it would stand for are SettlePool's to
		// raise once the corrected valuation is the one being settled.
		if decision.EventKind != domain.EventOriginal {
			events, err := s.raiseCorrections(ctx, pool, decision.EventKind, priorAllocations, allocations, in.Actor)
			if err != nil {
				return nil, err
			}
			outcome.Events = events
		}

		status := domain.PoolValued
		if pool.Status == domain.PoolSettled || pool.Status == domain.PoolReopened {
			status = domain.PoolReopened
		}
		updated, err := s.repo.SetPoolStatus(ctx, pool.TenantID, pool.ID, status, in.Actor)
		if err != nil {
			return nil, fmt.Errorf("move pool to %s: %w", status, err)
		}
		outcome.Pool = updated
		return outcome, nil

	default:
		return nil, fmt.Errorf("retroactivity outcome %q is not recognised", decision.Outcome)
	}
}

type DeclareRetroactivityPolicyInput struct {
	TenantID          string
	Name              string
	Mode              domain.RetroactivityMode
	MaxLookbackDays   int32
	MinimumAdjustment money.Money
	EffectiveFrom     time.Time
	EffectiveTo       *time.Time
	Actor             string
}

func (s *Service) DeclareRetroactivityPolicy(ctx context.Context, in DeclareRetroactivityPolicyInput) (*domain.RecoveryRetroactivityPolicy, error) {
	switch {
	case in.TenantID == "":
		return nil, errors.New("tenant_id is required")
	case in.Name == "":
		return nil, errors.New("name is required")
	case in.Actor == "":
		return nil, errors.New("actor is required")
	case in.EffectiveFrom.IsZero():
		return nil, errors.New("effective_from is required")
	case in.MaxLookbackDays < 0:
		return nil, errors.New("max_lookback_days must not be negative")
	}
	switch in.Mode {
	case domain.RetroDoNotReopen, domain.RetroRecalculate, domain.RetroApplyIncremental, domain.RetroCustom:
	default:
		return nil, fmt.Errorf("retroactivity mode %q is not recognised", in.Mode)
	}
	if in.EffectiveTo != nil && !in.EffectiveTo.After(in.EffectiveFrom) {
		return nil, errors.New("effective_to must fall after effective_from")
	}

	return s.repo.CreateRetroactivityPolicy(ctx, &domain.RecoveryRetroactivityPolicy{
		ID:                ulidpkg.New().String(),
		TenantID:          in.TenantID,
		Name:              in.Name,
		Mode:              in.Mode,
		MaxLookbackDays:   in.MaxLookbackDays,
		MinimumAdjustment: in.MinimumAdjustment,
		EffectiveFrom:     in.EffectiveFrom,
		EffectiveTo:       in.EffectiveTo,
		CreatedBy:         in.Actor,
	})
}

func (s *Service) GetEffectiveRetroactivityPolicy(ctx context.Context, tenantID string, at time.Time) (*domain.RecoveryRetroactivityPolicy, error) {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return s.repo.GetEffectiveRetroactivityPolicy(ctx, tenantID, at)
}

// compute gathers a pool's inputs and values it.
func (s *Service) compute(ctx context.Context, pool *domain.Pool, prices []domain.ComponentPrice) (*domain.ValuationResult, error) {
	if len(prices) == 0 {
		return nil, errors.New("no component prices supplied; a pool cannot be valued without a rate card")
	}

	producers, err := s.repo.ListProducerMilk(ctx, pool.TenantID, pool.ID)
	if err != nil {
		return nil, fmt.Errorf("load producer milk: %w", err)
	}
	utilisations, err := s.repo.ListUtilisations(ctx, pool.TenantID, pool.ID)
	if err != nil {
		return nil, fmt.Errorf("load utilisations: %w", err)
	}

	return domain.ComputeValuation(domain.ValuationInput{
		Currency:        pool.Currency,
		Scale:           pool.Scale,
		Producers:       producers,
		Utilisations:    utilisations,
		ComponentPrices: prices,
	})
}

// persistValuation stamps identifiers onto a computed result and stores it with
// its allocations.
func (s *Service) persistValuation(ctx context.Context, pool *domain.Pool, result *domain.ValuationResult, actor string) (*domain.PoolValuation, []domain.Allocation, error) {
	valuationID := ulidpkg.New().String()
	now := time.Now().UTC()

	valuation := &domain.PoolValuation{
		ID:                     valuationID,
		TenantID:               pool.TenantID,
		PoolID:                 pool.ID,
		ClassifiedValue:        result.ClassifiedValue,
		ComponentValue:         result.ComponentValue,
		ProducerSettlementFund: result.ProducerSettlementFund,
		TotalQuantity:          result.TotalQuantity,
		BlendPrice:             result.BlendPrice,
		RoundingTrail:          result.RoundingTrail,
		Origin:                 origin.Origin{Kind: origin.Derived, DerivationID: valuationID},
		ComputedAt:             now,
		CreatedBy:              actor,
	}

	allocations := make([]domain.Allocation, 0, len(result.Allocations))
	for _, a := range result.Allocations {
		a.ID = ulidpkg.New().String()
		a.TenantID = pool.TenantID
		a.PoolID = pool.ID
		a.ValuationID = valuationID
		a.CreatedBy = actor
		allocations = append(allocations, a)
	}

	return s.repo.SaveValuation(ctx, valuation, allocations)
}

// raiseCorrections turns the difference between two valuations into producer
// economic events of the kind the policy chose.
func (s *Service) raiseCorrections(
	ctx context.Context,
	pool *domain.Pool,
	kind domain.EventKind,
	prior []domain.Allocation,
	current []domain.Allocation,
	actor string,
) ([]domain.ProducerEconomicEvent, error) {
	priorTotals := make(map[string]money.Money, len(prior))
	for _, a := range prior {
		priorTotals[a.ProducerRef] = a.Total
	}

	settled, err := s.latestEventPerProducer(ctx, pool.TenantID, pool.ID)
	if err != nil {
		return nil, err
	}

	derivationID := ulidpkg.New().String()
	events := make([]domain.ProducerEconomicEvent, 0, len(current))

	for _, a := range current {
		amount := a.Total
		if kind == domain.EventIncremental {
			previous, ok := priorTotals[a.ProducerRef]
			if ok {
				if amount, err = money.Sub(a.Total, previous); err != nil {
					return nil, fmt.Errorf("difference for producer %s: %w", a.ProducerRef, err)
				}
			}
			// An adjustment of nothing is not a payable; raising one would put a
			// zero-value correction on the producer's statement for no reason.
			if amount.IsZero() {
				continue
			}
		}

		eventKind, supersedes := kind, settled[a.ProducerRef]
		// A producer with no settled event was never paid from this pool, so
		// their amount is a first payable rather than a correction of one.
		if supersedes == "" {
			eventKind = domain.EventOriginal
			amount = a.Total
		}

		events = append(events, domain.ProducerEconomicEvent{
			ID:                ulidpkg.New().String(),
			TenantID:          pool.TenantID,
			PoolID:            pool.ID,
			AllocationID:      a.ID,
			ProducerRef:       a.ProducerRef,
			Amount:            amount,
			Kind:              eventKind,
			SupersedesEventID: supersedes,
			Origin:            origin.Origin{Kind: origin.Derived, DerivationID: derivationID},
			CreatedBy:         actor,
		})
	}

	return s.repo.CreateEconomicEvents(ctx, events)
}

func (s *Service) latestEventPerProducer(ctx context.Context, tenantID, poolID string) (map[string]string, error) {
	events, err := s.repo.ListEconomicEvents(ctx, tenantID, poolID, "", 500, 0)
	if err != nil {
		return nil, fmt.Errorf("load the pool's settled events: %w", err)
	}
	// Newest first, so the first sighting of a producer is the event a
	// correction must supersede.
	latest := make(map[string]string, len(events))
	for _, e := range events {
		if _, seen := latest[e.ProducerRef]; !seen {
			latest[e.ProducerRef] = e.ID
		}
	}
	return latest, nil
}

// validateQuantity rejects a literal the domain could not weight at three
// decimals. The currency is irrelevant to a quantity and is a placeholder.
func validateQuantity(q, field string) error {
	if q == "" {
		return errors.New(field + " is required")
	}
	if _, err := money.Parse(q, 3, "XXX"); err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	return nil
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
