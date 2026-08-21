package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/origin"
)

func policy(mode ResolutionMode, dims ...IdentityDimension) *CollectionIdentityPolicy {
	if len(dims) == 0 {
		dims = []IdentityDimension{DimProducer, DimCollectionDate, DimShift}
	}
	return &CollectionIdentityPolicy{
		ID:         "pol-1",
		TenantID:   "tnt",
		Name:       "default",
		Dimensions: dims,
		Resolution: mode,
		Version:    1,
	}
}

func values() map[IdentityDimension]string {
	return map[IdentityDimension]string{
		DimProducer:       "P-001",
		DimCollectionDate: "2026-02-14",
		DimShift:          "MORNING",
	}
}

func claim(ref string, at time.Time, quality int32) Claim {
	return Claim{
		SourceRef:  ref,
		Values:     values(),
		Origin:     origin.Native,
		RecordedAt: at,
		Quality:    quality,
	}
}

func settledSlot(ref string, at time.Time, quality int32) *AuthoritativeCollectionSlot {
	return &AuthoritativeCollectionSlot{
		ID:                  "slot-1",
		TenantID:            "tnt",
		SlotKey:             "slot:abc",
		OriginKind:          origin.Native,
		PolicyID:            "pol-1",
		PolicyVersion:       1,
		AuthoritativeRef:    ref,
		Status:              SlotSettled,
		IncumbentRecordedAt: at,
		IncumbentQuality:    quality,
		Values:              values(),
	}
}

var (
	t1 = time.Date(2026, 2, 14, 6, 0, 0, 0, time.UTC)
	t2 = time.Date(2026, 2, 14, 8, 0, 0, 0, time.UTC)
)

func TestValidatePolicy(t *testing.T) {
	cases := []struct {
		name    string
		policy  *CollectionIdentityPolicy
		wantErr error
	}{
		{"valid", policy(ResolveFirstWins), nil},
		{"no dimensions", &CollectionIdentityPolicy{Resolution: ResolveFirstWins}, ErrNoDimensions},
		{
			"unknown dimension",
			&CollectionIdentityPolicy{Dimensions: []IdentityDimension{DimProducer, "WEATHER"}, Resolution: ResolveFirstWins},
			ErrUnknownDim,
		},
		{
			"duplicate dimension",
			&CollectionIdentityPolicy{Dimensions: []IdentityDimension{DimProducer, DimProducer}, Resolution: ResolveFirstWins},
			ErrDuplicateDim,
		},
		{
			"unknown mode",
			&CollectionIdentityPolicy{Dimensions: []IdentityDimension{DimProducer}, Resolution: "COIN_FLIP"},
			ErrUnknownMode,
		},
	}
	for _, c := range cases {
		err := ValidatePolicy(c.policy)
		if c.wantErr == nil {
			if err != nil {
				t.Errorf("%s: unexpected error %v", c.name, err)
			}
			continue
		}
		if !errors.Is(err, c.wantErr) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.wantErr)
		}
	}
}

func TestPolicyMustIdentifyAProducer(t *testing.T) {
	p := &CollectionIdentityPolicy{
		Dimensions: []IdentityDimension{DimCollectionDate, DimShift},
		Resolution: ResolveFirstWins,
	}
	if err := ValidatePolicy(p); err == nil {
		t.Fatal("a policy with no producer dimension was accepted")
	}
}

func TestSlotKeyIsStableAndDimensionSensitive(t *testing.T) {
	p := policy(ResolveFirstWins)
	c := claim("obs-1", t1, 0)

	first, err := SlotKey(p, c)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		again, err := SlotKey(p, c)
		if err != nil {
			t.Fatal(err)
		}
		if again != first {
			t.Fatalf("slot key is not stable: %s vs %s", first, again)
		}
	}

	// A different shift is a different collection.
	other := c
	other.Values = values()
	other.Values[DimShift] = "EVENING"
	evening, err := SlotKey(p, other)
	if err != nil {
		t.Fatal(err)
	}
	if evening == first {
		t.Error("morning and evening collections share a slot key")
	}
}

