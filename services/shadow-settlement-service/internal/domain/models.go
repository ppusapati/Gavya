package domain

import (
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/libs/integrity/origin"
)

// ComponentKind names a line in a settlement. The vocabulary is closed so an
// external assertion and a shadow computation can be compared line by line
// rather than only on their totals.
type ComponentKind string

const (
	ComponentBasePrice        ComponentKind = "BASE_PRICE"
	ComponentFatIncentive     ComponentKind = "FAT_INCENTIVE"
	ComponentSNFIncentive     ComponentKind = "SNF_INCENTIVE"
	ComponentQualityBonus     ComponentKind = "QUALITY_BONUS"
	ComponentVolumeBonus      ComponentKind = "VOLUME_BONUS"
	ComponentTransportDeduct  ComponentKind = "TRANSPORT_DEDUCTION"
	ComponentFeedRecovery     ComponentKind = "FEED_RECOVERY"
	ComponentLoanRecovery     ComponentKind = "LOAN_RECOVERY"
	ComponentAdvanceRecovery  ComponentKind = "ADVANCE_RECOVERY"
	ComponentPenaltyDeduction ComponentKind = "PENALTY_DEDUCTION"
	ComponentRoundingAdjust   ComponentKind = "ROUNDING_ADJUSTMENT"
	ComponentOther            ComponentKind = "OTHER"
)

// IsRecovery reports whether a component recovers a prior advance from the
// producer. Recoveries are the single most common cause of a legitimate
// divergence, because the two systems disagree on which period a recovery
// falls in rather than on the milk itself.
func (c ComponentKind) IsRecovery() bool {
	switch c {
	case ComponentFeedRecovery, ComponentLoanRecovery, ComponentAdvanceRecovery:
		return true
	default:
		return false
	}
}

// Component is one line of a settlement.
type Component struct {
	Kind   ComponentKind `json:"kind"`
	Label  string        `json:"label,omitempty"`
	Amount money.Money   `json:"amount"`
	// Quantity and Rate are carried when the line is a product of the two, so a
	// divergence can be attributed to the input rather than to the money.
	Quantity string `json:"quantity,omitempty"`
	Rate     string `json:"rate,omitempty"`
}

