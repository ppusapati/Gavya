// Package domain holds the decisions authentication makes, separated from the
// database and the wire so they can be reasoned about and tested on their own.
package domain

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Account is what the store knows about somebody trying to sign in. Assembled
// even when no such account exists, with Exists false, so the caller does the
// same work either way.
type Account struct {
	Exists         bool
	UserID         string
	PasswordHash   string
	Status         string
	FailedAttempts int
	LockedUntil    *time.Time
}

// Membership is one tenant a person belongs to.
type Membership struct {
	TenantID  string
	RoleID    string
	RoleName  string
	Status    string
	IsDefault bool
}

// Outcome is what an attempt decided, and what to record about it.
type Outcome struct {
	Allowed bool

	// Reason names the cause, for the operator reading the log. It must never
	// reach whoever is signing in: "no such account" and "wrong password" are
	// different answers to "does this address exist", which is the first
	// question in every credential-stuffing run. The client is told the same
	// thing every time.
	Reason string

	// FailedAttempts and LockUntil are the account's new state.
	FailedAttempts int
	LockUntil      *time.Time
}

// ErrSignInFailed is what the client is told, whatever actually happened.
var ErrSignInFailed = errors.New("the email address or password is not correct")

// Lockout is how repeated failures are slowed.
//
// This trades availability for brute-force resistance, and the trade is not
// free: anybody who knows a colleague's address can keep them locked out by
// failing on purpose. It is the right way round here because the addresses in a
// dairy ERP are known — they are on the society's noticeboard — so an attacker
// needs no discovery step and a password is all that stands in the way.
//
// The cost is bounded on purpose. The lock grows with each failure and stops at
// a quarter of an hour, so a person who has genuinely forgotten waits a few
// minutes rather than being shut out of the working day, while an attacker gets
// a few attempts an hour instead of thousands a second.
type Lockout struct {
	// After this many consecutive failures, locking starts.
	Threshold int
	// The first lock. Each further failure doubles it.
	Initial time.Duration
	// The longest a lock ever lasts.
	Max time.Duration
}

var DefaultLockout = Lockout{Threshold: 5, Initial: time.Minute, Max: 15 * time.Minute}

// Decide says whether an attempt succeeds, given whether the password verified.
//
// It takes the verification result rather than doing it, because comparing a
// password is the credential package's job and this has no business holding a
// plaintext. It takes now as an argument so a test can put the clock where it
// needs it.
func (l Lockout) Decide(now time.Time, a Account, passwordMatched bool) Outcome {
	// The order matters. A locked account is refused before the password is
	// considered, so an attacker cannot use the response to test passwords
	// against a locked account; and a missing account produces the same refusal
	// as a wrong password.
	switch {
	case a.LockedUntil != nil && a.LockedUntil.After(now):
		// The failure count is not raised. Otherwise an attacker hammering a
		// locked account extends the lock indefinitely, which turns the
		// protection into the denial of service it was meant to bound.
		return Outcome{Reason: "locked", FailedAttempts: a.FailedAttempts, LockUntil: a.LockedUntil}

	case !a.Exists:
		return Outcome{Reason: "no such account"}

	case a.Status != "active":
		// Counted as a failure like any other, so an attacker cannot tell a
		// suspended account from a wrong password by how the count behaves.
		return l.fail(now, a, "account is "+a.Status)

	case a.PasswordHash == "":
		return l.fail(now, a, "account has no password set")

	case !passwordMatched:
		return l.fail(now, a, "wrong password")
	}

	// Success clears the count. A person who mistypes four times and then gets
	// it right should not be one mistake away from a lock tomorrow.
	return Outcome{Allowed: true, FailedAttempts: 0, LockUntil: nil}
}

