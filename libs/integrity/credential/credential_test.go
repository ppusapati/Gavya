package credential

import (
	"errors"
	"sort"
	"strings"
	"testing"
	"time"
)

// cheap keeps the suite quick. Every test that does not care about cost uses it;
// the ones that do care say so.
var cheap = Params{Memory: 8 * 1024, Time: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}

func TestASecretVerifiesAgainstItsOwnHash(t *testing.T) {
	h, err := HashWith("correct horse battery staple", cheap)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify("correct horse battery staple", h); err != nil {
		t.Errorf("the right secret did not verify: %v", err)
	}
}

func TestTheWrongSecretDoesNot(t *testing.T) {
	h, _ := HashWith("correct horse battery staple", cheap)
	if err := Verify("correct horse battery stapl", h); !errors.Is(err, ErrMismatch) {
		t.Errorf("err = %v, want ErrMismatch", err)
	}
}

// Two people with the same password must not have the same stored hash. Equal
// hashes tell anybody holding the table which accounts share a password, and
// make one cracked password crack all of them at once.
func TestTheSameSecretHashesDifferentlyEveryTime(t *testing.T) {
	a, _ := HashWith("same", cheap)
	b, _ := HashWith("same", cheap)
	if a == b {
		t.Fatal("two hashes of one secret are identical, so the salt is not doing anything")
	}
	if err := Verify("same", a); err != nil {
		t.Error(err)
	}
	if err := Verify("same", b); err != nil {
		t.Error(err)
	}
}

// An empty secret hashes perfectly well and then matches an empty input, which
// turns "this account has no password yet" into "anyone may sign in by sending
// nothing".
func TestAnEmptySecretIsRefused(t *testing.T) {
	if _, err := HashWith("", cheap); err == nil {
		t.Fatal("an empty secret was hashed")
	}
}

// Raising the cost must not invalidate what is already stored. If it does, the
// cost can never be raised, which means it never is.
func TestAHashMadeAtALowerCostStillVerifies(t *testing.T) {
	old := Params{Memory: 16 * 1024, Time: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}
	h, err := HashWith("secret", old)
	if err != nil {
		t.Fatal(err)
	}

	// The default moves up under it, as it would over the years.
	restore := Default
	Default = Params{Memory: 64 * 1024, Time: 3, Parallelism: 4, SaltLength: 16, KeyLength: 32}
	defer func() { Default = restore }()

	if err := Verify("secret", h); err != nil {
		t.Fatalf("a hash made at the old cost stopped verifying: %v", err)
	}
	if !NeedsRehash(h) {
		t.Error("the old hash was not reported as due for an upgrade")
	}
}

func TestAHashAtTheCurrentCostIsNotFlaggedForRehash(t *testing.T) {
	restore := Default
	Default = cheap
	defer func() { Default = restore }()

	h, _ := HashWith("secret", cheap)
	if NeedsRehash(h) {
		t.Error("a hash at the current cost was reported as needing one")
	}
}

// A stored hash carries its own cost, so a hash written with a trivial cost
// verifies trivially. That is what somebody who can write one row would use to
// make a password of their choosing verify instantly.
func TestAHashWithATrivialCostIsRefusedRatherThanVerified(t *testing.T) {
	// Built by hand at m=8, far below the floor. The digest does not matter:
	// this must be refused before any comparison happens.
	weak := "$argon2id$v=19$m=8,t=1,p=1$c29tZXNhbHR2YWx1ZQ$" +
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	err := Verify("anything", weak)
	if !errors.Is(err, ErrUnusableHash) {
		t.Errorf("err = %v, want ErrUnusableHash", err)
	}
}

// A hash asking for more memory than the machine has does not fail slowly — it
// allocates until something is killed. One rejected sign-in beats a service that
// dies.
//
// Four gibibytes, not something larger. An earlier version of this test used a
// value past the range of the field the parameter is parsed into, so it was
// refused for being unreadable and the ceiling was never reached — the test
// passed with the ceiling removed entirely.
func TestAHashDemandingAbsurdMemoryIsRefusedRatherThanAttempted(t *testing.T) {
	// m is in KiB, so this asks for four gibibytes.
	huge := "$argon2id$v=19$m=4194304,t=1,p=1$c29tZXNhbHR2YWx1ZQ$" +
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

	done := make(chan error, 1)
	go func() { done <- Verify("anything", huge) }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrUnusableHash) {
			t.Errorf("err = %v, want ErrUnusableHash", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("verification tried to allocate four gibibytes instead of refusing")
	}
}

