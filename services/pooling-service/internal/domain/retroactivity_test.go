package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"
)

var (
	periodEnd = time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)
	// Thirty days after the pool closed.
	soon = periodEnd.AddDate(0, 0, 30)
	// Two years after the pool closed.
	late = periodEnd.AddDate(2, 0, 0)
)

func inr(t *testing.T, s string) money.Money {
	t.Helper()
	m, err := money.Parse(s, 2, "INR")
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return m
}

func retroPolicy(t *testing.T, mode RetroactivityMode, lookbackDays int32, floor string) *RecoveryRetroactivityPolicy {
	t.Helper()
	return &RecoveryRetroactivityPolicy{
		ID:                "pol-1",
		TenantID:          "tnt",
		Name:              "default",
		Mode:              mode,
		MaxLookbackDays:   lookbackDays,
		MinimumAdjustment: inr(t, floor),
		EffectiveFrom:     time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func settledPool() *Pool {
	return &Pool{
		ID:        "pool-1",
		TenantID:  "tnt",
		Status:    PoolSettled,
		PeriodEnd: periodEnd,
	}
}

func TestUnsettledPoolNeedsNoRetroactivityRuling(t *testing.T) {
	pool := settledPool()
	pool.Status = PoolValued

	d, err := DecideRetroactivity(retroPolicy(t, RetroDoNotReopen, 0, "0.00"), pool, inr(t, "500.00"), soon)
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != RetroRestate {
		t.Fatalf("got %s, want RESTATE: %s", d.Outcome, d.Reason)
	}
	if d.EventKind != EventOriginal {
		t.Errorf("event kind = %s, want ORIGINAL", d.EventKind)
	}
}

func TestDoNotReopenAppliesForward(t *testing.T) {
	d, err := DecideRetroactivity(retroPolicy(t, RetroDoNotReopen, 90, "0.00"), settledPool(), inr(t, "500.00"), soon)
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != RetroApplyForward {
		t.Fatalf("got %s, want APPLY_FORWARD: %s", d.Outcome, d.Reason)
	}
}

func TestRecalculateRestatesThePool(t *testing.T) {
	d, err := DecideRetroactivity(retroPolicy(t, RetroRecalculate, 90, "0.00"), settledPool(), inr(t, "500.00"), soon)
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != RetroRestate {
		t.Fatalf("got %s, want RESTATE: %s", d.Outcome, d.Reason)
	}
	if d.EventKind != EventRestatement {
		t.Errorf("event kind = %s, want RESTATEMENT", d.EventKind)
	}
}

func TestApplyIncrementalRaisesTheDifference(t *testing.T) {
	d, err := DecideRetroactivity(retroPolicy(t, RetroApplyIncremental, 90, "0.00"), settledPool(), inr(t, "500.00"), soon)
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != RetroAdjust {
		t.Fatalf("got %s, want ADJUST: %s", d.Outcome, d.Reason)
	}
	if d.EventKind != EventIncremental {
		t.Errorf("event kind = %s, want INCREMENTAL", d.EventKind)
	}
}

// Guessing which of the other modes a custom rule meant would move real money
// on an assumption.
func TestCustomModeEscalatesRatherThanGuessing(t *testing.T) {
	d, err := DecideRetroactivity(retroPolicy(t, RetroCustom, 90, "0.00"), settledPool(), inr(t, "500.00"), soon)
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != RetroEscalate {
		t.Fatalf("got %s, want ESCALATE: %s", d.Outcome, d.Reason)
	}
	if d.EventKind != "" {
		t.Errorf("an escalation named an event kind %q", d.EventKind)
	}
}

// The lookback bounds every mode, including the ones that would otherwise
// reopen freely.
func TestLookbackWindowBoundsEveryMode(t *testing.T) {
	for _, mode := range []RetroactivityMode{RetroRecalculate, RetroApplyIncremental, RetroCustom} {
		d, err := DecideRetroactivity(retroPolicy(t, mode, 90, "0.00"), settledPool(), inr(t, "500.00"), late)
		if err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if d.Outcome != RetroApplyForward {
			t.Errorf("%s: a two year old pool was reopened under a 90 day window: %s", mode, d.Reason)
		}
	}
}

func TestZeroLookbackClosesSettledPoolsForGood(t *testing.T) {
	d, err := DecideRetroactivity(retroPolicy(t, RetroRecalculate, 0, "0.00"), settledPool(), inr(t, "500.00"), soon)
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != RetroApplyForward {
		t.Fatalf("got %s, want APPLY_FORWARD: %s", d.Outcome, d.Reason)
	}
}

func TestImmaterialAdjustmentIsSuppressed(t *testing.T) {
	d, err := DecideRetroactivity(retroPolicy(t, RetroApplyIncremental, 90, "50.00"), settledPool(), inr(t, "1.00"), soon)
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != RetroSuppress {
		t.Fatalf("got %s, want SUPPRESS: %s", d.Outcome, d.Reason)
	}
}

func TestMaterialAdjustmentPassesTheFloor(t *testing.T) {
	d, err := DecideRetroactivity(retroPolicy(t, RetroApplyIncremental, 90, "50.00"), settledPool(), inr(t, "500.00"), soon)
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != RetroAdjust {
		t.Fatalf("got %s, want ADJUST: %s", d.Outcome, d.Reason)
	}
}

// A negative correction of the same magnitude is equally material.
func TestMaterialityUsesMagnitudeNotSign(t *testing.T) {
	policy := retroPolicy(t, RetroApplyIncremental, 90, "50.00")

	small, err := DecideRetroactivity(policy, settledPool(), inr(t, "-1.00"), soon)
	if err != nil {
		t.Fatal(err)
	}
	if small.Outcome != RetroSuppress {
		t.Errorf("a small negative correction was not suppressed: %s", small.Outcome)
	}

	large, err := DecideRetroactivity(policy, settledPool(), inr(t, "-500.00"), soon)
	if err != nil {
		t.Fatal(err)
	}
	if large.Outcome != RetroAdjust {
		t.Errorf("a large negative correction was suppressed: %s", large.Outcome)
	}
}

// A correction out of reach is out of reach whatever its size; reporting it as
// merely immaterial would misdescribe why it was not applied.
func TestWindowIsCheckedBeforeMateriality(t *testing.T) {
	d, err := DecideRetroactivity(retroPolicy(t, RetroApplyIncremental, 90, "50.00"), settledPool(), inr(t, "1.00"), late)
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != RetroApplyForward {
		t.Fatalf("got %s, want APPLY_FORWARD", d.Outcome)
	}
}

func TestZeroAdjustmentIsSuppressed(t *testing.T) {
	d, err := DecideRetroactivity(retroPolicy(t, RetroRecalculate, 90, "0.00"), settledPool(), inr(t, "0.00"), soon)
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != RetroSuppress {
		t.Fatalf("got %s, want SUPPRESS: %s", d.Outcome, d.Reason)
	}
}

func TestReopenedPoolIsStillGovernedByThePolicy(t *testing.T) {
	pool := settledPool()
	pool.Status = PoolReopened

	d, err := DecideRetroactivity(retroPolicy(t, RetroDoNotReopen, 90, "0.00"), pool, inr(t, "500.00"), soon)
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != RetroApplyForward {
		t.Fatalf("a reopened pool escaped the policy: got %s", d.Outcome)
	}
}

func TestMissingPolicyIsAnError(t *testing.T) {
	if _, err := DecideRetroactivity(nil, settledPool(), inr(t, "1.00"), soon); !errors.Is(err, ErrNoRetroactivityPolicy) {
		t.Errorf("got %v, want ErrNoRetroactivityPolicy", err)
	}
}

func TestPolicyOutsideItsWindowIsAnError(t *testing.T) {
	policy := retroPolicy(t, RetroRecalculate, 90, "0.00")
	policy.EffectiveFrom = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

	if _, err := DecideRetroactivity(policy, settledPool(), inr(t, "500.00"), soon); err == nil {
		t.Error("a policy not yet in force was applied")
	}
}

func TestUnknownModeIsAnError(t *testing.T) {
	policy := retroPolicy(t, "WHENEVER", 90, "0.00")
	if _, err := DecideRetroactivity(policy, settledPool(), inr(t, "500.00"), soon); err == nil {
		t.Error("an unrecognised retroactivity mode was applied")
	}
}

func TestEveryDecisionCarriesAReason(t *testing.T) {
	modes := []RetroactivityMode{RetroDoNotReopen, RetroRecalculate, RetroApplyIncremental, RetroCustom}
	for _, mode := range modes {
		for _, at := range []time.Time{soon, late} {
			d, err := DecideRetroactivity(retroPolicy(t, mode, 90, "50.00"), settledPool(), inr(t, "500.00"), at)
			if err != nil {
				t.Fatalf("%s: %v", mode, err)
			}
			if d.Reason == "" {
				t.Errorf("%s at %s: outcome %s carries no reason", mode, at, d.Outcome)
			}
		}
	}
}
