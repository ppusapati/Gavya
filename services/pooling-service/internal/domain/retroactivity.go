package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"
)

// RetroactivityOutcome is what a correction is permitted to do to a pool.
type RetroactivityOutcome string

const (
	// RetroApplyForward leaves the settled pool alone; the correction lands in
	// a future period.
	RetroApplyForward RetroactivityOutcome = "APPLY_FORWARD"
	// RetroRestate recomputes the pool and replaces every producer's amount.
	RetroRestate RetroactivityOutcome = "RESTATE"
	// RetroAdjust raises only the difference against what was already settled.
	RetroAdjust RetroactivityOutcome = "ADJUST"
	// RetroSuppress drops the correction as too small to be worth reissuing a
	// payment for.
	RetroSuppress RetroactivityOutcome = "SUPPRESS"
	// RetroEscalate refuses to decide and sends the correction to a human.
	RetroEscalate RetroactivityOutcome = "ESCALATE"
)

// RetroactivityDecision is the ruling on one correction.
type RetroactivityDecision struct {
	Outcome RetroactivityOutcome
	Reason  string
	// EventKind is the kind of producer economic event to raise, when the
	// outcome raises one.
	EventKind EventKind
}

var ErrNoRetroactivityPolicy = errors.New("no recovery retroactivity policy in force")

// DecideRetroactivity rules on whether a late correction may reach a settled
// pool.
//
// Reopening a settled pool moves money producers have already been paid, so
// nothing here is implicit and nothing is guessed. Every path that cannot be
// decided from the policy escalates instead: a wrong automatic answer here
// produces a payment nobody authorised.
func DecideRetroactivity(
	policy *RecoveryRetroactivityPolicy,
	pool *Pool,
	adjustment money.Money,
	at time.Time,
) (RetroactivityDecision, error) {
	if policy == nil {
		return RetroactivityDecision{}, ErrNoRetroactivityPolicy
	}
	if pool == nil {
		return RetroactivityDecision{}, errors.New("no pool to correct")
	}
	if !policy.IsEffectiveAt(at) {
		return RetroactivityDecision{}, fmt.Errorf(
			"policy %q is not in force at %s", policy.Name, at.UTC().Format(time.RFC3339))
	}

	// A pool that was never settled has paid nobody, so a correction simply
	// changes an unfinished computation and needs no retroactivity ruling.
	if pool.Status != PoolSettled && pool.Status != PoolReopened {
		return RetroactivityDecision{
			Outcome:   RetroRestate,
			Reason:    fmt.Sprintf("pool is %s and has settled nothing, so the correction applies directly", pool.Status),
			EventKind: EventOriginal,
		}, nil
	}

	if adjustment.IsZero() {
		return RetroactivityDecision{
			Outcome: RetroSuppress,
			Reason:  "the correction changes no amount",
		}, nil
	}

	// The lookback bounds every mode. A zero window means settled pools are
	// closed for good, whatever the mode would otherwise permit.
	if policy.MaxLookbackDays <= 0 {
		return RetroactivityDecision{
			Outcome: RetroApplyForward,
			Reason:  "policy permits no reopening of settled pools",
		}, nil
	}
	age := at.Sub(pool.PeriodEnd)
	limit := time.Duration(policy.MaxLookbackDays) * 24 * time.Hour
	if age > limit {
		return RetroactivityDecision{
			Outcome: RetroApplyForward,
			Reason: fmt.Sprintf("pool closed %d days ago, beyond the %d day lookback",
				int(age.Hours()/24), policy.MaxLookbackDays),
		}, nil
	}

	// Materiality is checked after the window, not before: a correction that is
	// out of reach is out of reach whatever its size, and reporting it as merely
	// immaterial would misdescribe why it was not applied.
	if !policy.MinimumAdjustment.IsZero() {
		cmp, err := money.Cmp(adjustment.Abs(), policy.MinimumAdjustment.Abs())
		if err != nil {
			return RetroactivityDecision{}, fmt.Errorf("compare against the materiality floor: %w", err)
		}
		if cmp < 0 {
			return RetroactivityDecision{
				Outcome: RetroSuppress,
				Reason: fmt.Sprintf("adjustment of %s is below the %s materiality floor",
					adjustment.Abs(), policy.MinimumAdjustment.Abs()),
			}, nil
		}
	}

	switch policy.Mode {
	case RetroDoNotReopen:
		return RetroactivityDecision{
			Outcome: RetroApplyForward,
			Reason:  "policy does not reopen settled pools",
		}, nil

	case RetroRecalculate:
		return RetroactivityDecision{
			Outcome:   RetroRestate,
			Reason:    "policy recalculates the pool in full",
			EventKind: EventRestatement,
		}, nil

	case RetroApplyIncremental:
		return RetroactivityDecision{
			Outcome:   RetroAdjust,
			Reason:    fmt.Sprintf("policy raises the %s difference against the settled amount", adjustment),
			EventKind: EventIncremental,
		}, nil

	case RetroCustom:
		// A tenant-specific rule the platform does not implement. Guessing
		// which of the other three modes was meant would move real money on an
		// assumption.
		return RetroactivityDecision{
			Outcome: RetroEscalate,
			Reason:  fmt.Sprintf("policy %q defers to a custom rule that must be applied by a person", policy.Name),
		}, nil

	default:
		return RetroactivityDecision{}, fmt.Errorf("retroactivity mode %q is not recognised", policy.Mode)
	}
}

func (p *RecoveryRetroactivityPolicy) IsEffectiveAt(t time.Time) bool {
	if t.Before(p.EffectiveFrom) {
		return false
	}
	return p.EffectiveTo == nil || t.Before(*p.EffectiveTo)
}
