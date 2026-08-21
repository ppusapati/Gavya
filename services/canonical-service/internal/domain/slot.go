package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ClaimOutcome is what happened when a claim was placed against a slot.
type ClaimOutcome string

const (
	// OutcomeEstablished: the slot was empty and this claim now holds it.
	OutcomeEstablished ClaimOutcome = "SLOT_ESTABLISHED"
	// OutcomeReasserted: the same claim was placed again. Idempotent.
	OutcomeReasserted ClaimOutcome = "SLOT_REASSERTED"
	// OutcomeRetained: a competing claim arrived and the policy kept the
	// incumbent. The newcomer is recorded as a contender.
	OutcomeRetained ClaimOutcome = "SLOT_RETAINED"
	// OutcomeReplaced: a competing claim arrived and the policy preferred it.
	OutcomeReplaced ClaimOutcome = "SLOT_REPLACED"
	// OutcomeConflict: two claims and no policy basis to choose. The slot has
	// no authoritative answer until a human resolves it.
	OutcomeConflict ClaimOutcome = "COLLECTION_SLOT_CONFLICT"
)

// ClaimDecision is the result of placing a claim.
type ClaimDecision struct {
	Outcome ClaimOutcome
	// Authoritative is the claim that holds the slot after this decision. It is
	// empty when the outcome is a conflict.
	Authoritative string
	Reason        string
	// Demoted is the claim that lost, if one did, ready to be recorded as a
	// contender.
	Demoted *Contender
}

var (
	ErrNoDimensions   = errors.New("policy declares no identity dimensions")
	ErrMissingValue   = errors.New("claim is missing a dimension the policy requires")
	ErrUnknownDim     = errors.New("policy declares an unrecognised dimension")
	ErrDuplicateDim   = errors.New("policy declares a dimension twice")
	ErrUnknownMode    = errors.New("policy declares an unrecognised resolution mode")
	ErrNoOriginOnClaim = errors.New("claim carries no record origin")
)

// ValidatePolicy checks a policy is usable before it is stored, because an
// unusable policy would only fail later, at the point where a collection is
// being placed and cannot be.
func ValidatePolicy(p *CollectionIdentityPolicy) error {
	if len(p.Dimensions) == 0 {
		return ErrNoDimensions
	}
	seen := make(map[IdentityDimension]bool, len(p.Dimensions))
	for _, d := range p.Dimensions {
		if !validDimension(d) {
			return fmt.Errorf("%w: %q", ErrUnknownDim, d)
		}
		if seen[d] {
			return fmt.Errorf("%w: %q", ErrDuplicateDim, d)
		}
		seen[d] = true
	}
	if !validResolution(p.Resolution) {
		return fmt.Errorf("%w: %q", ErrUnknownMode, p.Resolution)
	}
	// A policy that does not identify a producer cannot identify a collection:
	// every other dimension is a qualifier on whose milk it was.
	if !seen[DimProducer] {
		return errors.New("policy must include the PRODUCER dimension")
	}
	return nil
}

// SlotKey derives the deterministic key a claim falls under.
//
// The key must be stable across processes and releases, so it is a hash of the
// policy's dimensions in their declared order paired with the claim's values.
// Values are normalised to lower case and trimmed: a source system that sends
// "P-001" one day and " p-001 " the next means the same producer, and treating
// them as different slots would pay that producer twice.
func SlotKey(p *CollectionIdentityPolicy, c Claim) (string, error) {
	if err := ValidatePolicy(p); err != nil {
		return "", err
	}
	h := sha256.New()
	fmt.Fprintf(h, "v%d\x1e", p.Version)
	for _, d := range p.Dimensions {
		v := normalise(c.Values[d])
		if v == "" {
			return "", fmt.Errorf("%w: %s", ErrMissingValue, d)
		}
		h.Write([]byte(d))
		h.Write([]byte{0x1f})
		h.Write([]byte(v))
		h.Write([]byte{0x1e})
	}
	return "slot:" + hex.EncodeToString(h.Sum(nil)), nil
}

// PlaceClaim decides what a claim does to a slot.
//
// It is pure and deterministic: the same policy, slot and claim always produce
// the same decision, which is what allows a disputed collection to be
// re-adjudicated years later and reach the same answer.
func PlaceClaim(p *CollectionIdentityPolicy, slot *AuthoritativeCollectionSlot, c Claim) (ClaimDecision, error) {
	if err := ValidatePolicy(p); err != nil {
		return ClaimDecision{}, err
	}
	if c.SourceRef == "" {
		return ClaimDecision{}, errors.New("claim carries no source reference")
	}
	if c.Origin == "" {
		return ClaimDecision{}, ErrNoOriginOnClaim
	}
	for _, d := range p.Dimensions {
		if normalise(c.Values[d]) == "" {
			return ClaimDecision{}, fmt.Errorf("%w: %s", ErrMissingValue, d)
		}
	}

	if slot == nil {
		return ClaimDecision{
			Outcome:       OutcomeEstablished,
			Authoritative: c.SourceRef,
			Reason:        "first claim on this slot",
		}, nil
	}

	if slot.AuthoritativeRef == c.SourceRef {
		return ClaimDecision{
			Outcome:       OutcomeReasserted,
			Authoritative: c.SourceRef,
			Reason:        "the same claim was placed again",
		}, nil
	}

	// A slot a human has not yet resolved stays conflicted. Letting a third
	// claim silently settle it would discard the disagreement that put it there.
	if slot.IsConflicted() {
		return ClaimDecision{
			Outcome: OutcomeConflict,
			Reason:  "slot is already in conflict and awaiting resolution",
			Demoted: contenderFrom(c, "arrived while the slot was in conflict"),
		}, nil
	}

	// A claim already recorded as a contender does not get to re-argue its case
	// on every redelivery.
	for _, existing := range slot.Contenders {
		if existing.SourceRef == c.SourceRef {
			return ClaimDecision{
				Outcome:       OutcomeRetained,
				Authoritative: slot.AuthoritativeRef,
				Reason:        "claim was already set aside for this slot",
			}, nil
		}
	}

	switch p.Resolution {
	case ResolveFirstWins:
		return decideByTime(slot, c, true), nil
	case ResolveLastWins:
		return decideByTime(slot, c, false), nil
	case ResolveHighestQuality:
		return decideByQuality(slot, c), nil
	case ResolveManual:
		return ClaimDecision{
			Outcome: OutcomeConflict,
			Reason: fmt.Sprintf("policy %q resolves collisions manually; %s and %s both claim this slot",
				p.Name, slot.AuthoritativeRef, c.SourceRef),
			Demoted: contenderFrom(c, "policy requires manual resolution"),
		}, nil
	default:
		return ClaimDecision{}, fmt.Errorf("%w: %q", ErrUnknownMode, p.Resolution)
	}
}

