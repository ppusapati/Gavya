package domain

import (
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"
	"github.com/ppusapati/gavya/libs/integrity/origin"
)

// UtilisationClass is how pooled milk was used. The classes follow the Federal
// Milk Marketing Order structure, which the platform carries as a structural
// fixture: an Indian cooperative does not file an FMMO, but the shape — pool
// the milk, value it by what it was used for, pay producers a blend of that
// value — is the same, and modelling it explicitly keeps the two separable.
type UtilisationClass string

const (
	// ClassI is fluid milk, which commands the highest price.
	ClassI UtilisationClass = "CLASS_I"
	// ClassII is soft products: curd, yoghurt, ice cream.
	ClassII UtilisationClass = "CLASS_II"
	// ClassIII is cheese and paneer.
	ClassIII UtilisationClass = "CLASS_III"
	// ClassIV is butter and powder, the balancing outlet.
	ClassIV UtilisationClass = "CLASS_IV"
)

func ValidClass(c UtilisationClass) bool {
	switch c {
	case ClassI, ClassII, ClassIII, ClassIV:
		return true
	default:
		return false
	}
}

// ComponentKind is a priced constituent of milk.
//
// Indian cooperatives price on fat and solids-not-fat; component pricing
// elsewhere splits SNF into protein and other solids. Both are expressible
// here, and which are in use is a property of the rate card rather than of the
// model.
type ComponentKind string

const (
	ComponentFat         ComponentKind = "FAT"
	ComponentSNF         ComponentKind = "SNF"
	ComponentProtein     ComponentKind = "PROTEIN"
	ComponentOtherSolids ComponentKind = "OTHER_SOLIDS"
	// ComponentVolume prices the milk itself rather than its constituents, for
	// orders that pay a flat rate per litre.
	ComponentVolume ComponentKind = "VOLUME"
)

// ProducerMilk is what one producer delivered into a pool for the period.
//
// Quantities are decimal literals rather than floats: a producer's payment is
// derived from them, and a binary float cannot represent 1234.567 kg exactly.
type ProducerMilk struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	PoolID      string `json:"pool_id"`
	ProducerRef string `json:"producer_ref"`

	// Quantity is the pooled volume or mass in the pool's unit.
	Quantity string `json:"quantity"`
	// Components maps each priced constituent to its quantity.
	Components map[ComponentKind]string `json:"components"`

	// SlotRefs names the authoritative collection slots this aggregate was
	// built from, so a producer's pooled quantity can be traced back to the
	// individual collections without re-deriving it.
	SlotRefs []string `json:"slot_refs"`

	Origin origin.Origin `json:"origin"`

	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

// PoolStatus tracks a pool through its lifecycle.
type PoolStatus string

const (
	// PoolOpen is still accepting producer milk.
	PoolOpen PoolStatus = "OPEN"
	// PoolValued has been computed but not yet distributed.
	PoolValued PoolStatus = "VALUED"
	// PoolSettled has produced its producer economic events.
	PoolSettled PoolStatus = "SETTLED"
	// PoolReopened was settled and has been reopened by a recovery policy.
	PoolReopened PoolStatus = "REOPENED"
)

