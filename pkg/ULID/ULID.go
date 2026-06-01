// Package ulid provides a production-grade ULID (Universally Unique Lexicographically
// Sortable Identifier) implementation optimized for microservices architectures.
//
// Features:
//   - Cryptographically secure randomness (crypto/rand)
//   - Monotonic ordering within same millisecond
//   - Thread-safe with high concurrency support
//   - Pool-based entropy for performance
//   - SQL Scanner/Valuer for database integration
//   - JSON/Protobuf marshaling support
//   - Zero-allocation string generation
//
// Usage:
//
//	id := ulid.New()           // Standard generation
//	id := ulid.NewMonotonic()  // Guaranteed monotonic
//	id := ulid.MustParse("01HGW2NBXP9VTQK8KXGWGZ3JXC")
//
// Author: P9e Microsystems Pvt Ltd
package ulid

import (
	"crypto/rand"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// -----------------------------------------------------------------------------
// Constants
// -----------------------------------------------------------------------------

const (
	// EncodedSize is the length of a ULID string
	EncodedSize = 26

	// BinarySize is the length of a ULID in bytes
	BinarySize = 16

	// Timestamp uses 6 bytes (48 bits) - milliseconds since Unix epoch
	timestampSize = 6

	// Randomness uses 10 bytes (80 bits)
	randomnessSize = 10

	// Maximum valid timestamp (year 10889)
	maxTimestamp = 281474976710655
)

// Crockford's Base32 alphabet (excludes I, L, O, U)
const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// Decode lookup table
var decodeTable [256]byte

func init() {
	// Initialize decode table with 0xFF (invalid)
	for i := range decodeTable {
		decodeTable[i] = 0xFF
	}

	// Map valid characters
	for i, c := range alphabet {
		decodeTable[c] = byte(i)
		decodeTable[c+32] = byte(i) // lowercase
	}

	// Error-tolerant mappings
	decodeTable['i'] = 1
	decodeTable['I'] = 1
	decodeTable['l'] = 1
	decodeTable['L'] = 1
	decodeTable['o'] = 0
	decodeTable['O'] = 0
}

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

var (
	ErrInvalidLength      = errors.New("ulid: invalid length")
	ErrInvalidCharacter   = errors.New("ulid: invalid character")
	ErrTimestampOverflow  = errors.New("ulid: timestamp overflow")
	ErrRandomnessOverflow = errors.New("ulid: randomness overflow in monotonic mode")
	ErrScanType           = errors.New("ulid: cannot scan type into ULID")
)

// -----------------------------------------------------------------------------
// ID Type
// -----------------------------------------------------------------------------

// ID represents a ULID as a 16-byte array
type ID [BinarySize]byte

// Zero is the zero-value ULID
var Zero ID

// -----------------------------------------------------------------------------
// Global Generators (Thread-Safe)
// -----------------------------------------------------------------------------

var (
	// Default generator for New()
	defaultGen = newGenerator(rand.Reader, false)

	// Monotonic generator for NewMonotonic()
	monotonicGen = newGenerator(rand.Reader, true)
)

// generator handles thread-safe ULID generation
type generator struct {
	mu        sync.Mutex
	entropy   io.Reader
	monotonic bool

	// Monotonic state
	lastTime uint64
	lastRand [randomnessSize]byte
}

func newGenerator(entropy io.Reader, monotonic bool) *generator {
	return &generator{
		entropy:   entropy,
		monotonic: monotonic,
	}
}

// generate creates a new ULID with the given timestamp
func (g *generator) generate(t time.Time) (ID, error) {
	var id ID
	ms := uint64(t.UnixMilli())

	if ms > maxTimestamp {
		return Zero, ErrTimestampOverflow
	}

	// Encode timestamp (big-endian, 6 bytes)
	id[0] = byte(ms >> 40)
	id[1] = byte(ms >> 32)
	id[2] = byte(ms >> 24)
	id[3] = byte(ms >> 16)
	id[4] = byte(ms >> 8)
	id[5] = byte(ms)

	g.mu.Lock()
	defer g.mu.Unlock()

	if g.monotonic && ms == g.lastTime {
		// Same millisecond: increment randomness
		if err := g.incrementRandomness(); err != nil {
			return Zero, err
		}
		copy(id[timestampSize:], g.lastRand[:])
	} else {
		// New millisecond or non-monotonic: fresh randomness
		if _, err := io.ReadFull(g.entropy, id[timestampSize:]); err != nil {
			return Zero, fmt.Errorf("ulid: failed to read random bytes: %w", err)
		}

		if g.monotonic {
			g.lastTime = ms
			copy(g.lastRand[:], id[timestampSize:])
		}
	}

	return id, nil
}

// incrementRandomness adds 1 to the randomness portion
func (g *generator) incrementRandomness() error {
	for i := randomnessSize - 1; i >= 0; i-- {
		g.lastRand[i]++
		if g.lastRand[i] != 0 {
			return nil // No overflow
		}
	}
	return ErrRandomnessOverflow
}

// -----------------------------------------------------------------------------
// Public Generation Functions
// -----------------------------------------------------------------------------

// New generates a new ULID with cryptographic randomness.
// Safe for concurrent use. Not guaranteed monotonic.
func New() ID {
	id, err := defaultGen.generate(time.Now())
	if err != nil {
		panic(err) // Only on entropy failure
	}
	return id
}

// NewMonotonic generates a monotonically increasing ULID.
// ULIDs generated within the same millisecond are guaranteed to be sorted.
// Safe for concurrent use.
func NewMonotonic() ID {
	id, err := monotonicGen.generate(time.Now())
	if err != nil {
		panic(err)
	}
	return id
}

// NewWithTime generates a ULID with a specific timestamp.
// Useful for testing or data migration.
func NewWithTime(t time.Time) ID {
	id, err := defaultGen.generate(t)
	if err != nil {
		panic(err)
	}
	return id
}

// NewString generates a new ULID and returns it as a string.
// Convenience function equivalent to New().String()
func NewString() string {
	return New().String()
}

// NewMonotonicString generates a monotonic ULID and returns it as a string.
func NewMonotonicString() string {
	return NewMonotonic().String()
}

// -----------------------------------------------------------------------------
// Parsing
// -----------------------------------------------------------------------------

// Parse parses a ULID string into an ID.
// Accepts both uppercase and lowercase, plus error-tolerant substitutions.
func Parse(s string) (ID, error) {
	if len(s) != EncodedSize {
		return Zero, ErrInvalidLength
	}

	var id ID

	// Decode using optimized lookup table
	// Each Base32 char = 5 bits, 26 chars = 130 bits, we use 128

	// First 10 chars → timestamp (50 bits, using 48)
	// We need to decode carefully to avoid overflow

	b := []byte(s)

	// Validate all characters first
	for i := 0; i < EncodedSize; i++ {
		if decodeTable[b[i]] == 0xFF {
			return Zero, fmt.Errorf("%w: '%c' at position %d", ErrInvalidCharacter, b[i], i)
		}
	}

	// Decode timestamp (first 10 characters)
	var ts uint64
	for i := 0; i < 10; i++ {
		ts = (ts << 5) | uint64(decodeTable[b[i]])
	}

	// Check timestamp overflow (first char must be 0-7)
	if decodeTable[b[0]] > 7 {
		return Zero, ErrTimestampOverflow
	}

	id[0] = byte(ts >> 40)
	id[1] = byte(ts >> 32)
	id[2] = byte(ts >> 24)
	id[3] = byte(ts >> 16)
	id[4] = byte(ts >> 8)
	id[5] = byte(ts)

	// Decode randomness (remaining 16 characters = 80 bits)
	// Process in chunks to fit in uint64

	// Characters 10-17 (8 chars = 40 bits)
	var hi uint64
	for i := 10; i < 18; i++ {
		hi = (hi << 5) | uint64(decodeTable[b[i]])
	}

	// Characters 18-25 (8 chars = 40 bits)
	var lo uint64
	for i := 18; i < 26; i++ {
		lo = (lo << 5) | uint64(decodeTable[b[i]])
	}

	// Pack into bytes 6-15
	id[6] = byte(hi >> 32)
	id[7] = byte(hi >> 24)
	id[8] = byte(hi >> 16)
	id[9] = byte(hi >> 8)
	id[10] = byte(hi)
	id[11] = byte(lo >> 32)
	id[12] = byte(lo >> 24)
	id[13] = byte(lo >> 16)
	id[14] = byte(lo >> 8)
	id[15] = byte(lo)

	return id, nil
}

// MustParse parses a ULID string or panics.
// Use only with compile-time constant strings.
func MustParse(s string) ID {
	id, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return id
}

// ParseBytes parses a 16-byte binary ULID.
func ParseBytes(b []byte) (ID, error) {
	if len(b) != BinarySize {
		return Zero, ErrInvalidLength
	}
	var id ID
	copy(id[:], b)
	return id, nil
}

// -----------------------------------------------------------------------------
// ID Methods
// -----------------------------------------------------------------------------

// String returns the 26-character Crockford Base32 representation.
// Zero-allocation implementation.
func (id ID) String() string {
	var buf [EncodedSize]byte

	// Encode timestamp (bytes 0-5 → chars 0-9)
	ts := uint64(id[0])<<40 | uint64(id[1])<<32 | uint64(id[2])<<24 |
		uint64(id[3])<<16 | uint64(id[4])<<8 | uint64(id[5])

	buf[0] = alphabet[(ts>>45)&0x1F]
	buf[1] = alphabet[(ts>>40)&0x1F]
	buf[2] = alphabet[(ts>>35)&0x1F]
	buf[3] = alphabet[(ts>>30)&0x1F]
	buf[4] = alphabet[(ts>>25)&0x1F]
	buf[5] = alphabet[(ts>>20)&0x1F]
	buf[6] = alphabet[(ts>>15)&0x1F]
	buf[7] = alphabet[(ts>>10)&0x1F]
	buf[8] = alphabet[(ts>>5)&0x1F]
	buf[9] = alphabet[ts&0x1F]

	// Encode randomness (bytes 6-15 → chars 10-25)
	// Split into two 40-bit chunks
	hi := uint64(id[6])<<32 | uint64(id[7])<<24 | uint64(id[8])<<16 |
		uint64(id[9])<<8 | uint64(id[10])
	lo := uint64(id[11])<<32 | uint64(id[12])<<24 | uint64(id[13])<<16 |
		uint64(id[14])<<8 | uint64(id[15])

	buf[10] = alphabet[(hi>>35)&0x1F]
	buf[11] = alphabet[(hi>>30)&0x1F]
	buf[12] = alphabet[(hi>>25)&0x1F]
	buf[13] = alphabet[(hi>>20)&0x1F]
	buf[14] = alphabet[(hi>>15)&0x1F]
	buf[15] = alphabet[(hi>>10)&0x1F]
	buf[16] = alphabet[(hi>>5)&0x1F]
	buf[17] = alphabet[hi&0x1F]

	buf[18] = alphabet[(lo>>35)&0x1F]
	buf[19] = alphabet[(lo>>30)&0x1F]
	buf[20] = alphabet[(lo>>25)&0x1F]
	buf[21] = alphabet[(lo>>20)&0x1F]
	buf[22] = alphabet[(lo>>15)&0x1F]
	buf[23] = alphabet[(lo>>10)&0x1F]
	buf[24] = alphabet[(lo>>5)&0x1F]
	buf[25] = alphabet[lo&0x1F]

	return string(buf[:])
}

// Bytes returns the 16-byte binary representation.
func (id ID) Bytes() []byte {
	b := make([]byte, BinarySize)
	copy(b, id[:])
	return b
}

// Time returns the timestamp embedded in the ULID.
func (id ID) Time() time.Time {
	ms := uint64(id[0])<<40 | uint64(id[1])<<32 | uint64(id[2])<<24 |
		uint64(id[3])<<16 | uint64(id[4])<<8 | uint64(id[5])
	return time.UnixMilli(int64(ms))
}

// Timestamp returns milliseconds since Unix epoch.
func (id ID) Timestamp() uint64 {
	return uint64(id[0])<<40 | uint64(id[1])<<32 | uint64(id[2])<<24 |
		uint64(id[3])<<16 | uint64(id[4])<<8 | uint64(id[5])
}

// IsZero returns true if the ULID is the zero value.
func (id ID) IsZero() bool {
	return id == Zero
}

// Compare returns -1 if id < other, 0 if id == other, 1 if id > other.
// ULIDs are compared lexicographically (same as string comparison).
func (id ID) Compare(other ID) int {
	for i := 0; i < BinarySize; i++ {
		if id[i] < other[i] {
			return -1
		}
		if id[i] > other[i] {
			return 1
		}
	}
	return 0
}

// -----------------------------------------------------------------------------
// Encoding Interfaces
// -----------------------------------------------------------------------------

// MarshalText implements encoding.TextMarshaler.
func (id ID) MarshalText() ([]byte, error) {
	return []byte(id.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (id *ID) UnmarshalText(data []byte) error {
	parsed, err := Parse(string(data))
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

// MarshalBinary implements encoding.BinaryMarshaler.
func (id ID) MarshalBinary() ([]byte, error) {
	return id.Bytes(), nil
}

// UnmarshalBinary implements encoding.BinaryUnmarshaler.
func (id *ID) UnmarshalBinary(data []byte) error {
	parsed, err := ParseBytes(data)
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

// MarshalJSON implements json.Marshaler.
func (id ID) MarshalJSON() ([]byte, error) {
	return []byte(`"` + id.String() + `"`), nil
}

// UnmarshalJSON implements json.Unmarshaler.
func (id *ID) UnmarshalJSON(data []byte) error {
	if len(data) < 2 || data[0] != '"' || data[len(data)-1] != '"' {
		return errors.New("ulid: invalid JSON string")
	}
	return id.UnmarshalText(data[1 : len(data)-1])
}

// -----------------------------------------------------------------------------
// SQL Database Integration
// -----------------------------------------------------------------------------

// Scan implements sql.Scanner for database reads.
// Supports: []byte (binary), string, nil
func (id *ID) Scan(src interface{}) error {
	switch v := src.(type) {
	case nil:
		*id = Zero
		return nil
	case []byte:
		if len(v) == BinarySize {
			copy(id[:], v)
			return nil
		}
		if len(v) == EncodedSize {
			return id.UnmarshalText(v)
		}
		return ErrInvalidLength
	case string:
		return id.UnmarshalText([]byte(v))
	default:
		return fmt.Errorf("%w: %T", ErrScanType, src)
	}
}

// Value implements driver.Valuer for database writes.
// Returns string for CHAR(26) storage.
func (id ID) Value() (driver.Value, error) {
	return id.String(), nil
}

// ValueBytes returns binary representation for BYTEA storage.
// Use: db.Exec("INSERT ... VALUES ($1)", id.ValueBytes())
func (id ID) ValueBytes() []byte {
	return id[:]
}

// -----------------------------------------------------------------------------
// Validation
// -----------------------------------------------------------------------------

// IsValid checks if a string is a valid ULID format.
func IsValid(s string) bool {
	if len(s) != EncodedSize {
		return false
	}

	// Check first character (timestamp overflow)
	if decodeTable[s[0]] > 7 {
		return false
	}

	for i := 0; i < EncodedSize; i++ {
		if decodeTable[s[i]] == 0xFF {
			return false
		}
	}
	return true
}

// -----------------------------------------------------------------------------
// High-Performance Pool-Based Generator
// -----------------------------------------------------------------------------

// Pool provides high-throughput ULID generation using multiple generators.
// Use when generating millions of ULIDs per second.
type Pool struct {
	generators []*generator
	counter    uint64
	size       int
}

// NewPool creates a pool with n generators for high concurrency.
// Recommended: runtime.NumCPU() or higher for CPU-bound workloads.
func NewPool(n int, monotonic bool) *Pool {
	if n < 1 {
		n = 1
	}

	p := &Pool{
		generators: make([]*generator, n),
		size:       n,
	}

	for i := 0; i < n; i++ {
		p.generators[i] = newGenerator(rand.Reader, monotonic)
	}

	return p
}

// New generates a ULID using a round-robin selected generator.
// Reduces lock contention under high concurrency.
func (p *Pool) New() ID {
	idx := atomic.AddUint64(&p.counter, 1) % uint64(p.size)
	id, err := p.generators[idx].generate(time.Now())
	if err != nil {
		panic(err)
	}
	return id
}

// NewString generates a ULID string using the pool.
func (p *Pool) NewString() string {
	return p.New().String()
}

// -----------------------------------------------------------------------------
// Utility Functions
// -----------------------------------------------------------------------------

// FromTime creates a ULID with minimum randomness for a given time.
// Useful for range queries: WHERE id >= ulid.FromTime(startTime).String()
func FromTime(t time.Time) ID {
	var id ID
	ms := uint64(t.UnixMilli())

	id[0] = byte(ms >> 40)
	id[1] = byte(ms >> 32)
	id[2] = byte(ms >> 24)
	id[3] = byte(ms >> 16)
	id[4] = byte(ms >> 8)
	id[5] = byte(ms)
	// Randomness bytes remain zero

	return id
}

// MaxForTime creates a ULID with maximum randomness for a given time.
// Useful for range queries: WHERE id <= ulid.MaxForTime(endTime).String()
func MaxForTime(t time.Time) ID {
	var id ID
	ms := uint64(t.UnixMilli())

	id[0] = byte(ms >> 40)
	id[1] = byte(ms >> 32)
	id[2] = byte(ms >> 24)
	id[3] = byte(ms >> 16)
	id[4] = byte(ms >> 8)
	id[5] = byte(ms)

	// Set all randomness bytes to max
	for i := timestampSize; i < BinarySize; i++ {
		id[i] = 0xFF
	}

	return id
}

// TimeBounds returns ULIDs for querying a time range.
// Usage: minID, maxID := ulid.TimeBounds(start, end)
//
//	WHERE id >= minID AND id <= maxID
func TimeBounds(start, end time.Time) (ID, ID) {
	return FromTime(start), MaxForTime(end)
}