func TestAStoredValueThatIsNotAHashIsRefused(t *testing.T) {
	for _, tc := range []struct{ name, value string }{
		{"empty", ""},
		{"plaintext", "hunter2"},
		{"bcrypt", "$2y$10$abcdefghijklmnopqrstuvABCDEFGHIJKLMNOPQRSTUVWXYZ012345"},
		{"argon2i rather than id", "$argon2i$v=19$m=65536,t=3,p=4$c29tZXNhbHR2YWx1ZQ$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		{"truncated", "$argon2id$v=19$m=65536,t=3,p=4$c29tZXNhbHR2YWx1ZQ"},
		{"unreadable parameters", "$argon2id$v=19$m=lots,t=3,p=4$c29tZXNhbHR2YWx1ZQ$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		{"salt is not base64", "$argon2id$v=19$m=65536,t=3,p=4$!!!!$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := Verify("anything", tc.value); !errors.Is(err, ErrUnusableHash) {
				t.Errorf("err = %v, want ErrUnusableHash", err)
			}
		})
	}
}

// A future argon2 version would hash differently for the same inputs. Verifying
// it against this build's algorithm would fail every time and look like everyone
// suddenly forgetting their password.
func TestAHashFromADifferentArgonVersionIsRefusedClearly(t *testing.T) {
	h, _ := HashWith("secret", cheap)
	other := strings.Replace(h, "$v=19$", "$v=20$", 1)
	err := Verify("secret", other)
	if !errors.Is(err, ErrUnusableHash) {
		t.Fatalf("err = %v, want ErrUnusableHash", err)
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("the error does not say the version is the problem: %v", err)
	}
}

// The dummy exists so a caller can do the same work whether or not the address
// has an account. If it verified against something, it would be a way in.
func TestTheDummyHashIsUsableAndUnmatchable(t *testing.T) {
	d := Dummy()
	if d == "" {
		t.Fatal("no dummy hash")
	}
	if err := Verify("anything at all", d); !errors.Is(err, ErrMismatch) {
		t.Errorf("verifying against the dummy gave %v, want a plain mismatch — it has to "+
			"reach the comparison, and it has to lose", err)
	}
	if Dummy() != d {
		t.Error("the dummy changes between calls, so the work done is not repeatable")
	}
}

// The whole point of the dummy. If verifying a wrong password against a real
// account and against the dummy take visibly different times, the difference
// answers "does this address have an account".
func TestVerifyingAgainstTheDummyCostsWhatARealVerificationCosts(t *testing.T) {
	if testing.Short() {
		t.Skip("times a real hash at the production cost")
	}
	real, err := Hash("the real password")
	if err != nil {
		t.Fatal(err)
	}

	// The median of several runs, not the mean of a few. A test machine under
	// load produces occasional slow runs, and a mean over three of them moves
	// enough to fail a test that is measuring something else entirely.
	measure := func(hash string) time.Duration {
		const runs = 5
		times := make([]time.Duration, runs)
		for i := range times {
			start := time.Now()
			_ = Verify("a wrong guess", hash)
			times[i] = time.Since(start)
		}
		sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
		return times[runs/2]
	}
	// Warm both paths before measuring either.
	_ = Verify("x", real)
	_ = Verify("x", Dummy())

	withAccount := measure(real)
	without := measure(Dummy())

	// A wide band on purpose. The defect this guards is returning early on a
	// missing account, which is microseconds against a tenth of a second — three
	// orders of magnitude. Anything tight enough to also catch a 20% difference
	// is tight enough to fail on a busy machine, and a security test that flakes
	// is a security test that gets skipped.
	ratio := float64(withAccount) / float64(without)
	if ratio < 0.25 || ratio > 4.0 {
		t.Errorf("verifying against a real hash took %v and against the dummy %v (%.2fx) — "+
			"a gap that size tells an attacker which addresses have accounts",
			withAccount, without, ratio)
	}
}

func TestAGeneratedServiceSecretIsLongAndDifferentEachTime(t *testing.T) {
	a, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewSecret()
	if a == b {
		t.Fatal("two generated secrets are identical")
	}
	if len(a) < 40 {
		t.Errorf("a generated secret is %d characters; it is never typed, so there is no "+
			"reason for it to be short", len(a))
	}
	h, err := HashWith(a, cheap)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(a, h); err != nil {
		t.Errorf("a generated secret does not verify against its own hash: %v", err)
	}
}
