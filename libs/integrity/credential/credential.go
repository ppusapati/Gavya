// Package credential hashes and verifies the secrets people and services sign
// in with.
//
// # Why argon2id
//
// The threat is not somebody guessing a password at the login endpoint — that is
// rate-limited and logged. It is somebody who has the table. Against a stolen
// table the only defence is that each guess costs the attacker real time and
// real memory, and that is what a memory-hard function buys: a GPU can run
// billions of SHA-256 guesses a second and is bounded by memory bandwidth for
// argon2id, which flattens the attacker's hardware advantage.
//
// # Why the parameters live in the hash
//
// Hardware gets faster and the cost has to be raised to match. If the parameters
// are constants in this file, raising them makes every password already stored
// unverifiable, because the stored digest was produced with the old cost and
// nothing records what it was. So each hash carries its own parameters, in the
// PHC string format that argon2 and every other modern password hash uses:
//
//	$argon2id$v=19$m=65536,t=3,p=4$<salt>$<digest>
//
// Verification reads the cost out of the stored string. Raising Default only
// affects hashes made from then on, and NeedsRehash says which stored hashes are
// below the current cost so they can be upgraded the next time somebody signs in
// — which is the only moment the plaintext is available to rehash with.
//
// # Why there is a dummy hash
//
// Verifying a password takes about a tenth of a second by design. Not verifying
// one — because the address has no account — takes microseconds. That difference
// is measurable over a network and it answers "does this address have an
// account", which is the first question in every credential-stuffing run. Dummy
// gives a caller something to verify against so the work is done either way.
package credential

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Params are the cost of one hash.
type Params struct {
	// Memory in KiB. The dominant term: it is what denies an attacker the
	// ability to run thousands of guesses in parallel on one card.
	Memory uint32
	// Time is the number of passes over that memory.
	Time uint32
	// Parallelism is the number of lanes.
	Parallelism uint8
	// SaltLength and KeyLength in bytes.
	SaltLength uint32
	KeyLength  uint32
}

// Default is the cost used for new hashes.
//
// 64 MiB and three passes is the OWASP recommendation for argon2id, and lands
// around a tenth of a second on server hardware — slow enough to matter to
// somebody working through a stolen table, fast enough that a person signing in
// does not notice. Raise it when the hardware justifies it; existing hashes keep
// working, and NeedsRehash finds them.
var Default = Params{
	Memory:      64 * 1024,
	Time:        3,
	Parallelism: 4,
	SaltLength:  16,
	KeyLength:   32,
}

// Minimum is the weakest hash that will be verified at all.
//
// A stored hash carries its own cost, which means a hash written with a trivial
// cost verifies trivially. That is intended for old hashes made when the cost
// was lower, and it is also what an attacker who can write one row would use to
// make a password of their choosing verify instantly. Refusing below this floor
// costs nothing and closes it.
var Minimum = Params{Memory: 8 * 1024, Time: 1, Parallelism: 1, SaltLength: 8, KeyLength: 16}

var (
	// ErrMismatch is the only failure a caller should report to whoever is
	// signing in. Everything else here distinguishes causes that are useful to
	// an operator and useful to an attacker in equal measure.
	ErrMismatch = errors.New("credential: does not match")

	// ErrUnusableHash means the stored value is not a hash this can verify: a
	// different algorithm, a corrupted row, or a cost below the floor. Never a
	// reason to let somebody in.
	ErrUnusableHash = errors.New("credential: stored hash cannot be verified")
)

// Hash produces a PHC string for a secret.
func Hash(secret string) (string, error) { return HashWith(secret, Default) }

// HashWith produces a PHC string at a chosen cost. Exported so a test can run at
// a cost that does not make the suite take a minute.
func HashWith(secret string, p Params) (string, error) {
	if secret == "" {
		// An empty secret hashes perfectly well and then matches an empty
		// input, which turns "this account has no password yet" into "anyone
		// may sign in as this account by sending nothing".
		return "", errors.New("credential: refusing to hash an empty secret")
	}
	if err := p.check(); err != nil {
		return "", err
	}

	salt := make([]byte, p.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("credential: %w", err)
	}
	key := argon2.IDKey([]byte(secret), salt, p.Time, p.Memory, p.Parallelism, p.KeyLength)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.Memory, p.Time, p.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify reports whether a secret produced this hash.
//
// Returns ErrMismatch for a wrong secret and ErrUnusableHash for a stored value
// that cannot be checked. A caller must tell whoever is signing in the same
// thing in both cases.
func Verify(secret, encoded string) error {
	p, salt, want, err := parse(encoded)
	if err != nil {
		return err
	}
	got := argon2.IDKey([]byte(secret), salt, p.Time, p.Memory, p.Parallelism, uint32(len(want)))

	// Constant time. A byte-by-byte comparison returns sooner the earlier it
	// finds a difference, which lets an attacker who can time it recover the
	// digest one byte at a time.
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrMismatch
	}
	return nil
}

