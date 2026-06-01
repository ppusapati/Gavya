package utils

import (
	"crypto/subtle"
	"encoding/base64"
	"fmt"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/scrypt"
)

// PasswordHashAlgorithm defines the supported password hashing algorithms
type PasswordHashAlgorithm string

const (
	// Scrypt constants
	scryptN         = 32768 // CPU/memory cost parameter
	scryptR         = 8     // Block size parameter
	scryptP         = 1     // Parallelization parameter
	scryptKeyLength = 32    // Desired key length in bytes

	// Argon2 constants
	argon2Time    = 3         // Number of iterations
	argon2Memory  = 64 * 1024 // Memory usage of 64 MB
	argon2Threads = 4         // Number of threads
	argon2KeyLen  = 32        // Desired key length in bytes

	// Algorithm types
	AlgorithmScrypt PasswordHashAlgorithm = "scrypt"
	AlgorithmArgon2 PasswordHashAlgorithm = "argon2"
)

// EncryptPasswordOptions provides configuration for password encryption
type EncryptPasswordOptions struct {
	Algorithm PasswordHashAlgorithm
}

// DefaultEncryptPasswordOptions returns the default encryption options
func DefaultEncryptPasswordOptions() *EncryptPasswordOptions {
	return &EncryptPasswordOptions{
		Algorithm: AlgorithmArgon2, // Recommended default
	}
}

// EncryptPassword securely hashes a password using the specified algorithm
func EncryptPassword(password string, existingSalt []byte, opts ...*EncryptPasswordOptions) (string, []byte, error) {
	// Determine options
	option := DefaultEncryptPasswordOptions()
	if len(opts) > 0 && opts[0] != nil {
		option = opts[0]
	}

	// Generate a new salt if not provided
	var salt []byte
	var err error
	if existingSalt == nil {
		salt, err = GenerateSalt()
		if err != nil {
			return "", nil, fmt.Errorf("failed to generate salt: %w", err)
		}
	} else {
		salt = existingSalt
	}

	var hashedPassword []byte
	switch option.Algorithm {
	case AlgorithmScrypt:
		hashedPassword, err = encryptPasswordScrypt(password, salt)
	case AlgorithmArgon2:
		hashedPassword, err = encryptPasswordArgon2(password, salt)
	default:
		return "", nil, fmt.Errorf("unsupported password hashing algorithm: %s", option.Algorithm)
	}

	if err != nil {
		return "", nil, fmt.Errorf("password encryption failed: %w", err)
	}

	// Base64 encode for storage
	return base64.StdEncoding.EncodeToString(hashedPassword), salt, nil
}

// encryptPasswordScrypt hashes password using scrypt
func encryptPasswordScrypt(password string, salt []byte) ([]byte, error) {
	return scrypt.Key(
		[]byte(password),
		salt,
		scryptN,
		scryptR,
		scryptP,
		scryptKeyLength,
	)
}

// encryptPasswordArgon2 hashes password using Argon2id
func encryptPasswordArgon2(password string, salt []byte) ([]byte, error) {
	return argon2.IDKey(
		[]byte(password),
		salt,
		argon2Time,
		argon2Memory,
		argon2Threads,
		argon2KeyLen,
	), nil
}

// CheckPassword verifies if the entered password matches the stored password
// using constant-time comparison.
//
// Both sides MUST be in the same representation before subtle.ConstantTimeCompare
// — the function is byte-for-byte. Pre-2026-04-26 the implementation compared
// `[]byte(hashedPassword)` (the BASE64-ENCODED ASCII bytes returned by
// EncryptPassword) against `decodedStoredPassword` (the raw 32 bytes after
// b64 decode of the stored hash). Those representations differ in length
// (44 ASCII bytes vs 32 raw bytes) and content, so the compare always
// returned 0 — meaning AuthService/Login could never validate ANY password.
// The bug was masked because the only path that exercised this end-to-end
// was prod login, and there were no integration tests covering it.
//
// The fix below normalizes both sides to the same b64 representation. We
// do NOT round-trip the stored hash through decode→re-encode (that would
// drop padding canonicalization and let two valid encodings of the same
// raw hash silently mismatch); we just compare both as their canonical
// b64 strings via subtle.ConstantTimeCompare on the byte slices.
func CheckPassword(enteredPassword string, salt []byte, storedPassword string, opts ...*EncryptPasswordOptions) (bool, error) {
	// Determine options
	option := DefaultEncryptPasswordOptions()
	if len(opts) > 0 && opts[0] != nil {
		option = opts[0]
	}

	// EncryptPassword returns the b64-encoded hash string. Compare against
	// the b64-encoded storedPassword directly — same representation, same
	// length, constant-time semantics preserved.
	hashedPasswordB64, _, err := EncryptPassword(enteredPassword, salt, option)
	if err != nil {
		return false, fmt.Errorf("password check failed: %w", err)
	}

	// Validate the stored hash decodes cleanly first — guards against
	// stored-row corruption returning a confusing constant-time false
	// instead of a clear error.
	if _, err := base64.StdEncoding.DecodeString(storedPassword); err != nil {
		return false, fmt.Errorf("invalid stored password format: %w", err)
	}

	return subtle.ConstantTimeCompare([]byte(hashedPasswordB64), []byte(storedPassword)) == 1, nil
}
