package domain

import (
	"time"

	"github.com/ppusapati/gavya/libs/integrity/origin"
)

// EntityKind names what an external identifier points at.
type EntityKind string

const (
	EntityProducer   EntityKind = "PRODUCER"
	EntityCattle     EntityKind = "CATTLE"
	EntityRoute      EntityKind = "ROUTE"
	EntityCentre     EntityKind = "CENTRE"
	EntityDevice     EntityKind = "DEVICE"
	EntitySettlement EntityKind = "SETTLEMENT"
)

// MappingMethod records how a mapping was arrived at, because a fuzzy match and
// a keyed match do not deserve the same trust when a divergence is traced back
// to a mis-mapped producer.
type MappingMethod string

const (
	// MappingExact is a keyed match on an identifier both systems agree on.
	MappingExact MappingMethod = "EXACT"
	// MappingManual was asserted by an implementer during data mapping.
	MappingManual MappingMethod = "MANUAL"
	// MappingInferred was derived from name, village and route similarity.
	// Mappings of this kind are the first thing to re-examine when a producer's
	// settlement diverges inexplicably.
	MappingInferred MappingMethod = "INFERRED"
)

// ExternalIdentity maps one source system's identifier to a platform entity.
//
// The mapping is bitemporal because external identifiers are reused: a
// cooperative retires a member number and issues it to a new farmer years
// later. Resolving without a time therefore has no single right answer, and
// every lookup must say when it is asking about.
type ExternalIdentity struct {
	ID             string        `json:"id"`
	TenantID       string        `json:"tenant_id"`
	SourceSystemID string        `json:"source_system_id"`
	EntityKind     EntityKind    `json:"entity_kind"`
	ExternalID     string        `json:"external_id"`
	EntityID       string        `json:"entity_id"`
	Method         MappingMethod `json:"method"`
	// Confidence is meaningful only for INFERRED mappings.
	Confidence float64 `json:"confidence,omitempty"`
	Note       string  `json:"note,omitempty"`

	ValidFrom time.Time `json:"valid_from"`
	ValidTo   time.Time `json:"valid_to"`

	RecordedAt   time.Time  `json:"recorded_at"`
	SupersededAt *time.Time `json:"superseded_at,omitempty"`
	SupersededBy string     `json:"superseded_by,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

// IdentityDimension is one component of what makes a collection "the same
// collection".
type IdentityDimension string

const (
	DimProducer       IdentityDimension = "PRODUCER"
	DimCollectionDate IdentityDimension = "COLLECTION_DATE"
	DimShift          IdentityDimension = "SHIFT"
	DimRoute          IdentityDimension = "ROUTE"
	DimCentre         IdentityDimension = "CENTRE"
	DimDevice         IdentityDimension = "DEVICE"
)

// ResolutionMode says what to do when two collections claim one slot.
type ResolutionMode string

const (
	// ResolveFirstWins keeps the earliest claim. Suits a cooperative whose
	// operators occasionally re-enter a collection they think failed to save.
	ResolveFirstWins ResolutionMode = "FIRST_WINS"
	// ResolveLastWins keeps the latest claim, on the view that a re-entry is a
	// correction.
	ResolveLastWins ResolutionMode = "LAST_WINS"
	// ResolveHighestQuality prefers the claim from the better-evidenced
	// measurement — a verified instrument over a manual entry.
	ResolveHighestQuality ResolutionMode = "HIGHEST_QUALITY"
	// ResolveManual never auto-resolves. Every collision waits for a human.
	ResolveManual ResolutionMode = "MANUAL"
)

// CollectionIdentityPolicy declares, per tenant, what makes two records the
// same collection.
//
// This cannot be a platform-wide constant. A cooperative collecting once a day
// per producer identifies a collection by producer and date; one running
// morning and evening shifts needs the shift too; one where a producer may
// deliver to more than one centre in a day needs the centre. Getting this wrong
// in either direction is expensive: too coarse silently merges two real
// collections into one payment, too fine lets a duplicate entry be paid twice.
type CollectionIdentityPolicy struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`
	// Dimensions is ordered, and the order is part of the policy's identity:
	// the slot key is built from it, so reordering produces different keys and
	// is therefore a new policy version rather than an edit.
	Dimensions []IdentityDimension `json:"dimensions"`
	Resolution ResolutionMode      `json:"resolution"`
	Version    int32               `json:"version"`

	EffectiveFrom time.Time  `json:"effective_from"`
	EffectiveTo   *time.Time `json:"effective_to,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

func (p *CollectionIdentityPolicy) IsEffectiveAt(t time.Time) bool {
	if t.Before(p.EffectiveFrom) {
		return false
	}
	return p.EffectiveTo == nil || t.Before(*p.EffectiveTo)
}

// SlotStatus is the state of an authoritative collection slot.
type SlotStatus string

const (
	// SlotSettled has exactly one authoritative claim.
	SlotSettled SlotStatus = "SETTLED"
	// SlotConflict has more than one and the policy could not choose. This is
	// the COLLECTION_SLOT_CONFLICT the amendment requires: it blocks nothing
	// upstream, but the slot has no authoritative answer until a human gives
	// it one.
	SlotConflict SlotStatus = "CONFLICT"
)

// Claim is one record asserting that it is the collection for a slot.
type Claim struct {
	// SourceRef identifies the claiming record: an observation or an imported
	// collection. Two claims with the same SourceRef are the same claim.
	SourceRef string `json:"source_ref"`
	// Values are the dimension values this claim carries. A claim missing a
	// value the policy requires cannot be placed in a slot at all.
	Values map[IdentityDimension]string `json:"values"`
	// Origin separates the platform's own capture from an incumbent's import.
	Origin origin.Kind `json:"origin"`
	// RecordedAt orders claims for the first- and last-wins modes.
	RecordedAt time.Time `json:"recorded_at"`
	// Quality ranks claims under HIGHEST_QUALITY. Higher is better.
	Quality int32 `json:"quality"`
}

// AuthoritativeCollectionSlot is the single answer to "what was collected from
// this producer, on this date, on this shift".
type AuthoritativeCollectionSlot struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	// SlotKey is derived deterministically from the policy's dimensions and the
	// claim's values, so the same collection always lands in the same slot.
	SlotKey string `json:"slot_key"`
	// OriginKind is part of the slot's identity. In shadow mode the platform
	// holds an imported collection and its own recomputed one side by side;
	// they are not competing claims and must not be forced into one slot. The
	// comparison between them is the shadow settlement's job, not this one's.
	OriginKind origin.Kind `json:"origin_kind"`

	PolicyID      string `json:"policy_id"`
	PolicyVersion int32  `json:"policy_version"`

	// AuthoritativeRef is the claim that currently holds the slot. It is empty
	// while the slot is in conflict, because a conflicted slot has no answer.
	AuthoritativeRef string     `json:"authoritative_ref"`
	Status           SlotStatus `json:"status"`

	// The holding claim's own attributes, kept so a later claim can be compared
	// against it without reloading the record it came from. Storing them on the
	// slot is what lets PlaceClaim stay a pure function of its arguments.
	IncumbentRecordedAt time.Time `json:"incumbent_recorded_at"`
	IncumbentQuality    int32     `json:"incumbent_quality"`

	// Contenders are the claims that did not win, kept so a reviewer can see
	// what was set aside and why.
	Contenders []Contender `json:"contenders"`

	Values map[IdentityDimension]string `json:"values"`

	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	ResolvedBy string     `json:"resolved_by,omitempty"`
	Resolution string     `json:"resolution,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedBy string    `json:"created_by"`
	UpdatedBy string    `json:"updated_by"`
}

// Contender is a claim that did not take the slot.
type Contender struct {
	SourceRef  string      `json:"source_ref"`
	Origin     origin.Kind `json:"origin"`
	RecordedAt time.Time   `json:"recorded_at"`
	Quality    int32       `json:"quality"`
	// Reason states why this claim did not win, in the policy's terms.
	Reason string `json:"reason"`
}

func (s *AuthoritativeCollectionSlot) IsConflicted() bool { return s.Status == SlotConflict }