// A source system sending "P-001" one day and " p-001 " the next means the same
// producer; treating them as different slots would pay that producer twice.
func TestSlotKeyNormalisesValues(t *testing.T) {
	p := policy(ResolveFirstWins)

	clean := claim("obs-1", t1, 0)
	messy := claim("obs-2", t1, 0)
	messy.Values = map[IdentityDimension]string{
		DimProducer:       "  p-001 ",
		DimCollectionDate: "2026-02-14",
		DimShift:          "morning",
	}

	a, err := SlotKey(p, clean)
	if err != nil {
		t.Fatal(err)
	}
	b, err := SlotKey(p, messy)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("normalisation failed: %s vs %s", a, b)
	}
}

func TestSlotKeyChangesWithPolicyVersion(t *testing.T) {
	c := claim("obs-1", t1, 0)

	v1 := policy(ResolveFirstWins)
	v2 := policy(ResolveFirstWins)
	v2.Version = 2

	a, err := SlotKey(v1, c)
	if err != nil {
		t.Fatal(err)
	}
	b, err := SlotKey(v2, c)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("a policy version change did not change the slot key")
	}
}

func TestSlotKeyRequiresEveryDimension(t *testing.T) {
	p := policy(ResolveFirstWins)
	c := claim("obs-1", t1, 0)
	delete(c.Values, DimShift)

	if _, err := SlotKey(p, c); !errors.Is(err, ErrMissingValue) {
		t.Fatalf("got %v, want ErrMissingValue", err)
	}
}

