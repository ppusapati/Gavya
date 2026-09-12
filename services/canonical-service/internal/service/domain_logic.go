package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/bitemporal"
	"github.com/ppusapati/gavya/libs/integrity/origin"
	ulidpkg "p9e.in/samavaya/packages/ulid"

	"github.com/ppusapati/gavya/services/canonical-service/internal/domain"
	"github.com/ppusapati/gavya/services/canonical-service/internal/repository"
)

// MapIdentityInput asserts that an external identifier means a platform entity
// over a period.
type MapIdentityInput struct {
	TenantID       string
	SourceSystemID string
	EntityKind     domain.EntityKind
	ExternalID     string
	EntityID       string
	Method         domain.MappingMethod
	Confidence     float64
	Note           string
	ValidFrom      time.Time
	ValidTo        time.Time
	Actor          string
}

func (s *Service) MapIdentity(ctx context.Context, in MapIdentityInput) (*domain.ExternalIdentity, error) {
	if err := validateMapping(in); err != nil {
		return nil, err
	}

	validTo := in.ValidTo
	if validTo.IsZero() {
		validTo = bitemporal.EndOfTime
	}
	if err := (bitemporal.Interval{From: in.ValidFrom, To: validTo}).Validate(); err != nil {
		return nil, err
	}

	return s.repo.CreateIdentity(ctx, &domain.ExternalIdentity{
		ID:             ulidpkg.New().String(),
		TenantID:       in.TenantID,
		SourceSystemID: in.SourceSystemID,
		EntityKind:     in.EntityKind,
		ExternalID:     in.ExternalID,
		EntityID:       in.EntityID,
		Method:         in.Method,
		Confidence:     in.Confidence,
		Note:           in.Note,
		ValidFrom:      in.ValidFrom,
		ValidTo:        validTo,
		CreatedBy:      in.Actor,
	})
}

// ResolveIdentity answers what an external identifier meant at an instant.
func (s *Service) ResolveIdentity(ctx context.Context, tenantID, sourceSystemID string, kind domain.EntityKind, externalID string, asOf time.Time) (*domain.ExternalIdentity, error) {
	if asOf.IsZero() {
		return nil, errors.New("as_of is required: external identifiers are reused, so a mapping has no meaning without an instant to resolve at")
	}
	return s.repo.ResolveIdentity(ctx, tenantID, sourceSystemID, kind, externalID, asOf)
}

func (s *Service) ReverseResolve(ctx context.Context, tenantID string, kind domain.EntityKind, entityID string) ([]*domain.ExternalIdentity, error) {
	return s.repo.ReverseResolve(ctx, tenantID, kind, entityID)
}

func (s *Service) ListIdentities(ctx context.Context, tenantID, sourceSystemID string, limit, offset int) ([]*domain.ExternalIdentity, error) {
	return s.repo.ListIdentities(ctx, tenantID, sourceSystemID, clampLimit(limit), clampOffset(offset))
}

// IdentityHistory returns every mapping ever recorded for one external
// identifier, retired ones included.
//
// Every other read of external_identities filters superseded_at IS NULL, which
// is right — a retired mapping must never resolve anything. The consequence was
// that the history was written to columns nothing returned, and the question a
// member actually asks had no answer through the API: this collection was
// attributed to me, why. The mapping that answers it is the retired one.
func (s *Service) IdentityHistory(ctx context.Context, tenantID, sourceSystemID string, kind domain.EntityKind, externalID string) ([]*domain.ExternalIdentity, error) {
	if sourceSystemID == "" || externalID == "" {
		return nil, errors.New("source_system_id and external_id are both required: " +
			"an identifier is only unique within the system that issued it")
	}
	return s.repo.IdentityHistory(ctx, tenantID, sourceSystemID, kind, externalID)
}

// RetireIdentity closes a mapping that was wrong or has been replaced.
//
// The row stays. This comment used to say it stays "readable", which it was not:
// every read filtered it out, so a settlement computed under the old mapping was
// explainable only to somebody with a database console. IdentityHistory above is
// what makes the sentence true.
func (s *Service) RetireIdentity(ctx context.Context, tenantID, id, actor string) error {
	if actor == "" {
		return errors.New("actor is required")
	}
	return s.repo.SupersedeIdentity(ctx, tenantID, id, actor)
}

// DeclarePolicyInput defines what makes two records the same collection.
type DeclarePolicyInput struct {
	TenantID      string
	Name          string
	Dimensions    []domain.IdentityDimension
	Resolution    domain.ResolutionMode
	Version       int32
	EffectiveFrom time.Time
	EffectiveTo   *time.Time
	Actor         string
}

// DeclarePolicy stores a collection identity policy after checking it is usable.
//
// Validating here rather than at claim time matters: an unusable policy would
// otherwise surface as a failure to place a collection, in the field, at the
// worst possible moment.
func (s *Service) DeclarePolicy(ctx context.Context, in DeclarePolicyInput) (*domain.CollectionIdentityPolicy, error) {
	switch {
	case in.TenantID == "":
		return nil, errors.New("tenant_id is required")
	case in.Name == "":
		return nil, errors.New("name is required")
	case in.Actor == "":
		return nil, errors.New("actor is required")
	case in.EffectiveFrom.IsZero():
		return nil, errors.New("effective_from is required")
	}

	version := in.Version
	if version < 1 {
		version = 1
	}

	p := &domain.CollectionIdentityPolicy{
		ID:            ulidpkg.New().String(),
		TenantID:      in.TenantID,
		Name:          in.Name,
		Dimensions:    in.Dimensions,
		Resolution:    in.Resolution,
		Version:       version,
		EffectiveFrom: in.EffectiveFrom,
		EffectiveTo:   in.EffectiveTo,
		CreatedBy:     in.Actor,
	}
	if err := domain.ValidatePolicy(p); err != nil {
		return nil, err
	}
	return s.repo.CreatePolicy(ctx, p)
}

