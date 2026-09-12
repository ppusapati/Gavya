// Package sys is the two dependencies every service takes on the world: an
// identifier source and a clock.
//
// Both were declared separately in about twenty main packages — `type ids
// struct{}` with a New method, `type clock struct{}` with a Now — which cost
// nothing while a service was only ever built by its own main function, and cost
// exactly one thing the moment something else wanted to build one: a type
// declared in package main cannot be named from anywhere else, so the wiring
// could not be moved or reused.
//
// They are here so a service's composition root can live somewhere importable,
// which is what lets the same twenty-nine services run as twenty-nine processes
// or as one.
package sys

import (
	"time"

	ulidpkg "p9e.in/samavaya/packages/ulid"
)

// IDs hands out identifiers.
type IDs struct{}

// New is a fresh ULID.
func (IDs) New() string { return ulidpkg.New().String() }

// Clock reads the time.
type Clock struct{}

// Now is the current instant, in UTC.
//
// Always UTC, never Local. A service that reads a local clock records an instant
// whose meaning depends on which machine it ran on, and a settlement period that
// starts at a different moment on two hosts is a settlement nobody can reproduce.
func (Clock) Now() time.Time { return time.Now().UTC() }