func TestPlaceClaimEstablishesAnEmptySlot(t *testing.T) {
	d, err := PlaceClaim(policy(ResolveFirstWins), nil, claim("obs-1", t1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != OutcomeEstablished {
		t.Fatalf("got %s, want SLOT_ESTABLISHED", d.Outcome)
	}
	if d.Authoritative != "obs-1" {
		t.Errorf("authoritative = %q, want obs-1", d.Authoritative)
	}
}

func TestPlaceClaimIsIdempotentForTheHoldingClaim(t *testing.T) {
	p := policy(ResolveFirstWins)
	slot := settledSlot("obs-1", t1, 0)

	for i := 0; i < 20; i++ {
		d, err := PlaceClaim(p, slot, claim("obs-1", t1, 0))
		if err != nil {
			t.Fatal(err)
		}
		if d.Outcome != OutcomeReasserted {
			t.Fatalf("iteration %d: got %s, want SLOT_REASSERTED", i, d.Outcome)
		}
		if d.Authoritative != "obs-1" {
			t.Fatalf("iteration %d: authoritative changed to %q", i, d.Authoritative)
		}
	}
}

func TestFirstWinsKeepsTheEarlierClaim(t *testing.T) {
	p := policy(ResolveFirstWins)
	slot := settledSlot("obs-early", t1, 0)

	d, err := PlaceClaim(p, slot, claim("obs-late", t2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != OutcomeRetained {
		t.Fatalf("got %s, want SLOT_RETAINED: %s", d.Outcome, d.Reason)
	}
	if d.Authoritative != "obs-early" {
		t.Errorf("authoritative = %q, want obs-early", d.Authoritative)
	}
	if d.Demoted == nil || d.Demoted.SourceRef != "obs-late" {
		t.Error("the losing claim was not recorded as a contender")
	}
}

// An earlier claim arriving after a later one still wins under FIRST_WINS: the
// outcome must depend on the recording time, not on arrival order.
func TestFirstWinsReplacesWhenAnEarlierClaimArrivesLate(t *testing.T) {
	p := policy(ResolveFirstWins)
	slot := settledSlot("obs-late", t2, 0)

	d, err := PlaceClaim(p, slot, claim("obs-early", t1, 0))
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != OutcomeReplaced {
		t.Fatalf("got %s, want SLOT_REPLACED: %s", d.Outcome, d.Reason)
	}
	if d.Authoritative != "obs-early" {
		t.Errorf("authoritative = %q, want obs-early", d.Authoritative)
	}
	if d.Demoted == nil || d.Demoted.SourceRef != "obs-late" {
		t.Error("the displaced incumbent was not recorded as a contender")
	}
}

func TestLastWinsPrefersTheLaterClaim(t *testing.T) {
	p := policy(ResolveLastWins)
	slot := settledSlot("obs-early", t1, 0)

	d, err := PlaceClaim(p, slot, claim("obs-late", t2, 0))
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != OutcomeReplaced {
		t.Fatalf("got %s, want SLOT_REPLACED: %s", d.Outcome, d.Reason)
	}
	if d.Authoritative != "obs-late" {
		t.Errorf("authoritative = %q, want obs-late", d.Authoritative)
	}
}

// Guessing between simultaneous claims would make the answer depend on arrival
// order, which is exactly the non-determinism this design removes.
func TestSimultaneousClaimsConflictRatherThanGuess(t *testing.T) {
	for _, mode := range []ResolutionMode{ResolveFirstWins, ResolveLastWins} {
		slot := settledSlot("obs-1", t1, 0)
		d, err := PlaceClaim(policy(mode), slot, claim("obs-2", t1, 0))
		if err != nil {
			t.Fatal(err)
		}
		if d.Outcome != OutcomeConflict {
			t.Errorf("%s: got %s, want COLLECTION_SLOT_CONFLICT", mode, d.Outcome)
		}
		if d.Authoritative != "" {
			t.Errorf("%s: a conflicted slot named %q as authoritative", mode, d.Authoritative)
		}
	}
}

func TestHighestQualityPrefersTheBetterEvidencedClaim(t *testing.T) {
	p := policy(ResolveHighestQuality)
	slot := settledSlot("obs-manual", t1, 10)

	d, err := PlaceClaim(p, slot, claim("obs-verified", t2, 90))
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != OutcomeReplaced {
		t.Fatalf("got %s, want SLOT_REPLACED: %s", d.Outcome, d.Reason)
	}
	if d.Authoritative != "obs-verified" {
		t.Errorf("authoritative = %q, want obs-verified", d.Authoritative)
	}
}

func TestHighestQualityKeepsTheIncumbentWhenTheClaimIsWorse(t *testing.T) {
	p := policy(ResolveHighestQuality)
	slot := settledSlot("obs-verified", t1, 90)

	d, err := PlaceClaim(p, slot, claim("obs-manual", t2, 10))
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != OutcomeRetained {
		t.Fatalf("got %s, want SLOT_RETAINED: %s", d.Outcome, d.Reason)
	}
	if d.Authoritative != "obs-verified" {
		t.Errorf("authoritative = %q, want obs-verified", d.Authoritative)
	}
}

// Equal quality is not a tie to break arbitrarily; it means the policy has no
// basis to choose.
func TestEqualQualityConflicts(t *testing.T) {
	p := policy(ResolveHighestQuality)
	slot := settledSlot("obs-1", t1, 50)

	d, err := PlaceClaim(p, slot, claim("obs-2", t2, 50))
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != OutcomeConflict {
		t.Fatalf("got %s, want COLLECTION_SLOT_CONFLICT: %s", d.Outcome, d.Reason)
	}
}

func TestManualModeAlwaysConflicts(t *testing.T) {
	p := policy(ResolveManual)
	slot := settledSlot("obs-1", t1, 10)

	d, err := PlaceClaim(p, slot, claim("obs-2", t2, 90))
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != OutcomeConflict {
		t.Fatalf("got %s, want COLLECTION_SLOT_CONFLICT", d.Outcome)
	}
	if d.Demoted == nil {
		t.Error("the competing claim was not preserved as a contender")
	}
}

// Letting a third claim silently settle a conflicted slot would discard the
// disagreement that put it there.
func TestAConflictedSlotStaysConflicted(t *testing.T) {
	p := policy(ResolveLastWins)
	slot := settledSlot("obs-1", t1, 0)
	slot.Status = SlotConflict
	slot.AuthoritativeRef = ""

	d, err := PlaceClaim(p, slot, claim("obs-3", t2.Add(time.Hour), 0))
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != OutcomeConflict {
		t.Fatalf("got %s, want COLLECTION_SLOT_CONFLICT", d.Outcome)
	}
	if d.Authoritative != "" {
		t.Errorf("a conflicted slot named %q as authoritative", d.Authoritative)
	}
}

func TestAKnownContenderDoesNotReargue(t *testing.T) {
	p := policy(ResolveFirstWins)
	slot := settledSlot("obs-1", t1, 0)
	slot.Contenders = []Contender{{SourceRef: "obs-2", Reason: "already set aside"}}

	d, err := PlaceClaim(p, slot, claim("obs-2", t1.Add(-time.Hour), 0))
	if err != nil {
		t.Fatal(err)
	}
	if d.Outcome != OutcomeRetained {
		t.Fatalf("got %s, want SLOT_RETAINED", d.Outcome)
	}
	if d.Demoted != nil {
		t.Error("a known contender was recorded a second time")
	}
}

func TestPlaceClaimRejectsAnIncompleteClaim(t *testing.T) {
	p := policy(ResolveFirstWins)

	noRef := claim("", t1, 0)
	if _, err := PlaceClaim(p, nil, noRef); err == nil {
		t.Error("a claim with no source reference was accepted")
	}

	noOrigin := claim("obs-1", t1, 0)
	noOrigin.Origin = ""
	if _, err := PlaceClaim(p, nil, noOrigin); !errors.Is(err, ErrNoOriginOnClaim) {
		t.Errorf("got %v, want ErrNoOriginOnClaim", err)
	}

	missingDim := claim("obs-1", t1, 0)
	delete(missingDim.Values, DimShift)
	if _, err := PlaceClaim(p, nil, missingDim); !errors.Is(err, ErrMissingValue) {
		t.Errorf("got %v, want ErrMissingValue", err)
	}
}

func TestPlaceClaimIsDeterministic(t *testing.T) {
	p := policy(ResolveHighestQuality)
	slot := settledSlot("obs-1", t1, 50)
	c := claim("obs-2", t2, 50)

	first, err := PlaceClaim(p, slot, c)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		again, err := PlaceClaim(p, slot, c)
		if err != nil {
			t.Fatal(err)
		}
		if again.Outcome != first.Outcome || again.Reason != first.Reason {
			t.Fatalf("decision is not deterministic:\n first: %s / %s\n again: %s / %s",
				first.Outcome, first.Reason, again.Outcome, again.Reason)
		}
	}
}

func TestEveryOutcomeCarriesAReason(t *testing.T) {
	cases := []struct {
		name string
		p    *CollectionIdentityPolicy
		slot *AuthoritativeCollectionSlot
		c    Claim
	}{
		{"established", policy(ResolveFirstWins), nil, claim("obs-1", t1, 0)},
		{"reasserted", policy(ResolveFirstWins), settledSlot("obs-1", t1, 0), claim("obs-1", t1, 0)},
		{"retained", policy(ResolveFirstWins), settledSlot("obs-1", t1, 0), claim("obs-2", t2, 0)},
		{"replaced", policy(ResolveLastWins), settledSlot("obs-1", t1, 0), claim("obs-2", t2, 0)},
		{"conflict", policy(ResolveManual), settledSlot("obs-1", t1, 0), claim("obs-2", t2, 0)},
	}
	for _, c := range cases {
		d, err := PlaceClaim(c.p, c.slot, c.c)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if d.Reason == "" {
			t.Errorf("%s: outcome %s carries no reason a reviewer could read", c.name, d.Outcome)
		}
		if d.Outcome == OutcomeConflict && d.Authoritative != "" {
			t.Errorf("%s: a conflict named an authoritative claim", c.name)
		}
	}
}

func TestPolicyEffectiveWindow(t *testing.T) {
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	p := policy(ResolveFirstWins)
	p.EffectiveFrom = from
	p.EffectiveTo = &to

	if p.IsEffectiveAt(from.Add(-time.Second)) {
		t.Error("policy was effective before it began")
	}
	if !p.IsEffectiveAt(from) {
		t.Error("policy was not effective at its own start instant")
	}
	if !p.IsEffectiveAt(t1) {
		t.Error("policy was not effective mid-window")
	}
	if p.IsEffectiveAt(to) {
		t.Error("policy was still effective at its end instant; the window is half-open")
	}

	open := policy(ResolveFirstWins)
	open.EffectiveFrom = from
	if !open.IsEffectiveAt(to.AddDate(10, 0, 0)) {
		t.Error("an open-ended policy expired")
	}
}
