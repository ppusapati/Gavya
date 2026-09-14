package domain

import (
	"github.com/ppusapati/gavya/libs/integrity/exact"
	"strings"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/origin"
)

// SubjectKind names what an observation is about.
//
// Each kind has its own typed, nullable column in the observations table rather
// than sharing a (subject_type, subject_id) pair. A pair cannot carry a foreign
// key and cannot be type checked, so a typo in the type string silently creates
// a subject that will never join to anything.
type SubjectKind string

const (
	SubjectCattle   SubjectKind = "CATTLE"
	SubjectProducer SubjectKind = "PRODUCER"
	SubjectRoute    SubjectKind = "ROUTE"
	SubjectTanker   SubjectKind = "TANKER"
	SubjectBatch    SubjectKind = "BATCH"
)

// SubjectRef identifies the one thing an observation measures.
type SubjectRef struct {
	Kind SubjectKind `json:"kind"`
	ID   string      `json:"id"`
}

func (s SubjectRef) Valid() bool {
	if s.ID == "" {
		return false
	}
	switch s.Kind {
	case SubjectCattle, SubjectProducer, SubjectRoute, SubjectTanker, SubjectBatch:
		return true
	default:
		return false
	}
}

// Ref renders the subject in the form the ML tier expects, "cattle:01H...".
func (s SubjectRef) Ref() string {
	return strings.ToLower(string(s.Kind)) + ":" + s.ID
}

// QuantityKind is the closed vocabulary of measurable quantities. It is closed
// because a settlement rule selects observations by quantity: a free-text kind
// would let a mistyped quantity drop silently out of every payment.
type QuantityKind string

const (
	QuantityVolumeLitres      QuantityKind = "VOLUME_LITRES"
	QuantityMassKG            QuantityKind = "MASS_KG"
	QuantityFatPercent        QuantityKind = "FAT_PERCENT"
	QuantitySNFPercent        QuantityKind = "SNF_PERCENT"
	QuantityLactosePercent    QuantityKind = "LACTOSE_PERCENT"
	QuantityProteinPercent    QuantityKind = "PROTEIN_PERCENT"
	QuantityTemperatureC      QuantityKind = "TEMPERATURE_C"
	QuantitySomaticCellCount  QuantityKind = "SOMATIC_CELL_COUNT"
	QuantityAdulterationIndex QuantityKind = "ADULTERATION_INDEX"
)

// AllQuantityKinds is the closed vocabulary, in one place, so a regime can be
// defined by what it excludes rather than by relisting everything it covers.
func AllQuantityKinds() []QuantityKind {
	return []QuantityKind{
		QuantityVolumeLitres, QuantityMassKG, QuantityFatPercent, QuantitySNFPercent,
		QuantityLactosePercent, QuantityProteinPercent, QuantityTemperatureC,
		QuantitySomaticCellCount, QuantityAdulterationIndex,
	}
}

func (q QuantityKind) Valid() bool {
	switch q {
	case QuantityVolumeLitres, QuantityMassKG, QuantityFatPercent, QuantitySNFPercent,
		QuantityLactosePercent, QuantityProteinPercent, QuantityTemperatureC,
		QuantitySomaticCellCount, QuantityAdulterationIndex:
		return true
	default:
		return false
	}
}

// IsTradeCritical reports whether this quantity enters the price a producer is
// paid.
//
// Whether a paying quantity is *regulated* is a regime's decision, not this
// type's — see Regime.Regulates. This stays because "does money rest on it" is
// a fact about the quantity itself, and every regime starts from it.
func (q QuantityKind) IsTradeCritical() bool {
	switch q {
	case QuantityTemperatureC, QuantitySomaticCellCount, QuantityAdulterationIndex:
		return false
	default:
		return q.Valid()
	}
}