func (s *Service) GetEffectivePolicy(ctx context.Context, tenantID string, at time.Time) (*domain.CollectionIdentityPolicy, error) {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	return s.repo.GetEffectivePolicy(ctx, tenantID, at)
}

func (s *Service) ListPolicies(ctx context.Context, tenantID string) ([]*domain.CollectionIdentityPolicy, error) {
	return s.repo.ListPolicies(ctx, tenantID)
}

// ClaimInput is a record asserting it is the collection for a slot.
type ClaimInput struct {
	TenantID   string
	SourceRef  string
	Values     map[domain.IdentityDimension]string
	Origin     origin.Kind
	RecordedAt time.Time
	Quality    int32
	// CollectedAt selects the policy version in force when the milk was
	// collected, not the one in force today. Re-adjudicating an old collection
	// under a new policy would silently repartition history.
	CollectedAt time.Time
	Actor       string
}

// Claim places a record into its authoritative collection slot.
func (s *Service) Claim(ctx context.Context, in ClaimInput) (*repository.ClaimResult, error) {
	switch {
	case in.TenantID == "":
		return nil, errors.New("tenant_id is required")
	case in.SourceRef == "":
		return nil, errors.New("source_ref is required")
	case in.Origin == "":
		return nil, errors.New("origin is required")
	case in.Actor == "":
		return nil, errors.New("actor is required")
	}
	if _, err := origin.ParseKind(string(in.Origin)); err != nil {
		return nil, err
	}

	collectedAt := in.CollectedAt
	if collectedAt.IsZero() {
		collectedAt = in.RecordedAt
	}
	policy, err := s.repo.GetEffectivePolicy(ctx, in.TenantID, collectedAt)
	if err != nil {
		return nil, fmt.Errorf("no collection identity policy in force at %s: %w",
			collectedAt.UTC().Format(time.RFC3339), err)
	}

	claim := domain.Claim{
		SourceRef:  in.SourceRef,
		Values:     in.Values,
		Origin:     in.Origin,
		RecordedAt: in.RecordedAt,
		Quality:    in.Quality,
	}

	slotKey, err := domain.SlotKey(policy, claim)
	if err != nil {
		return nil, err
	}

	res, err := s.repo.ClaimSlot(ctx, repository.ClaimInput{
		TenantID: in.TenantID,
		SlotKey:  slotKey,
		Claim:    claim,
		Policy:   policy,
		Actor:    in.Actor,
		SlotID:   ulidpkg.New().String(),
	}, domain.PlaceClaim)
	if err != nil {
		return nil, err
	}

	if res.Decision.Outcome == domain.OutcomeConflict {
		s.log.Warnf("collection slot conflict: tenant=%s slot=%s claim=%s: %s",
			in.TenantID, slotKey, in.SourceRef, res.Decision.Reason)
	}
	return res, nil
}

func (s *Service) GetSlot(ctx context.Context, tenantID, slotKey string, kind origin.Kind) (*domain.AuthoritativeCollectionSlot, error) {
	return s.repo.GetSlot(ctx, tenantID, slotKey, kind)
}

func (s *Service) ListConflicts(ctx context.Context, tenantID string, limit, offset int) ([]*domain.AuthoritativeCollectionSlot, error) {
	return s.repo.ListConflicts(ctx, tenantID, clampLimit(limit), clampOffset(offset))
}

// ResolveConflict records which claim a human chose.
//
// The chosen reference must be one of the claims already on the slot: a
// resolution that introduces a record nobody claimed would be unauditable.
func (s *Service) ResolveConflict(ctx context.Context, tenantID, slotID, authoritativeRef, resolution, actor string) (*domain.AuthoritativeCollectionSlot, error) {
	switch {
	case authoritativeRef == "":
		return nil, errors.New("a resolution must name the claim that holds the slot")
	case resolution == "":
		return nil, errors.New("a resolution note is required so the decision is auditable")
	case actor == "":
		return nil, errors.New("actor is required")
	}
	return s.repo.ResolveConflict(ctx, tenantID, slotID, authoritativeRef, resolution, actor)
}

func validateMapping(in MapIdentityInput) error {
	switch {
	case in.TenantID == "":
		return errors.New("tenant_id is required")
	case in.SourceSystemID == "":
		return errors.New("source_system_id is required")
	case in.ExternalID == "":
		return errors.New("external_id is required")
	case in.EntityID == "":
		return errors.New("entity_id is required")
	case in.ValidFrom.IsZero():
		return errors.New("valid_from is required")
	case in.Actor == "":
		return errors.New("actor is required")
	}
	switch in.EntityKind {
	case domain.EntityProducer, domain.EntityCattle, domain.EntityRoute,
		domain.EntityCentre, domain.EntityDevice, domain.EntitySettlement:
	default:
		return fmt.Errorf("entity kind %q is not recognised", in.EntityKind)
	}
	switch in.Method {
	case domain.MappingExact, domain.MappingManual:
	case domain.MappingInferred:
		// An inferred mapping without a confidence cannot be triaged, and
		// inferred mappings are the first thing to re-examine when a producer's
		// settlement diverges.
		if in.Confidence <= 0 || in.Confidence > 1 {
			return errors.New("an inferred mapping requires a confidence in (0,1]")
		}
	default:
		return fmt.Errorf("mapping method %q is not recognised", in.Method)
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
