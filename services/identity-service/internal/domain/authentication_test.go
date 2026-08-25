package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var now = time.Date(2026, 8, 25, 6, 30, 0, 0, time.UTC)

func account(mods ...func(*Account)) Account {
	a := Account{Exists: true, UserID: "US_1", PasswordHash: "$argon2id$x", Status: "active"}
	for _, m := range mods {
		m(&a)
	}
	return a
}

func TestTheRightPasswordSignsSomebodyIn(t *testing.T) {
	out := DefaultLockout.Decide(now, account(), true)
	if !out.Allowed {
		t.Fatalf("refused: %s", out.Reason)
	}
}

func TestTheWrongPasswordDoesNot(t *testing.T) {
	out := DefaultLockout.Decide(now, account(), false)
	if out.Allowed {
		t.Fatal("the wrong password signed somebody in")
	}
	if out.FailedAttempts != 1 {
		t.Errorf("failed attempts = %d, want 1", out.FailedAttempts)
	}
}

// An address with no account and an address with the wrong password must be
// indistinguishable to whoever is asking. The reasons differ in the log, which
// is where the difference is useful and where an attacker cannot see it.
func TestAMissingAccountAndAWrongPasswordAreBothJustRefused(t *testing.T) {
	missing := DefaultLockout.Decide(now, Account{}, false)
	wrong := DefaultLockout.Decide(now, account(), false)

	if missing.Allowed || wrong.Allowed {
		t.Fatal("one of these was allowed")
	}
	if missing.Reason == wrong.Reason {
		t.Error("the operator log cannot tell the two apart, which is the half that should differ")
	}
	// What the client is told comes from ErrSignInFailed in both cases, and
	// that is a single value, so there is nothing here to diverge.
	if ErrSignInFailed == nil {
		t.Error("no single refusal for the client to receive")
	}
}

// A person who mistypes four times and then gets it right must not be one
// mistake from a lock tomorrow.
func TestSigningInSuccessfullyClearsTheCount(t *testing.T) {
	out := DefaultLockout.Decide(now, account(func(a *Account) { a.FailedAttempts = 4 }), true)
	if !out.Allowed {
		t.Fatalf("refused: %s", out.Reason)
	}
	if out.FailedAttempts != 0 {
		t.Errorf("failed attempts = %d after a success, want 0", out.FailedAttempts)
	}
	if out.LockUntil != nil {
		t.Error("a successful sign-in left a lock in place")
	}
}

func TestRepeatedFailuresEventuallyLock(t *testing.T) {
	l := DefaultLockout
	for n := 1; n < l.Threshold; n++ {
		out := l.Decide(now, account(func(a *Account) { a.FailedAttempts = n - 1 }), false)
		if out.LockUntil != nil {
			t.Fatalf("locked after %d failures, before the threshold of %d", n, l.Threshold)
		}
	}
	out := l.Decide(now, account(func(a *Account) { a.FailedAttempts = l.Threshold - 1 }), false)
	if out.LockUntil == nil {
		t.Fatalf("not locked after %d failures", l.Threshold)
	}
	if got := out.LockUntil.Sub(now); got != l.Initial {
		t.Errorf("first lock is %v, want %v", got, l.Initial)
	}
}

func TestTheLockGrowsAndThenStops(t *testing.T) {
	l := DefaultLockout
	var last time.Duration
	for n := l.Threshold; n < l.Threshold+20; n++ {
		out := l.Decide(now, account(func(a *Account) { a.FailedAttempts = n - 1 }), false)
		if out.LockUntil == nil {
			t.Fatalf("no lock after %d failures", n)
		}
		d := out.LockUntil.Sub(now)
		if d < last {
			t.Errorf("the lock shortened from %v to %v at %d failures", last, d, n)
		}
		if d > l.Max {
			t.Fatalf("the lock reached %v, past the cap of %v — a person who forgot their "+
				"password would be shut out for the rest of the day", d, l.Max)
		}
		last = d
	}
	if last != l.Max {
		t.Errorf("the lock stalled at %v and never reached the cap of %v", last, l.Max)
	}
}