func (l Lockout) fail(now time.Time, a Account, reason string) Outcome {
	n := a.FailedAttempts + 1
	out := Outcome{Reason: reason, FailedAttempts: n}

	if n < l.Threshold {
		return out
	}
	d := l.Initial
	for i := l.Threshold; i < n && d < l.Max; i++ {
		d *= 2
	}
	if d > l.Max {
		d = l.Max
	}
	until := now.Add(d)
	out.LockUntil = &until
	return out
}

// ErrNoTenant is returned when somebody has no tenant to sign in to.
var ErrNoTenant = errors.New("this account belongs to no tenant")

// ErrNotAMember is returned when somebody asks for a tenant they do not belong
// to. Deliberately the same for a tenant that does not exist: distinguishing
// them would let anybody with an account enumerate the tenant list.
var ErrNotAMember = errors.New("this account does not belong to that tenant")

// AmbiguousTenantError is returned when a person belongs to several tenants and
// did not say which.
type AmbiguousTenantError struct {
	Tenants []string
}

func (e *AmbiguousTenantError) Error() string {
	return fmt.Sprintf("this account belongs to %d tenants and none is marked default; "+
		"sign in to one of: %s", len(e.Tenants), strings.Join(e.Tenants, ", "))
}

// ChooseTenant decides which tenant a session acts for.
//
// A session is scoped to one tenant, never to all of a person's tenants at once,
// because every downstream policy compares against a single value and "all of
// them" has no representation there. A vet who works across four societies signs
// in to one and switches.
//
// When the answer is genuinely unclear it refuses and says what the options are.
// Picking the first, or the most recently joined, would be a silent decision
// about whose data somebody is about to see and edit — and it would be right
// often enough that the times it was wrong would not be noticed.
func ChooseTenant(requested string, memberships []Membership) (Membership, error) {
	var active []Membership
	for _, m := range memberships {
		if m.Status == "active" {
			active = append(active, m)
		}
	}

	if requested != "" {
		for _, m := range active {
			if m.TenantID == requested {
				return m, nil
			}
		}
		return Membership{}, ErrNotAMember
	}

	switch {
	case len(active) == 0:
		return Membership{}, ErrNoTenant
	case len(active) == 1:
		return active[0], nil
	}

	for _, m := range active {
		if m.IsDefault {
			return m, nil
		}
	}

	ids := make([]string, 0, len(active))
	for _, m := range active {
		ids = append(ids, m.TenantID)
	}
	sort.Strings(ids)
	return Membership{}, &AmbiguousTenantError{Tenants: ids}
}

// SessionLifetime is how long a session lasts before it has to be renewed.
//
// Twelve hours covers a working day including both milking shifts, so nobody is
// signed out mid-collection — a person standing at a chilling centre with a
// queue behind them will write the readings on paper rather than sign in again,
// and the paper is what the platform exists to replace. It is also short enough
// that a session nobody revoked does not outlive the season.
const SessionLifetime = 12 * time.Hour

// ServiceSessionLifetime is shorter. A service renews without anybody noticing,
// so there is no reason for its credentials to sit around.
const ServiceSessionLifetime = time.Hour

// ServiceIdentity is what the store knows about a calling service.
type ServiceIdentity struct {
	Exists      bool
	ID          string
	TenantID    string
	SecretHash  string
	Status      string
	ExpiresAt   time.Time
	Permissions []string
}

// DecideService says whether a service may have a session.
func DecideService(now time.Time, s ServiceIdentity, secretMatched bool) Outcome {
	switch {
	case !s.Exists:
		return Outcome{Reason: "no such service identity"}
	case s.Status != "active":
		return Outcome{Reason: "service identity is " + s.Status}
	case !s.ExpiresAt.After(now):
		// Checked here rather than left to whoever remembers. A service
		// credential is a password nobody notices needs rotating.
		return Outcome{Reason: "service credential expired on " + s.ExpiresAt.Format(time.RFC3339)}
	case !secretMatched:
		return Outcome{Reason: "wrong secret"}
	}
	return Outcome{Allowed: true}
}