// dummyHash is a real hash of a secret nobody knows, produced once. Its cost is
// Default's, so verifying against it takes the same work as verifying against a
// real account.
var dummyHash string

// Dummy returns something to verify against when there is no account.
//
// Use it, do not skip the verification. Returning early on a missing address is
// microseconds against a tenth of a second, and that gap answers "does this
// address have an account" to anybody who can time a request.
func Dummy() string {
	if dummyHash == "" {
		// Generated rather than hard-coded, so no secret in this file is ever
		// the one a dummy verification would accept.
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			panic("credential: no randomness available: " + err.Error())
		}
		h, err := HashWith(base64.RawStdEncoding.EncodeToString(b), Default)
		if err != nil {
			panic("credential: cannot build the dummy hash: " + err.Error())
		}
		dummyHash = h
	}
	return dummyHash
}

// NeedsRehash reports whether a stored hash was made at a lower cost than the
// current default, so it can be upgraded while the plaintext is at hand — which
// is only ever during a successful sign-in.
func NeedsRehash(encoded string) bool {
	p, _, _, err := parse(encoded)
	if err != nil {
		// Unreadable is worth replacing too, once somebody proves they know the
		// secret by some other means.
		return true
	}
	return p.Memory < Default.Memory ||
		p.Time < Default.Time ||
		p.Parallelism < Default.Parallelism
}

// NewSecret returns a random secret for a service identity, in a form somebody
// can paste into a configuration file.
//
// 32 bytes, because a service credential is never typed and never remembered, so
// there is no reason for it to be short enough to guess.
func NewSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("credential: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (p Params) check() error {
	switch {
	case p.Memory < Minimum.Memory:
		return fmt.Errorf("%w: %d KiB of memory is below the floor of %d",
			ErrUnusableHash, p.Memory, Minimum.Memory)
	case p.Time < Minimum.Time:
		return fmt.Errorf("%w: %d passes is below the floor of %d",
			ErrUnusableHash, p.Time, Minimum.Time)
	case p.Parallelism < Minimum.Parallelism:
		return fmt.Errorf("%w: %d lanes is below the floor of %d",
			ErrUnusableHash, p.Parallelism, Minimum.Parallelism)
	case p.SaltLength < Minimum.SaltLength:
		return fmt.Errorf("%w: a %d-byte salt is below the floor of %d",
			ErrUnusableHash, p.SaltLength, Minimum.SaltLength)
	case p.KeyLength < Minimum.KeyLength:
		return fmt.Errorf("%w: a %d-byte digest is below the floor of %d",
			ErrUnusableHash, p.KeyLength, Minimum.KeyLength)
	}
	return nil
}

func parse(encoded string) (Params, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	// "", algorithm, version, parameters, salt, digest
	if len(parts) != 6 || parts[0] != "" {
		return Params{}, nil, nil, fmt.Errorf("%w: not a PHC string", ErrUnusableHash)
	}
	if parts[1] != "argon2id" {
		return Params{}, nil, nil, fmt.Errorf("%w: algorithm is %q, not argon2id",
			ErrUnusableHash, parts[1])
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return Params{}, nil, nil, fmt.Errorf("%w: unreadable version %q", ErrUnusableHash, parts[2])
	}
	if version != argon2.Version {
		return Params{}, nil, nil, fmt.Errorf("%w: argon2 version %d, this build speaks %d",
			ErrUnusableHash, version, argon2.Version)
	}

	var p Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.Memory, &p.Time, &p.Parallelism); err != nil {
		return Params{}, nil, nil, fmt.Errorf("%w: unreadable parameters %q", ErrUnusableHash, parts[3])
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil {
		return Params{}, nil, nil, fmt.Errorf("%w: unreadable salt", ErrUnusableHash)
	}
	digest, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil {
		return Params{}, nil, nil, fmt.Errorf("%w: unreadable digest", ErrUnusableHash)
	}

	p.SaltLength = uint32(len(salt))
	p.KeyLength = uint32(len(digest))
	if err := p.check(); err != nil {
		return Params{}, nil, nil, err
	}

	// A hash asking for more memory than the machine has does not fail — it
	// allocates until something is killed. Refusing is the difference between
	// one rejected sign-in and a service that dies.
	if uint64(p.Memory)*1024 > availableMemoryCeiling() {
		return Params{}, nil, nil, fmt.Errorf("%w: asks for %d KiB, which this machine cannot spend on one hash",
			ErrUnusableHash, p.Memory)
	}
	return p, salt, digest, nil
}

// availableMemoryCeiling is a deliberately crude bound: no single hash should
// ever want a gigabyte, and anything that does is corrupt or hostile rather
// than merely expensive.
func availableMemoryCeiling() uint64 {
	const gib = 1 << 30
	if runtime.GOARCH == "386" || runtime.GOARCH == "arm" {
		return 256 << 20
	}
	return gib
}
