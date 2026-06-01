// Tests for password hashing + verification primitives.
//
// Existence of these tests is the regression-safety lock for the bug
// closed 2026-04-26: CheckPassword previously compared []byte of the
// b64-encoded hash against the raw-decoded stored hash — different
// representations, different lengths, never equal. Result: every Login
// returned AUTH_INVALID_CREDENTIALS regardless of password correctness.
// Without these tests anyone could re-introduce the bug while
// "modernizing" the comparison or "removing redundant b64 work."

package utils

import (
	"strings"
	"testing"
)

func TestEncryptPassword_RoundTripsThroughCheckPassword(t *testing.T) {
	password := "Sup3r-Secret-Password!"

	hashB64, salt, err := EncryptPassword(password, nil)
	if err != nil {
		t.Fatalf("EncryptPassword: %v", err)
	}
	if hashB64 == "" {
		t.Fatal("hash must not be empty")
	}
	if len(salt) == 0 {
		t.Fatal("salt must not be empty")
	}

	ok, err := CheckPassword(password, salt, hashB64)
	if err != nil {
		t.Fatalf("CheckPassword: %v", err)
	}
	if !ok {
		t.Fatal("CheckPassword returned false for the password we just hashed — round-trip is broken (THIS IS THE 2026-04-26 BUG returning)")
	}
}

func TestCheckPassword_RejectsWrongPassword(t *testing.T) {
	hashB64, salt, err := EncryptPassword("right-password", nil)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	ok, err := CheckPassword("wrong-password", salt, hashB64)
	if err != nil {
		t.Fatalf("CheckPassword (wrong): %v", err)
	}
	if ok {
		t.Fatal("CheckPassword accepted a wrong password")
	}
}

func TestCheckPassword_RejectsWrongSalt(t *testing.T) {
	password := "right-password"
	hashB64, _, err := EncryptPassword(password, nil)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	wrongSalt := make([]byte, 32) // all zeros, definitely not the original
	ok, err := CheckPassword(password, wrongSalt, hashB64)
	if err != nil {
		t.Fatalf("CheckPassword (wrong salt): %v", err)
	}
	if ok {
		t.Fatal("CheckPassword accepted a wrong salt — would let any user log in with any password if salt is forgotten")
	}
}

func TestCheckPassword_RejectsCorruptedStoredHash(t *testing.T) {
	salt := make([]byte, 32)
	for i := range salt {
		salt[i] = byte(i)
	}
	// Pass a non-b64 string — must return a clear error rather than
	// silently false (which would hide the corruption from operators).
	if _, err := CheckPassword("anything", salt, "not-base64-!!@#$"); err == nil {
		t.Fatal("expected error for corrupted stored hash, got nil")
	}
}

func TestCheckPassword_DeterministicAcrossCalls(t *testing.T) {
	// Two consecutive checks of the same correct password must return
	// the same answer. Catches any accidental nondeterminism (entropy
	// being mixed in, time-based salt mutation, etc.).
	password := "deterministic-test"
	hashB64, salt, err := EncryptPassword(password, nil)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	for i := 0; i < 5; i++ {
		ok, err := CheckPassword(password, salt, hashB64)
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		if !ok {
			t.Fatalf("iter %d: false (must be deterministic true)", i)
		}
	}
}

func TestEncryptPassword_SameInputsProduceSameHash(t *testing.T) {
	// Argon2 with identical (password, salt, params) is deterministic.
	// If this test ever flakes, the package has accidentally introduced
	// nondeterministic state — likely a regression in EncryptPassword.
	password := "same-input-test"
	salt := []byte("0123456789abcdef0123456789abcdef")
	h1, _, err := EncryptPassword(password, salt)
	if err != nil {
		t.Fatalf("h1: %v", err)
	}
	h2, _, err := EncryptPassword(password, salt)
	if err != nil {
		t.Fatalf("h2: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("Argon2 with identical inputs produced different hashes: h1=%s h2=%s", h1, h2)
	}
}

func TestEncryptPassword_GeneratesUniqueSaltsWhenNoneProvided(t *testing.T) {
	// Every (password, freshly-generated-salt) tuple should differ from
	// the next. Guards against a regression where GenerateSalt returns
	// a fixed value (which would let an attacker pre-compute rainbow
	// tables for every user).
	password := "fresh-salt-test"
	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		_, salt, err := EncryptPassword(password, nil)
		if err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		key := string(salt)
		if seen[key] {
			t.Fatalf("iter %d: duplicate salt — GenerateSalt is not random", i)
		}
		seen[key] = true
	}
}

func TestEncryptPassword_NonEmptyInputProducesB64String(t *testing.T) {
	hashB64, _, err := EncryptPassword("x", nil)
	if err != nil {
		t.Fatalf("EncryptPassword: %v", err)
	}
	// All chars must be in the base64 alphabet (A-Za-z0-9+/=).
	for _, c := range hashB64 {
		if !strings.ContainsRune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/=", c) {
			t.Fatalf("hash contains non-b64 char %q in %q", c, hashB64)
		}
	}
}
