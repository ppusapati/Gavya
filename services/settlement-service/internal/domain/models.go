// Package domain holds what settlement is about: the period a society pays for,
// what each producer earned in it, what is recovered from those earnings, and
// what is left to hand over.
//
// The money is the same money as everywhere else — libs/integrity/money — and
// the earnings are copied from procurement rather than recomputed. A settlement
// that recomputed a collection could reach a different figure from the one the
// producer was shown at the collection centre, and there is no version of that
// disagreement where the platform looks right.
package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"
)

// CycleStatus is how far a period has got.
//
// A closed set, and the transitions between them are the whole point: money that
// has been handed over cannot go back to being a draft.
type CycleStatus string

const (
	// CycleOpen has been declared and is still gathering.
	CycleOpen CycleStatus = "OPEN"
	// CycleGathered holds the period's collections and the figures they came to.
	CycleGathered CycleStatus = "GATHERED"
	// CycleApproved has been signed off. The figures are now what producers have
	// been told, so nothing further may be gathered into it.
	CycleApproved CycleStatus = "APPROVED"
	// CyclePaid has had every payable settled.
	CyclePaid CycleStatus = "PAID"
	// CycleAbandoned was a mistake. Its lines are released so the collections
	// they held can be gathered into a cycle that is not.
	CycleAbandoned CycleStatus = "ABANDONED"
)

// DeductionPolicy is what happens when a producer owes more than their milk
// earned this period.
//
// There is no sensible default. A society that caps at earnings never hands a
// producer a bill; a society that does not, does — and both exist. Choosing one
// here would be choosing on their behalf, invisibly, in a way the producer finds
// out about at the window.
type DeductionPolicy string

const (
	// CapAtEarnings recovers only what the milk covers. The rest stays
	// outstanding and is recovered next period.
	CapAtEarnings DeductionPolicy = "CAP_AT_EARNINGS"
	// AllowNegative recovers the instalment in full even when it exceeds the
	// earnings, leaving the producer owing the difference.
	AllowNegative DeductionPolicy = "ALLOW_NEGATIVE"
)

func ValidPolicy(p DeductionPolicy) bool {
	return p == CapAtEarnings || p == AllowNegative
}

// Cycle is one period a society settles.
type Cycle struct {
	ID          string
	TenantID    string
	SocietyCode string
	Name        string

	// PeriodStart and PeriodEnd are both inclusive, because a fortnight is
	// spoken about as "the 1st to the 15th" and an exclusive end would put the
	// 15th's milk in the next one.
	PeriodStart time.Time
	PeriodEnd   time.Time

	Currency    string
	AmountScale int32

	Policy DeductionPolicy
	Status CycleStatus

	GatheredAt *time.Time
	ApprovedAt *time.Time
	ApprovedBy string
	PaidAt     *time.Time

	CreatedAt time.Time
	CreatedBy string
}

// Line is one collection, as it stood when the cycle gathered it.
//
// The amount is copied rather than referenced. A collection re-priced after a
// producer has been paid must not change what they were paid; if it should
// change, that is a correction with its own record, not a number that quietly
// differs from the slip in somebody's pocket.
type Line struct {
	ID       string
	TenantID string
	CycleID  string

	ProducerRef  string
	CollectionID string
	CollectedOn  time.Time
	Shift        string

	Quantity     string
	QuantityUnit string
	Rate         string

	Amount money.Money
}

// RecoveryKind is what a producer owes for.
type RecoveryKind string

const (
	Advance      RecoveryKind = "ADVANCE"
	FeedCredit   RecoveryKind = "FEED_CREDIT"
	SocietyDues  RecoveryKind = "SOCIETY_DUES"
	Loan         RecoveryKind = "LOAN"
	OtherRecover RecoveryKind = "OTHER"
)

func ValidRecoveryKind(k RecoveryKind) bool {
	switch k {
	case Advance, FeedCredit, SocietyDues, Loan, OtherRecover:
		return true
	}
	return false
}

type RecoveryStatus string

const (
	Outstanding RecoveryStatus = "OUTSTANDING"
	Settled     RecoveryStatus = "SETTLED"
	Waived      RecoveryStatus = "WAIVED"
)

// Recovery is something a producer owes the society, recovered out of milk.
type Recovery struct {
	ID          string
	TenantID    string
	ProducerRef string

	Kind      RecoveryKind
	Reference string

	// Principal is what was lent or supplied. Recovered is how much of it has
	// come back so far. The difference is what is still owed, and no arithmetic
	// anywhere may take more than that.
	Principal money.Money
	Recovered money.Money

	// Instalment is the most that may be taken in any one period. Zero means
	// there is no instalment and the whole outstanding balance is recovered as
	// soon as there is milk to recover it from — which is what society dues on a
	// single fortnight look like.
	Instalment money.Money

	// Priority orders recovery when the milk will not cover everything. Lower
	// first. There is no default: which of a producer's debts is served first
	// out of a short fortnight is a decision about money, and leaving it to the
	// order rows come back in makes it a decision nobody made.
	Priority int32

	Status   RecoveryStatus
	OpenedOn time.Time

	CreatedAt time.Time
	CreatedBy string
}

// Outstanding is what is still owed on this recovery.
func (r *Recovery) OutstandingAmount() (money.Money, error) {
	return money.Sub(r.Principal, r.Recovered)
}