// Unit is fixed by the quantity kind rather than supplied by the caller, so a
// litre reading can never be stored labelled as kilograms.
func (q QuantityKind) Unit() string {
	switch q {
	case QuantityVolumeLitres:
		return "L"
	case QuantityMassKG:
		return "kg"
	case QuantityFatPercent, QuantitySNFPercent, QuantityLactosePercent, QuantityProteinPercent:
		return "%"
	case QuantityTemperatureC:
		return "degC"
	case QuantitySomaticCellCount:
		return "cells/mL"
	case QuantityAdulterationIndex:
		return "index"
	default:
		return ""
	}
}

// InstrumentKind names the class of measuring instrument.
type InstrumentKind string

const (
	InstrumentWeighbridge   InstrumentKind = "WEIGHBRIDGE"
	InstrumentMilkAnalyser  InstrumentKind = "MILK_ANALYSER"
	InstrumentPlatformScale InstrumentKind = "PLATFORM_SCALE"
	InstrumentFlowMeter     InstrumentKind = "FLOW_METER"
	InstrumentThermometer   InstrumentKind = "THERMOMETER"
	InstrumentManual        InstrumentKind = "MANUAL_ENTRY"
)

func (k InstrumentKind) Valid() bool {
	switch k {
	case InstrumentWeighbridge, InstrumentMilkAnalyser, InstrumentPlatformScale,
		InstrumentFlowMeter, InstrumentThermometer, InstrumentManual:
		return true
	default:
		return false
	}
}