// decideByTime resolves on recording order.
//
// Claims recorded at the identical instant cannot be ordered, and guessing
// would make the outcome depend on arrival order — which is exactly the
// non-determinism this design exists to remove. Those go to conflict.
func decideByTime(slot *AuthoritativeCollectionSlot, c Claim, firstWins bool) ClaimDecision {
	incumbent := slot.IncumbentRecordedAt

	if c.RecordedAt.Equal(incumbent) {
		return ClaimDecision{
			Outcome: OutcomeConflict,
			Reason: fmt.Sprintf("%s and %s were both recorded at %s, so neither is first or last",
				slot.AuthoritativeRef, c.SourceRef, incumbent.UTC().Format(time.RFC3339)),
			Demoted: contenderFrom(c, "recorded at the same instant as the incumbent"),
		}
	}

	claimIsEarlier := c.RecordedAt.Before(incumbent)
	claimWins := claimIsEarlier == firstWins

	mode := "last"
	if firstWins {
		mode = "first"
	}
	if claimWins {
		return ClaimDecision{
			Outcome:       OutcomeReplaced,
			Authoritative: c.SourceRef,
			Reason:        fmt.Sprintf("%s claim %s supersedes %s", mode, c.SourceRef, slot.AuthoritativeRef),
			Demoted: &Contender{
				SourceRef:  slot.AuthoritativeRef,
				Origin:     slot.OriginKind,
				RecordedAt: incumbent,
				Reason:     fmt.Sprintf("superseded by the %s claim %s", mode, c.SourceRef),
			},
		}
	}
	return ClaimDecision{
		Outcome:       OutcomeRetained,
		Authoritative: slot.AuthoritativeRef,
		Reason:        fmt.Sprintf("incumbent %s is the %s claim", slot.AuthoritativeRef, mode),
		Demoted:       contenderFrom(c, fmt.Sprintf("incumbent %s is the %s claim", slot.AuthoritativeRef, mode)),
	}
}

// decideByQuality prefers the better-evidenced claim. Equal quality is not a
// tie to be broken arbitrarily — it means the policy has no basis to choose.
func decideByQuality(slot *AuthoritativeCollectionSlot, c Claim) ClaimDecision {
	incumbentQuality := slot.IncumbentQuality

	switch {
	case c.Quality > incumbentQuality:
		return ClaimDecision{
			Outcome:       OutcomeReplaced,
			Authoritative: c.SourceRef,
			Reason: fmt.Sprintf("claim %s scores %d against the incumbent's %d",
				c.SourceRef, c.Quality, incumbentQuality),
			Demoted: &Contender{
				SourceRef:  slot.AuthoritativeRef,
				Origin:     slot.OriginKind,
				RecordedAt: slot.IncumbentRecordedAt,
				Quality:    incumbentQuality,
				Reason:     fmt.Sprintf("outscored by %s", c.SourceRef),
			},
		}
	case c.Quality < incumbentQuality:
		return ClaimDecision{
			Outcome:       OutcomeRetained,
			Authoritative: slot.AuthoritativeRef,
			Reason: fmt.Sprintf("incumbent scores %d against claim %s's %d",
				incumbentQuality, c.SourceRef, c.Quality),
			Demoted: contenderFrom(c, fmt.Sprintf("outscored by the incumbent %s", slot.AuthoritativeRef)),
		}
	default:
		return ClaimDecision{
			Outcome: OutcomeConflict,
			Reason: fmt.Sprintf("%s and %s both score %d, so quality cannot separate them",
				slot.AuthoritativeRef, c.SourceRef, c.Quality),
			Demoted: contenderFrom(c, "tied with the incumbent on quality"),
		}
	}
}

func contenderFrom(c Claim, reason string) *Contender {
	return &Contender{
		SourceRef:  c.SourceRef,
		Origin:     c.Origin,
		RecordedAt: c.RecordedAt,
		Quality:    c.Quality,
		Reason:     reason,
	}
}

func normalise(v string) string { return strings.ToLower(strings.TrimSpace(v)) }

func validDimension(d IdentityDimension) bool {
	switch d {
	case DimProducer, DimCollectionDate, DimShift, DimRoute, DimCentre, DimDevice:
		return true
	default:
		return false
	}
}

func validResolution(r ResolutionMode) bool {
	switch r {
	case ResolveFirstWins, ResolveLastWins, ResolveHighestQuality, ResolveManual:
		return true
	default:
		return false
	}
}
