// Package bitemporal provides the two time axes every authoritative record
// carries: when a fact was true in the world (valid time) and when the platform
// learned it (transaction time).
//
// Corrections are appended as new versions with a later recorded_at and an
// overlapping valid interval; nothing is ever updated in place. This is what
// makes a settlement replayable "as we knew it on date X".
package bitemporal

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvertedInterval = errors.New("bitemporal: valid_to precedes valid_from")
	ErrZeroValidFrom    = errors.New("bitemporal: valid_from must be set")
	ErrEmptyInterval    = errors.New("bitemporal: valid_from equals valid_to, so the fact was true for no time at all")
)

// EndOfTime marks an open-ended valid interval. A concrete sentinel rather than
// NULL keeps interval overlap checks expressible in plain SQL predicates.
var EndOfTime = time.Date(9999, time.December, 31, 23, 59, 59, 0, time.UTC)

// Interval is a half-open valid-time range [From, To).
type Interval struct {
	From time.Time `json:"valid_from"`
	To   time.Time `json:"valid_to"`
}

func NewInterval(from, to time.Time) (Interval, error) {
	iv := Interval{From: from.UTC(), To: to.UTC()}
	return iv, iv.Validate()
}

// OpenInterval starts at from and remains true until superseded.
func OpenInterval(from time.Time) Interval {
	return Interval{From: from.UTC(), To: EndOfTime}
}

func (i Interval) Validate() error {
	if i.From.IsZero() {
		return ErrZeroValidFrom
	}
	if i.To.Before(i.From) {
		return fmt.Errorf("%w: %s < %s", ErrInvertedInterval, i.To, i.From)
	}
	// The interval is half-open, so [t, t) contains nothing — not even t. A
	// record written with one exists, occupies whatever slot it was written
	// into, and can never be returned by any query at any valid time. That is
	// worse than a refusal, because nothing afterwards reports it as missing:
	// it is simply a fact the platform holds and cannot find.
	if i.To.Equal(i.From) {
		return fmt.Errorf("%w: %s", ErrEmptyInterval, i.From)
	}
	return nil
}

func (i Interval) IsOpen() bool { return i.To.Equal(EndOfTime) }

// Contains reports whether t falls in [From, To).
func (i Interval) Contains(t time.Time) bool {
	u := t.UTC()
	return !u.Before(i.From) && u.Before(i.To)
}

// Overlaps reports whether two half-open intervals intersect.
func (i Interval) Overlaps(o Interval) bool {
	return i.From.Before(o.To) && o.From.Before(i.To)
}

// Version is the bitemporal envelope stored alongside a record's payload.
// RecordedAt is assigned by the database clock on insert; SupersededAt is set
// on the prior version when a correction lands, which is the only permitted
// write to an existing row.
type Version struct {
	Valid        Interval   `json:"valid"`
	RecordedAt   time.Time  `json:"recorded_at"`
	SupersededAt *time.Time `json:"superseded_at,omitempty"`
	SupersededBy string     `json:"superseded_by,omitempty"`
}

func (v Version) IsCurrent() bool { return v.SupersededAt == nil }

// KnownAt reports whether this version was part of the platform's knowledge at
// transaction time asOf: recorded by then and not yet superseded by then.
func (v Version) KnownAt(asOf time.Time) bool {
	u := asOf.UTC()
	if v.RecordedAt.After(u) {
		return false
	}
	return v.SupersededAt == nil || v.SupersededAt.After(u)
}

// EffectiveAt reports whether the version is the platform's answer for a fact
// valid at validAt, as known at transaction time asOf.
func (v Version) EffectiveAt(validAt, asOf time.Time) bool {
	return v.KnownAt(asOf) && v.Valid.Contains(validAt)
}

// Snapshot selects the versions that answer a bitemporal query. Callers pass
// versions for a single logical entity; the result preserves input order.
func Snapshot[T any](items []T, versionOf func(T) Version, validAt, asOf time.Time) []T {
	out := make([]T, 0, len(items))
	for _, it := range items {
		if versionOf(it).EffectiveAt(validAt, asOf) {
			out = append(out, it)
		}
	}
	return out
}