// Instrument is a physical measuring device whose readings become observations.
type Instrument struct {
	ID       string         `json:"id"`
	TenantID string         `json:"tenant_id"`
	Serial   string         `json:"serial"`
	Kind     InstrumentKind `json:"kind"`
	Label    string         `json:"label,omitempty"`
	Make     string         `json:"make,omitempty"`
	Model    string         `json:"model,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

// VerificationCertificate is a stamping certificate issued under the Legal
// Metrology Act by a state verification authority.
//
// The authority and number are stored as given, empty included: an instrument
// imported from an incumbent system often arrives with those fields blank, and
// recording the blank is what lets the eligibility verdict be UNKNOWN rather
// than a guess in either direction.
type VerificationCertificate struct {
	ID                 string    `json:"id"`
	TenantID           string    `json:"tenant_id"`
	InstrumentID       string    `json:"instrument_id"`
	CertificateNumber  string    `json:"certificate_number"`
	VerifyingAuthority string    `json:"verifying_authority"`
	IssuedAt           time.Time `json:"issued_at"`
	ExpiresAt          time.Time `json:"expires_at"`

	Origin origin.Origin `json:"origin"`

	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

// EligibilityVerdict says whether an observation may be used to settle a
// payment. UNKNOWN is a first-class answer, not a variant of NOT_ELIGIBLE.
type EligibilityVerdict string

const (
	EligibilityEligible    EligibilityVerdict = "ELIGIBLE"
	EligibilityNotEligible EligibilityVerdict = "NOT_ELIGIBLE"
	EligibilityUnknown     EligibilityVerdict = "UNKNOWN"
)

// UncertaintyEstimate is the measurement uncertainty budget evaluated for one
// observation, in the sense of the GUM.
type UncertaintyEstimate struct {
	ModelID             string    `json:"uncertainty_model_id"`
	ModelVersion        string    `json:"model_version"`
	StandardUncertainty float64   `json:"standard_uncertainty"`
	ExpandedUncertainty float64   `json:"expanded_uncertainty"`
	CoverageFactor      float64   `json:"coverage_factor"`
	CoverageProbability float64   `json:"coverage_probability"`
	EstimatedAt         time.Time `json:"estimated_at"`
}

// AnomalyAssessment is the ML tier's advisory opinion of one observation
// against the subject's own history. It marks an observation for review and
// never rejects it.
type AnomalyAssessment struct {
	Score        float64 `json:"score"`
	Flagged      bool    `json:"flagged"`
	Method       string  `json:"method"`
	ModelVersion string  `json:"model_version"`
	// LowerBound and UpperBound are nil when the scorer established no baseline
	// and the tolerance band is therefore unbounded. Zero would read as an
	// infinitely tight band, which is the opposite of what nil means.
	LowerBound  *float64  `json:"lower_bound,omitempty"`
	UpperBound  *float64  `json:"upper_bound,omitempty"`
	Explanation string    `json:"explanation,omitempty"`
	ScoredAt    time.Time `json:"scored_at"`
}

func (a AnomalyAssessment) Bounded() bool { return a.LowerBound != nil && a.UpperBound != nil }

// Observation is one measured fact about one subject over one valid interval.
//
// It is append-only. A correction is a new observation that supersedes this
// one; the superseded row keeps its original value forever, because a payment
// already made was made on the number as it stood then.
// The value column: NUMERIC(20,6). A reading arriving from the wire is checked
// against it and held at its scale.
//
// The check is Column and not NonNegativeColumn: TEMPERATURE_C is a quantity
// kind here, and a cooling tank below zero is the ordinary case rather than a
// mistake.
const (
	ValueScale     int32 = 6
	ValuePrecision int32 = 20
)

type Observation struct {
	ID       string       `json:"id"`
	TenantID string       `json:"tenant_id"`
	Subject  SubjectRef   `json:"subject"`
	Quantity QuantityKind `json:"quantity_kind"`
	// Value is what was measured, exact at the column's six decimals.
	//
	// It was a float64 from the wire to the column and back, in the one service
	// whose whole subject is recording measurements. balance-service already
	// holds its measured flows as exact decimals and only its statistics as
	// floats; this is the same arrangement. The uncertainty and anomaly figures
	// below stay floats because they are results of floating-point computation
	// in the ML tier, not things anybody measured.
	Value exact.Fixed `json:"value"`
	Unit  string      `json:"unit"`

	InstrumentID string `json:"instrument_id,omitempty"`
	// SessionRef names the capture session the reading arrived in. It is a
	// reference, not a foreign key: sessions are owned by ingestion-service.
	SessionRef string `json:"session_ref,omitempty"`
	ObservedBy string `json:"observed_by,omitempty"`

	Origin origin.Origin `json:"origin"`

	ValidFrom    time.Time  `json:"valid_from"`
	ValidTo      time.Time  `json:"valid_to"`
	RecordedAt   time.Time  `json:"recorded_at"`
	SupersededAt *time.Time `json:"superseded_at,omitempty"`
	SupersededBy string     `json:"superseded_by,omitempty"`
	// Supersedes names the observation this one corrects, so the correction
	// chain reads in both directions.
	Supersedes string `json:"supersedes,omitempty"`

	EligibilityVerdict       EligibilityVerdict `json:"eligibility_verdict"`
	EligibilityReason        string             `json:"eligibility_reason"`
	EligibilityCertificateID string             `json:"eligibility_certificate_id,omitempty"`

	UncertaintyModelID string `json:"uncertainty_model_id,omitempty"`
	// Uncertainty is nil when the estimate could not be obtained. The
	// observation is recorded either way: a settlement must never be blocked
	// because a model was unreachable.
	Uncertainty *UncertaintyEstimate `json:"uncertainty,omitempty"`
	Anomaly     *AnomalyAssessment   `json:"anomaly,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

func (o *Observation) IsCurrent() bool { return o.SupersededAt == nil }

// UncertaintyMissing reports that no uncertainty estimate is attached, so a
// consumer knows the value carries no stated confidence rather than a perfect
// one.
func (o *Observation) UncertaintyMissing() bool { return o.Uncertainty == nil }

// NeedsReview reports whether the observation sits in the anomaly queue. A flag
// is cleared by correcting the observation, which supersedes it.
func (o *Observation) NeedsReview() bool {
	return o.Anomaly != nil && o.Anomaly.Flagged && o.SupersededAt == nil
}