// An attacker hammering a locked account must not extend the lock. Otherwise the
// protection becomes the denial of service it was meant to bound: anybody who
// knows an address can keep its owner out indefinitely.
func TestFailingAgainstALockedAccountDoesNotExtendTheLock(t *testing.T) {
	l := DefaultLockout
	until := now.Add(2 * time.Minute)
	locked := account(func(a *Account) {
		a.FailedAttempts = 9
		a.LockedUntil = &until
	})

	out := l.Decide(now, locked, false)
	if out.Allowed {
		t.Fatal("a locked account was signed in")
	}
	if out.FailedAttempts != 9 {
		t.Errorf("failed attempts went to %d while locked, want them held at 9", out.FailedAttempts)
	}
	if out.LockUntil == nil || !out.LockUntil.Equal(until) {
		t.Errorf("the lock moved to %v, want it left at %v", out.LockUntil, until)
	}
}

// And the right password must not work while the lock stands, or an attacker
// could use the response to confirm a guess against a locked account.
func TestTheRightPasswordDoesNotWorkWhileLocked(t *testing.T) {
	until := now.Add(2 * time.Minute)
	out := DefaultLockout.Decide(now, account(func(a *Account) { a.LockedUntil = &until }), true)
	if out.Allowed {
		t.Fatal("a locked account was signed in with the right password")
	}
}

func TestALockThatHasExpiredNoLongerBlocks(t *testing.T) {
	past := now.Add(-time.Second)
	out := DefaultLockout.Decide(now, account(func(a *Account) {
		a.FailedAttempts = 7
		a.LockedUntil = &past
	}), true)
	if !out.Allowed {
		t.Fatalf("an expired lock still blocked: %s", out.Reason)
	}
}

func TestASuspendedAccountIsRefused(t *testing.T) {
	out := DefaultLockout.Decide(now, account(func(a *Account) { a.Status = "suspended" }), true)
	if out.Allowed {
		t.Fatal("a suspended account was signed in with the right password")
	}
	// Counted like any other failure, so the count does not distinguish a
	// suspension from a wrong password.
	if out.FailedAttempts != 1 {
		t.Errorf("failed attempts = %d, want a suspension to count like any other failure",
			out.FailedAttempts)
	}
}

// An account with no password set must not be signed in by presenting nothing.
func TestAnAccountWithNoPasswordCannotBeSignedInTo(t *testing.T) {
	out := DefaultLockout.Decide(now, account(func(a *Account) { a.PasswordHash = "" }), true)
	if out.Allowed {
		t.Fatal("an account with no password was signed in")
	}
}

// -------------------------------------------------------------------------
// Choosing a tenant
// -------------------------------------------------------------------------

func member(tenant string, mods ...func(*Membership)) Membership {
	m := Membership{TenantID: tenant, RoleID: "RL_1", RoleName: "manager", Status: "active"}
	for _, f := range mods {
		f(&m)
	}
	return m
}

func TestSomebodyInOneTenantGoesToIt(t *testing.T) {
	got, err := ChooseTenant("", []Membership{member("T_A")})
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != "T_A" {
		t.Errorf("tenant = %s", got.TenantID)
	}
}

// The case the whole design exists for. A vet across four societies signs in to
// one, because every policy downstream compares against a single value and "all
// of them" has no representation there.
func TestSomebodyInSeveralTenantsMustSayWhich(t *testing.T) {
	_, err := ChooseTenant("", []Membership{member("T_A"), member("T_B"), member("T_C")})

	var amb *AmbiguousTenantError
	if !errors.As(err, &amb) {
		t.Fatalf("err = %v, want a refusal naming the choices", err)
	}
	if len(amb.Tenants) != 3 {
		t.Errorf("the refusal names %v, want all three", amb.Tenants)
	}
	if !strings.Contains(amb.Error(), "T_A") || !strings.Contains(amb.Error(), "T_C") {
		t.Errorf("the message does not list the options: %s", amb.Error())
	}
}