// Pool is one marketing period's aggregation.
type Pool struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	Name        string     `json:"name"`
	PeriodStart time.Time  `json:"period_start"`
	PeriodEnd   time.Time  `json:"period_end"`
	Unit        string     `json:"unit"`
	Currency    string     `json:"currency"`
	Scale       int32      `json:"scale"`
	Status      PoolStatus `json:"status"`

	// RateCardID and PolicyVersion fix the rules the pool was valued under, so
	// a revaluation can be told from a rule change.
	RateCardID    string `json:"rate_card_id"`
	PolicyVersion string `json:"policy_version"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	CreatedBy string    `json:"created_by"`
	UpdatedBy string    `json:"updated_by"`
}

// ClassifiedUtilisation is how much of the pool went to one class, and what
// that class paid.
type ClassifiedUtilisation struct {
	ID       string           `json:"id"`
	TenantID string           `json:"tenant_id"`
	PoolID   string           `json:"pool_id"`
	Class    UtilisationClass `json:"class"`
	Quantity string           `json:"quantity"`
	// Price is per unit of Quantity.
	Price money.Rate `json:"price"`

	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

// ComponentPrice is the uniform price paid to every producer for a component,
// before the pool's residual value is shared out.
type ComponentPrice struct {
	Component ComponentKind `json:"component"`
	Price     money.Rate    `json:"price"`
}

// PoolValuation is the computed value of a pool and its blend price.
type PoolValuation struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	PoolID   string `json:"pool_id"`

	// ClassifiedValue is what the pool's milk was worth at class prices.
	ClassifiedValue money.Money `json:"classified_value"`
	// ComponentValue is what producers are owed at uniform component prices.
	ComponentValue money.Money `json:"component_value"`
	// ProducerSettlementFund is the residual shared out by contribution. It can
	// be negative when class prices fall below component prices, and that is a
	// real outcome rather than an error: producers then bear the shortfall.
	ProducerSettlementFund money.Money `json:"producer_settlement_fund"`

	TotalQuantity string `json:"total_quantity"`
	// BlendPrice is the pool's average realisation per unit, the number
	// producers actually compare between cooperatives.
	BlendPrice money.Rate `json:"blend_price"`

	RoundingTrail []money.RoundingStep `json:"rounding_trail"`
	Origin        origin.Origin        `json:"origin"`

	ComputedAt time.Time `json:"computed_at"`
	CreatedAt  time.Time `json:"created_at"`
	CreatedBy  string    `json:"created_by"`
}

// Allocation is one producer's share of the pool.
type Allocation struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	PoolID      string `json:"pool_id"`
	ValuationID string `json:"valuation_id"`
	ProducerRef string `json:"producer_ref"`

	// ComponentValue is what this producer earned at uniform component prices.
	ComponentValue money.Money `json:"component_value"`
	// FundShare is this producer's slice of the residual fund.
	FundShare money.Money `json:"fund_share"`
	// Total is ComponentValue plus FundShare, and is what becomes payable.
	Total money.Money `json:"total"`

	// Weight is the share basis, kept so the split can be checked by hand.
	Weight int64 `json:"weight"`

	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

// ProducerEconomicEvent is the payable that leaves the pool for one producer.
//
// In shadow mode it is computed and compared, never paid.
type ProducerEconomicEvent struct {
	ID           string      `json:"id"`
	TenantID     string      `json:"tenant_id"`
	PoolID       string      `json:"pool_id"`
	AllocationID string      `json:"allocation_id"`
	ProducerRef  string      `json:"producer_ref"`
	Amount       money.Money `json:"amount"`
	// Kind separates an original payable from a later adjustment raised by a
	// recovery policy, so the two never net silently.
	Kind EventKind `json:"kind"`
	// SupersedesEventID links an adjustment to what it corrects.
	SupersedesEventID string `json:"supersedes_event_id,omitempty"`

	Origin origin.Origin `json:"origin"`

	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

// EventKind distinguishes an original payable from a correction.
type EventKind string

const (
	EventOriginal EventKind = "ORIGINAL"
	// EventIncremental is the difference against a prior settled amount, raised
	// when a recovery policy says to adjust rather than restate.
	EventIncremental EventKind = "INCREMENTAL"
	// EventRestatement replaces a prior amount in full.
	EventRestatement EventKind = "RESTATEMENT"
)

// RetroactivityMode says what happens to already-settled pools when a late
// correction arrives.
type RetroactivityMode string

const (
	// RetroDoNotReopen leaves settled pools alone. The correction affects only
	// future periods.
	RetroDoNotReopen RetroactivityMode = "DO_NOT_REOPEN"
	// RetroRecalculate restates the pool in full: every producer's amount is
	// recomputed and replaces the prior one.
	RetroRecalculate RetroactivityMode = "RECALCULATE"
	// RetroApplyIncremental raises only the difference against what was already
	// settled, which is what a cooperative that has already paid needs.
	RetroApplyIncremental RetroactivityMode = "APPLY_INCREMENTAL"
	// RetroCustom defers to a tenant-specific rule and, until one is supplied,
	// escalates rather than guessing.
	RetroCustom RetroactivityMode = "CUSTOM"
)

// RecoveryRetroactivityPolicy governs how far back a correction may reach.
//
// Reopening a settled pool moves money that producers have already been paid,
// so the decision cannot be implicit. The window bounds it: a correction older
// than the window is out of reach whatever the mode says.
type RecoveryRetroactivityPolicy struct {
	ID       string            `json:"id"`
	TenantID string            `json:"tenant_id"`
	Name     string            `json:"name"`
	Mode     RetroactivityMode `json:"mode"`
	// MaxLookbackDays bounds how old a settled pool may be and still be
	// reopened. Zero means no reopening at all, whatever the mode.
	MaxLookbackDays int32 `json:"max_lookback_days"`
	// MinimumAdjustment suppresses corrections too small to be worth the
	// disruption of reissuing a payment.
	MinimumAdjustment money.Money `json:"minimum_adjustment"`

	EffectiveFrom time.Time  `json:"effective_from"`
	EffectiveTo   *time.Time `json:"effective_to,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}