// Deduction is one recovery taken from one producer in one cycle.
type Deduction struct {
	ID         string
	TenantID   string
	CycleID    string
	RecoveryID string

	ProducerRef string
	Kind        RecoveryKind
	Reference   string
	Amount      money.Money
}

// PayableStatus is where a producer's money has got to.
type PayableStatus string

const (
	// Payable has been computed and not yet signed off.
	Payable PayableStatus = "PAYABLE"
	// PayableApproved has been signed off and may be paid.
	PayableApproved PayableStatus = "APPROVED"
	// PayablePaid has been handed over. Nothing about it changes after this.
	PayablePaid PayableStatus = "PAID"
	// PayableHeld is deliberately not being paid — a dispute, a death, a
	// membership under question. Held rather than deleted, because the earnings
	// still exist and somebody is eventually owed them.
	PayableHeld PayableStatus = "HELD"
)

// ProducerPayable is what one producer takes home from one cycle.
type ProducerPayable struct {
	ID          string
	TenantID    string
	CycleID     string
	ProducerRef string

	// Gross is the milk. Deducted is what was recovered. Net is the difference,
	// and the three are stored together so a statement does not have to be
	// trusted to subtract.
	Gross    money.Money
	Deducted money.Money
	Net      money.Money

	// CarriedForward is what the period could not recover under a policy that
	// caps at earnings. It is not a number the producer owes twice — it is
	// already part of the outstanding balance on the recovery — it is here so a
	// statement can say why a debt did not go down as much as expected.
	CarriedForward money.Money

	Status PayableStatus

	ApprovedAt       *time.Time
	ApprovedBy       string
	PaidAt           *time.Time
	PaidBy           string
	PaymentReference string
	HeldReason       string

	CreatedAt time.Time
}

// Statement is what a producer is handed: the milk, the deductions, the number.
type Statement struct {
	TenantID    string
	ProducerRef string
	Cycle       *Cycle

	Lines      []*Line
	Deductions []*Deduction
	Payable    *ProducerPayable

	// LitresOrKg is the period's quantity, kept per unit rather than summed
	// across them. A society that records some collections in litres and some in
	// kilograms has a data problem, and adding the two would hide it behind a
	// number that looks fine.
	Quantities map[string]string
}

var (
	ErrNoSociety     = errors.New("a cycle must say which society it settles")
	ErrNoPeriod      = errors.New("a cycle must say what period it covers")
	ErrBackwards     = errors.New("the period ends before it starts")
	ErrNoPolicy      = errors.New("a cycle must say what happens when a producer owes more than their milk earned: CAP_AT_EARNINGS or ALLOW_NEGATIVE")
	ErrNoProducer    = errors.New("a recovery with no producer cannot be recovered from anybody")
	ErrNoPrincipal   = errors.New("a recovery of nothing is not a recovery")
	ErrNoPriority    = errors.New("a recovery must say where it comes in the order; which debt is served first out of a short fortnight is a decision about money")
	ErrOverRecovered = errors.New("this would recover more than is owed")
)

// ErrWrongStatus names a transition that was refused and why.
type ErrWrongStatus struct {
	What   string
	ID     string
	Is     string
	Wanted string
	Action string
}

func (e *ErrWrongStatus) Error() string {
	return fmt.Sprintf("cannot %s %s %s: it is %s, and only one that is %s can be",
		e.Action, e.What, e.ID, e.Is, e.Wanted)
}

// Validate refuses a cycle that cannot settle anything.
func (c *Cycle) Validate() error {
	switch {
	case c.TenantID == "":
		return errors.New("tenant_id is required")
	case c.SocietyCode == "":
		return ErrNoSociety
	case c.PeriodStart.IsZero() || c.PeriodEnd.IsZero():
		return ErrNoPeriod
	case c.PeriodEnd.Before(c.PeriodStart):
		return ErrBackwards
	case !ValidPolicy(c.Policy):
		return ErrNoPolicy
	case c.Currency == "":
		return errors.New("a cycle must say what currency it settles in")
	case c.CreatedBy == "":
		return errors.New("actor is required")
	}
	return nil
}

// Validate refuses a recovery that cannot be recovered.
func (r *Recovery) Validate() error {
	switch {
	case r.TenantID == "":
		return errors.New("tenant_id is required")
	case r.ProducerRef == "":
		return ErrNoProducer
	case !ValidRecoveryKind(r.Kind):
		return errors.New("a recovery must say what it is for: ADVANCE, FEED_CREDIT, SOCIETY_DUES, LOAN or OTHER")
	case r.Principal.Value <= 0:
		return ErrNoPrincipal
	case r.Recovered.Value < 0:
		return errors.New("a negative amount recovered is not a thing that can have happened")
	case r.Instalment.Value < 0:
		return errors.New("a negative instalment would pay the producer out of their own debt")
	case r.Priority <= 0:
		return ErrNoPriority
	case r.OpenedOn.IsZero():
		return errors.New("a recovery must say when it was opened; two debts at the same priority are served oldest first")
	case r.CreatedBy == "":
		return errors.New("actor is required")
	}
	if r.Recovered.Value > r.Principal.Value {
		return ErrOverRecovered
	}
	return nil
}

// Day is the date something belongs to, with the time discarded.
func Day(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