// ExternalSettlementAssertion is what the incumbent system says it paid.
//
// It is an assertion, not a fact: the platform records it verbatim, hashes it
// for replay detection, and never edits it. A correction from the source
// arrives as a new version rather than an update.
type ExternalSettlementAssertion struct {
	ID                   string        `json:"id"`
	TenantID             string        `json:"tenant_id"`
	SourceSystemID       string        `json:"source_system_id"`
	ExternalSettlementID string        `json:"external_settlement_id"`
	ProducerRef          string        `json:"producer_ref"`
	PeriodStart          time.Time     `json:"period_start"`
	PeriodEnd            time.Time     `json:"period_end"`
	Total                money.Money   `json:"total"`
	Components           []Component   `json:"components"`
	AssertedAt           time.Time     `json:"asserted_at"`
	Origin               origin.Origin `json:"origin"`

	ValidFrom    time.Time  `json:"valid_from"`
	ValidTo      time.Time  `json:"valid_to"`
	RecordedAt   time.Time  `json:"recorded_at"`
	SupersededAt *time.Time `json:"superseded_at,omitempty"`
	SupersededBy string     `json:"superseded_by,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

// ShadowSettlementComputation is what this platform independently computed for
// the same producer and period.
//
// In shadow mode it is never paid out. Its only purpose is to be compared with
// the assertion, so it carries everything needed to defend the number: the
// policy version, the rate card, the digest of the inputs it consumed, and the
// full rounding trail.
type ShadowSettlementComputation struct {
	ID          string      `json:"id"`
	TenantID    string      `json:"tenant_id"`
	AssertionID string      `json:"assertion_id,omitempty"`
	ProducerRef string      `json:"producer_ref"`
	PeriodStart time.Time   `json:"period_start"`
	PeriodEnd   time.Time   `json:"period_end"`
	Total       money.Money `json:"total"`
	Components  []Component `json:"components"`

	PolicyVersion string               `json:"policy_version"`
	RateCardID    string               `json:"rate_card_id"`
	RoundingTrail []money.RoundingStep `json:"rounding_trail"`
	// InputDigest fixes exactly which observations fed the computation, so a
	// replay that produces a different total can be told from one that simply
	// consumed different inputs.
	InputDigest string `json:"input_digest"`
	// AsOf is the transaction time the inputs were read at. Recomputing with
	// the same AsOf must produce the same total.
	AsOf       time.Time     `json:"as_of"`
	Origin     origin.Origin `json:"origin"`
	ComputedAt time.Time     `json:"computed_at"`
	RecordedAt time.Time     `json:"recorded_at"`

	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

// Classification is the closed vocabulary for why an assertion and a shadow
// computation differ.
type Classification string

const (
	// ClassMatch means the two totals agree exactly.
	ClassMatch Classification = "MATCH"
	// ClassRounding means the delta is within the rounding the two systems'
	// declared policies can produce.
	ClassRounding Classification = "ROUNDING_DIFFERENCE"
	// ClassInput means the systems agree on the rules but consumed different
	// measurements.
	ClassInput Classification = "INPUT_DIFFERENCE"
	// ClassPolicy means the systems applied different rate cards or rules.
	ClassPolicy Classification = "POLICY_DIFFERENCE"
	// ClassRecovery means the delta is attributable to recovery components.
	ClassRecovery Classification = "RECOVERY_DIFFERENCE"
	// ClassUnexplained means the deterministic classifier found no attribution.
	// This is the only class the ML tier is ever consulted about.
	ClassUnexplained Classification = "UNEXPLAINED"
	// ClassInsufficientEvidence means the comparison could not be made at all:
	// a missing counterpart, mismatched currency, or absent component detail.
	ClassInsufficientEvidence Classification = "INSUFFICIENT_EVIDENCE"
)

// DivergenceStatus tracks the human workflow over a divergence.
type DivergenceStatus string

const (
	StatusOpen         DivergenceStatus = "OPEN"
	StatusUnderReview  DivergenceStatus = "UNDER_REVIEW"
	StatusAccepted     DivergenceStatus = "ACCEPTED"
	StatusExternalWins DivergenceStatus = "EXTERNAL_CONFIRMED"
	StatusShadowWins   DivergenceStatus = "SHADOW_CONFIRMED"
	StatusResolved     DivergenceStatus = "RESOLVED"
)

// Evidence is the deterministic per-component comparison behind a
// classification. A reviewer must be able to reach the same conclusion from
// this alone, without rerunning anything.
type Evidence struct {
	Kind ComponentKind `json:"kind"`
	// ExternalMinorUnits and ShadowMinorUnits are the two sides' amounts.
	ExternalMinorUnits int64 `json:"external_minor_units"`
	ShadowMinorUnits   int64 `json:"shadow_minor_units"`
	DeltaMinorUnits    int64 `json:"delta_minor_units"`
	// Present flags a component that only one side produced at all.
	OnlyExternal bool `json:"only_external,omitempty"`
	OnlyShadow   bool `json:"only_shadow,omitempty"`
	// QuantityDiffers and RateDiffers separate an input disagreement from a
	// rule disagreement on the same line.
	QuantityDiffers bool `json:"quantity_differs,omitempty"`
	RateDiffers     bool `json:"rate_differs,omitempty"`
}

// MLHypothesis is the advisory explanation from the Rust divergence service.
// It is stored beside the deterministic classification and never replaces it.
type MLHypothesis struct {
	Classification   string   `json:"classification"`
	Confidence       float64  `json:"confidence"`
	Rationale        string   `json:"rationale"`
	SupportingFields []string `json:"supporting_fields,omitempty"`
	ModelVersion     string   `json:"model_version"`
}

// SettlementDivergence is the adjudicable record of a difference.
type SettlementDivergence struct {
	ID            string `json:"id"`
	TenantID      string `json:"tenant_id"`
	AssertionID   string `json:"assertion_id"`
	ComputationID string `json:"computation_id"`
	ProducerRef   string `json:"producer_ref"`

	Delta          money.Money    `json:"delta"`
	Classification Classification `json:"classification"`
	// Rationale states the deterministic reason in plain language.
	Rationale string     `json:"rationale"`
	Evidence  []Evidence `json:"evidence"`

	// MLHypotheses is populated only when Classification is UNEXPLAINED.
	MLHypotheses []MLHypothesis `json:"ml_hypotheses,omitempty"`

	Status     DivergenceStatus `json:"status"`
	ResolvedAt *time.Time       `json:"resolved_at,omitempty"`
	ResolvedBy string           `json:"resolved_by,omitempty"`
	Resolution string           `json:"resolution,omitempty"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	CreatedBy string     `json:"created_by"`
	UpdatedBy string     `json:"updated_by"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// ClassSummary is the divergence mix over a period, which is the headline the
// integrity workspace reports: how much money the two systems disagree about,
// and how much of that disagreement is understood.
type ClassSummary struct {
	Classification     Classification `json:"classification"`
	Currency           string         `json:"currency"`
	Count              int64          `json:"count"`
	TotalAbsMinorUnits int64          `json:"total_abs_minor_units"`
}

// NeedsHumanReview reports whether a divergence must reach the integrity
// workspace. An unexplained difference and one the platform could not evaluate
// at all both do; an exact match does not.
func (d *SettlementDivergence) NeedsHumanReview() bool {
	switch d.Classification {
	case ClassMatch:
		return false
	case ClassRounding:
		// Rounding is expected and self-explaining, but only while it stays at
		// the scale rounding can actually produce.
		return d.Delta.Abs().Value > 2
	default:
		return true
	}
}