func TestADefaultSettlesIt(t *testing.T) {
	got, err := ChooseTenant("", []Membership{
		member("T_A"),
		member("T_B", func(m *Membership) { m.IsDefault = true }),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != "T_B" {
		t.Errorf("tenant = %s, want the default", got.TenantID)
	}
}

func TestAskingForATenantYouBelongToWorks(t *testing.T) {
	got, err := ChooseTenant("T_B", []Membership{member("T_A"), member("T_B")})
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != "T_B" {
		t.Errorf("tenant = %s", got.TenantID)
	}
}

// A tenant somebody does not belong to and a tenant that does not exist give the
// same answer. Telling them apart would let anybody with an account enumerate
// the tenant list one guess at a time.
func TestAskingForATenantYouDoNotBelongToIsRefused(t *testing.T) {
	_, err := ChooseTenant("T_SOMEBODY_ELSE", []Membership{member("T_A")})
	if !errors.Is(err, ErrNotAMember) {
		t.Errorf("err = %v, want ErrNotAMember", err)
	}
}

func TestASuspendedMembershipDoesNotCount(t *testing.T) {
	_, err := ChooseTenant("T_A", []Membership{
		member("T_A", func(m *Membership) { m.Status = "suspended" }),
	})
	if !errors.Is(err, ErrNotAMember) {
		t.Errorf("err = %v, want a suspended membership to be no membership", err)
	}

	// And it does not make an otherwise unambiguous choice ambiguous.
	got, err := ChooseTenant("", []Membership{
		member("T_A"),
		member("T_B", func(m *Membership) { m.Status = "suspended" }),
	})
	if err != nil {
		t.Fatalf("a suspended second membership made the choice ambiguous: %v", err)
	}
	if got.TenantID != "T_A" {
		t.Errorf("tenant = %s", got.TenantID)
	}
}

func TestSomebodyInNoTenantCannotSignIn(t *testing.T) {
	if _, err := ChooseTenant("", nil); !errors.Is(err, ErrNoTenant) {
		t.Errorf("err = %v, want ErrNoTenant", err)
	}
}

// -------------------------------------------------------------------------
// Services
// -------------------------------------------------------------------------

func TestAServiceWithAGoodSecretGetsASession(t *testing.T) {
	s := ServiceIdentity{Exists: true, ID: "SI_1", Status: "active", ExpiresAt: now.Add(24 * time.Hour)}
	if out := DecideService(now, s, true); !out.Allowed {
		t.Fatalf("refused: %s", out.Reason)
	}
}

// The expiry is checked where the credential is read, not left to whoever
// remembers. A service credential is a password nobody notices needs rotating.
func TestAnExpiredServiceCredentialIsRefusedEvenWithTheRightSecret(t *testing.T) {
	s := ServiceIdentity{Exists: true, ID: "SI_1", Status: "active", ExpiresAt: now.Add(-time.Second)}
	out := DecideService(now, s, true)
	if out.Allowed {
		t.Fatal("an expired service credential was accepted")
	}
	if !strings.Contains(out.Reason, "expired") {
		t.Errorf("reason = %q", out.Reason)
	}
}

func TestARevokedServiceIdentityIsRefused(t *testing.T) {
	s := ServiceIdentity{Exists: true, ID: "SI_1", Status: "revoked", ExpiresAt: now.Add(24 * time.Hour)}
	if out := DecideService(now, s, true); out.Allowed {
		t.Fatal("a revoked service identity was accepted")
	}
}

func TestAServiceSessionIsShorterThanAPersons(t *testing.T) {
	if ServiceSessionLifetime >= SessionLifetime {
		t.Errorf("a service session lasts %v and a person's %v; a service renews without "+
			"anybody noticing, so there is no reason for its credentials to sit around longer",
			ServiceSessionLifetime, SessionLifetime)
	}
}

// Twelve hours covers a working day including both milking shifts. Somebody
// signed out mid-collection, with a queue behind them, writes the readings on
// paper — and the paper is what this platform exists to replace.
func TestASessionCoversAWorkingDay(t *testing.T) {
	if SessionLifetime < 12*time.Hour {
		t.Errorf("a session lasts %v, which does not span morning and evening collection",
			SessionLifetime)
	}
	if SessionLifetime > 24*time.Hour {
		t.Errorf("a session lasts %v; one nobody revoked would outlive the week", SessionLifetime)
	}
}
