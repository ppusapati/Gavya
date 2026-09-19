// Every identifier in this platform comes from here, and until now nothing
// tested it.
//
// libs/integrity/sys.IDs.New delegates straight to ulid.New(), and about a
// hundred and twenty call sites across twenty-nine services call that for every
// primary key, every audit row and every settlement line. The happy path was
// therefore exercised constantly — the end-to-end suite alone generates
// thousands of identifiers per run — and the edges were exercised by nothing.
//
// Writing this found three defects, each of which is a comment in ULID.go now:
// 'U' decoded as 5 so two strings parsed to the same identifier; the two range
// helpers turned a time before 1970 into a bound in the year 8920; and a pool
// built with monotonic true is not monotonic. The first two are fixed and tested
// below. The third is arithmetic rather than a defect, and is pinned by a test
// that states what it actually does.
//
// The suite is written against the exported behaviour plus the two unexported
// pieces that carry the risk — the decode table and the generator — because the
// generator is where entropy failure and clock movement live, and neither can be
// reached through New() without breaking the process.
package ulid

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// The decode table, which is where the first defect was
// -----------------------------------------------------------------------------

// Each character of the alphabet decodes to its own position, in either case.
//
// Not a restatement of the loop that builds it: the loop derived the lowercase
// entry arithmetically and got it wrong for digits, and this is the check that
// would have said so.
func TestEveryAlphabetCharacterDecodesToItsOwnPosition(t *testing.T) {
	for i, c := range alphabet {
		if got := decodeTable[c]; got != byte(i) {
			t.Errorf("%q decodes to %d and is at position %d", c, got, i)
		}
		lower := byte(c) | 0x20
		if got := decodeTable[lower]; got != byte(i) {
			t.Errorf("%q, the lower case of %q, decodes to %d and should decode to %d",
				lower, c, got, i)
		}
	}
}

// The four letters Crockford leaves out, and what each one does here.
//
// I, L and O are the documented tolerance: a person reading an identifier off a
// screen writes I for 1 and O for 0, and accepting those is deliberate. U is not
// in that list and never was — it was accepted because 5 + 32 is U.
func TestTheLettersCrockfordLeavesOut(t *testing.T) {
	tolerated := map[byte]byte{'I': 1, 'i': 1, 'L': 1, 'l': 1, 'O': 0, 'o': 0}
	for c, want := range tolerated {
		if got := decodeTable[c]; got != want {
			t.Errorf("%q decodes to %d; it is meant to be tolerated as %d", c, got, want)
		}
	}

	// U is not tolerated, in either case. It used to be tolerated in one.
	for _, c := range []byte{'U', 'u'} {
		if decodeTable[c] != 0xFF {
			t.Errorf("%q decodes to %d rather than being refused. It is not one of "+
				"Crockford's substitutions; it was reachable because '5'+32 is 'U', "+
				"which made two different strings parse to the same identifier.",
				c, decodeTable[c])
		}
	}
}

// Nothing outside the alphabet and the three substitutions decodes at all.
//
// The whole byte range, because the defect that prompted this suite was an entry
// nobody put there on purpose.
func TestNothingElseDecodes(t *testing.T) {
	valid := map[byte]bool{}
	for _, c := range alphabet {
		valid[byte(c)] = true
		valid[byte(c)|0x20] = true
	}
	for _, c := range []byte{'I', 'i', 'L', 'l', 'O', 'o'} {
		valid[c] = true
	}

	for i := 0; i < 256; i++ {
		c := byte(i)
		if valid[c] {
			continue
		}
		if decodeTable[c] != 0xFF {
			t.Errorf("byte %d (%q) decodes to %d and is not an alphabet character "+
				"nor one of the three substitutions", i, c, decodeTable[c])
		}
	}
}

// A string with U in it no longer parses, and used to parse as the same
// identifier as the string with 5 in it.
func TestUIsNoLongerAnAliasForFive(t *testing.T) {
	withFive := "01ARZ3NDEKTSV4RRFFQ69G5FA5"
	withU := "01ARZ3NDEKTSV4RRFFQ69G5FAU"

	five, err := Parse(withFive)
	if err != nil {
		t.Fatalf("parse %s: %v", withFive, err)
	}
	if _, err := Parse(withU); !errors.Is(err, ErrInvalidCharacter) {
		t.Errorf("Parse(%q) returned %v; it must refuse, because it used to return "+
			"the identifier %s parses to, which makes two strings one identifier",
			withU, err, withFive)
	}
	if IsValid(withU) {
		t.Errorf("IsValid(%q) is true", withU)
	}
	// And the string that is meant to work still does.
	if five.String() != withFive {
		t.Errorf("%s round-tripped to %s", withFive, five.String())
	}
}

// -----------------------------------------------------------------------------
// The round trip, which is the property everything else rests on
// -----------------------------------------------------------------------------

// An identifier survives being written down and read back.
//
// Across the boundaries as well as the middle: all-zero, all-ones in the random
// part, the largest timestamp a ULID can carry, and a few thousand real ones.
func TestAnIdentifierSurvivesTheRoundTrip(t *testing.T) {
	cases := map[string]ID{
		"zero":             Zero,
		"max randomness":   MaxForTime(time.UnixMilli(0)),
		"max timestamp":    MaxForTime(time.UnixMilli(maxTimestamp)),
		"epoch, no random": FromTime(time.UnixMilli(0)),
	}
	for name, id := range cases {
		t.Run(name, func(t *testing.T) {
			s := id.String()
			if len(s) != EncodedSize {
				t.Fatalf("%s encoded to %d characters", name, len(s))
			}
			back, err := Parse(s)
			if err != nil {
				t.Fatalf("parse %q: %v", s, err)
			}
			if back != id {
				t.Errorf("%s round-tripped to a different identifier:\n  before %v\n  after  %v",
					name, id, back)
			}
		})
	}

	for i := 0; i < 5000; i++ {
		id := New()
		back, err := Parse(id.String())
		if err != nil {
			t.Fatalf("parse %q: %v", id.String(), err)
		}
		if back != id {
			t.Fatalf("a generated identifier did not survive: %v became %v", id, back)
		}
	}
}

// What a generated identifier looks like, because the database column holds 26
// characters and nothing else checks.
func TestAGeneratedIdentifierFitsTheColumn(t *testing.T) {
	for i := 0; i < 1000; i++ {
		s := NewString()
		if len(s) != 26 {
			t.Fatalf("NewString returned %d characters: %q", len(s), s)
		}
		for j := 0; j < len(s); j++ {
			if !strings.ContainsRune(alphabet, rune(s[j])) {
				t.Fatalf("NewString returned %q, which has %q at position %d — not a "+
					"character this alphabet emits", s, s[j], j)
			}
		}
	}
}

// The two string conveniences agree with the identifiers they wrap.
func TestTheStringConveniences(t *testing.T) {
	if s := NewString(); !IsValid(s) {
		t.Errorf("NewString returned %q, which does not parse", s)
	}
	prev := NewMonotonicString()
	for i := 0; i < 200; i++ {
		next := NewMonotonicString()
		if !IsValid(next) {
			t.Fatalf("NewMonotonicString returned %q, which does not parse", next)
		}
		// The strings sort the way the identifiers do, which is the only reason
		// to have a monotonic string at all.
		if next <= prev {
			t.Fatalf("NewMonotonicString went backwards at %d:\n  %s\n  %s", i, prev, next)
		}
		prev = next
	}
}

// -----------------------------------------------------------------------------
// Parsing, and what it refuses
// -----------------------------------------------------------------------------

func TestParseRefuses(t *testing.T) {
	valid := New().String()

	cases := []struct {
		name string
		in   string
		want error
	}{
		{"empty", "", ErrInvalidLength},
		{"one short", valid[:25], ErrInvalidLength},
		{"one long", valid + "0", ErrInvalidLength},
		{"a character that is not in the alphabet", "01ARZ3NDEKTSV4RRFFQ69G5FA!", ErrInvalidCharacter},
		{"a space", "01ARZ3NDEKTSV4RRFFQ69G5FA ", ErrInvalidCharacter},
		// The first character carries the top three bits of a 48-bit timestamp,
		// so it can only be 0 through 7. Anything above that is a timestamp no
		// ULID can hold, and decoding it would silently truncate.
		{"a timestamp that does not fit", "81ARZ3NDEKTSV4RRFFQ69G5FA5", ErrTimestampOverflow},
		{"the largest first character", "Z1ARZ3NDEKTSV4RRFFQ69G5FA5", ErrTimestampOverflow},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id, err := Parse(c.in)
			if !errors.Is(err, c.want) {
				t.Errorf("Parse(%q) = %v, want %v", c.in, err, c.want)
			}
			if id != Zero {
				t.Errorf("Parse(%q) returned %v alongside its error; a refused parse "+
					"must hand back nothing usable", c.in, id)
			}
		})
	}
}

// IsValid and Parse answer the same question.
//
// Two implementations of one rule, which is the arrangement that drifts. IsValid
// is the cheap one a caller reaches for first, and a disagreement means
// something passes validation and then fails to parse.
func TestIsValidAgreesWithParse(t *testing.T) {
	valid := New().String()
	for _, s := range []string{
		valid,
		strings.ToLower(valid),
		"",
		valid[:25],
		valid + "0",
		"01ARZ3NDEKTSV4RRFFQ69G5FA!",
		"01ARZ3NDEKTSV4RRFFQ69G5FAU",
		"81ARZ3NDEKTSV4RRFFQ69G5FA5",
		"01ARZ3NDEKTSV4RRFFQ69G5FAI",
		"01ARZ3NDEKTSV4RRFFQ69G5FAO",
	} {
		_, err := Parse(s)
		if got, want := IsValid(s), err == nil; got != want {
			t.Errorf("IsValid(%q) = %v and Parse returned %v", s, got, err)
		}
	}
}

// Lower case parses, and means the same thing.
func TestLowerCaseParsesToTheSameIdentifier(t *testing.T) {
	id := New()
	s := id.String()
	lower, err := Parse(strings.ToLower(s))
	if err != nil {
		t.Fatalf("parse the lower-case form: %v", err)
	}
	if lower != id {
		t.Errorf("%q and %q are different identifiers", s, strings.ToLower(s))
	}
}

// MustParse panics rather than returning, which is the whole of its contract.
func TestMustParsePanicsOnSomethingThatIsNotOne(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustParse returned on an invalid string")
		}
	}()
	MustParse("not a ulid")
}

func TestParseBytes(t *testing.T) {
	id := New()
	back, err := ParseBytes(id.Bytes())
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if back != id {
		t.Errorf("ParseBytes round trip changed the identifier")
	}
	for _, b := range [][]byte{nil, {}, make([]byte, 15), make([]byte, 17)} {
		if _, err := ParseBytes(b); !errors.Is(err, ErrInvalidLength) {
			t.Errorf("ParseBytes(%d bytes) = %v, want %v", len(b), err, ErrInvalidLength)
		}
	}
}

// Bytes hands back a copy, not the identifier's own storage.
func TestBytesIsACopy(t *testing.T) {
	id := New()
	b := id.Bytes()
	b[0] ^= 0xFF
	if id.Bytes()[0] == b[0] {
		t.Error("writing to the slice Bytes returned changed the identifier it came from")
	}
}

// -----------------------------------------------------------------------------
// The timestamp
// -----------------------------------------------------------------------------

func TestTheTimestampComesBackOut(t *testing.T) {
	for _, ms := range []int64{0, 1, 1_700_000_000_000, maxTimestamp} {
		at := time.UnixMilli(ms)
		id := NewWithTime(at)
		if got := id.Timestamp(); got != uint64(ms) {
			t.Errorf("Timestamp() = %d for a ULID made at %d", got, ms)
		}
		if got := id.Time().UnixMilli(); got != ms {
			t.Errorf("Time() = %d for a ULID made at %d", got, ms)
		}
	}
}

// A ULID cannot carry a time before the epoch, and says so rather than wrapping.
//
// generate() refuses; the range helpers clamp. The difference is deliberate and
// is the subject of the two tests below.
func TestAGeneratorRefusesATimeItCannotCarry(t *testing.T) {
	g := newGenerator(bytes.NewReader(make([]byte, 64)), false)

	for _, name := range []string{"the zero time", "before the epoch", "past the ceiling"} {
		var at time.Time
		switch name {
		case "the zero time":
			at = time.Time{}
		case "before the epoch":
			at = time.UnixMilli(-1)
		case "past the ceiling":
			at = time.UnixMilli(maxTimestamp + 1)
		}
		if _, err := g.generate(at); !errors.Is(err, ErrTimestampOverflow) {
			t.Errorf("generate(%s) = %v, want %v", name, err, ErrTimestampOverflow)
		}
	}
}

// -----------------------------------------------------------------------------
// The range helpers, which is where the second defect was
// -----------------------------------------------------------------------------

// A bound outside what a ULID can carry is clamped to the end, not wrapped.
//
// FromTime(time.Time{}) used to return an identifier dated the year 8920,
// because uint64 of a negative UnixMilli is enormous. As the lower bound of a
// range query that silently matches nothing — the worst shape of wrong, since
// an empty result looks like an answer.
func TestABoundOutsideTheRangeIsClamped(t *testing.T) {
	for _, c := range []struct {
		name string
		at   time.Time
		want uint64
	}{
		{"the zero time", time.Time{}, 0},
		{"a millisecond before the epoch", time.UnixMilli(-1), 0},
		{"a year before the epoch", time.UnixMilli(-31_536_000_000), 0},
		{"past the ceiling", time.UnixMilli(maxTimestamp + 1), maxTimestamp},
		{"far past the ceiling", time.UnixMilli(maxTimestamp * 1000), maxTimestamp},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := FromTime(c.at).Timestamp(); got != c.want {
				t.Errorf("FromTime(%s).Timestamp() = %d, want %d", c.name, got, c.want)
			}
			if got := MaxForTime(c.at).Timestamp(); got != c.want {
				t.Errorf("MaxForTime(%s).Timestamp() = %d, want %d", c.name, got, c.want)
			}
		})
	}
}

// The bounds actually bound: every identifier issued in the window sorts inside
// them, and one issued outside does not.
func TestTheBoundsBound(t *testing.T) {
	start := time.UnixMilli(1_700_000_000_000)
	end := start.Add(time.Second)
	lo, hi := TimeBounds(start, end)

	if lo.Compare(hi) >= 0 {
		t.Fatalf("the lower bound %s is not below the upper bound %s", lo, hi)
	}

	inside := NewWithTime(start.Add(500 * time.Millisecond))
	if inside.Compare(lo) <= 0 || inside.Compare(hi) >= 0 {
		t.Errorf("an identifier from the middle of the window sorts outside it:\n"+
			"  lower  %s\n  inside %s\n  upper  %s", lo, inside, hi)
	}

	before := NewWithTime(start.Add(-time.Second))
	if before.Compare(lo) >= 0 {
		t.Errorf("an identifier from before the window does not sort below the lower bound")
	}
	after := NewWithTime(end.Add(time.Second))
	if after.Compare(hi) <= 0 {
		t.Errorf("an identifier from after the window does not sort above the upper bound")
	}
}

// -----------------------------------------------------------------------------
// Sorting, which is the L in ULID
// -----------------------------------------------------------------------------

// Comparing the bytes and comparing the strings give the same order.
//
// This is the property the whole format exists for: an index on a CHAR(26)
// column is in the order the rows were created. If the two disagreed, sorting in
// the database and sorting in Go would disagree.
func TestByteOrderAndStringOrderAgree(t *testing.T) {
	ids := make([]ID, 500)
	for i := range ids {
		ids[i] = NewWithTime(time.UnixMilli(int64(1_700_000_000_000 + i%17)))
	}
	for i := range ids {
		for j := range ids {
			byBytes := ids[i].Compare(ids[j])
			byString := strings.Compare(ids[i].String(), ids[j].String())
			if byBytes != byString {
				t.Fatalf("Compare says %d and the strings say %d for\n  %s\n  %s",
					byBytes, byString, ids[i], ids[j])
			}
		}
	}
}

// Identifiers issued in order sort in order, across milliseconds.
func TestIdentifiersSortIntoTheOrderTheyWereIssued(t *testing.T) {
	var ids []ID
	for ms := int64(0); ms < 200; ms++ {
		ids = append(ids, NewWithTime(time.UnixMilli(1_700_000_000_000+ms)))
	}
	sorted := append([]ID(nil), ids...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Compare(sorted[j]) < 0 })
	for i := range ids {
		if ids[i] != sorted[i] {
			t.Fatalf("identifier %d is out of order once sorted", i)
		}
	}
}

func TestCompareAndIsZero(t *testing.T) {
	if !Zero.IsZero() {
		t.Error("Zero.IsZero() is false")
	}
	if New().IsZero() {
		t.Error("a generated identifier reports itself as zero")
	}
	id := New()
	if id.Compare(id) != 0 {
		t.Error("an identifier does not compare equal to itself")
	}
}

// -----------------------------------------------------------------------------
// The monotonic generator
// -----------------------------------------------------------------------------

// Within one millisecond, each identifier is greater than the one before it.
//
// The claim the package makes, tested at a fixed instant so the millisecond
// cannot roll over underneath it.
func TestWithinOneMillisecondMonotonicMeansIncreasing(t *testing.T) {
	g := newGenerator(fixedEntropy{}, true)
	at := time.UnixMilli(1_700_000_000_000)

	prev, err := g.generate(at)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		next, err := g.generate(at)
		if err != nil {
			t.Fatalf("identifier %d: %v", i, err)
		}
		if next.Compare(prev) <= 0 {
			t.Fatalf("identifier %d is not above its predecessor:\n  %s\n  %s", i, prev, next)
		}
		prev = next
	}
}

// NewMonotonic, through the package's own global generator.
func TestNewMonotonicIncreases(t *testing.T) {
	prev := NewMonotonic()
	for i := 0; i < 5000; i++ {
		next := NewMonotonic()
		if next.Compare(prev) <= 0 {
			t.Fatalf("NewMonotonic went backwards at %d:\n  %s\n  %s", i, prev, next)
		}
		prev = next
	}
}

// The randomness runs out, and says so rather than wrapping to a smaller
// identifier.
//
// Eighty bits, so it cannot happen on a real clock; it can happen here because
// the entropy is chosen. What the test pins is that the answer is an error
// rather than an identifier below the one before it, which is the failure that
// would be invisible.
func TestTheRandomnessRunningOutIsAnError(t *testing.T) {
	g := newGenerator(allOnes{}, true)
	at := time.UnixMilli(1_700_000_000_000)

	if _, err := g.generate(at); err != nil {
		t.Fatalf("the first identifier of the millisecond: %v", err)
	}
	if _, err := g.generate(at); !errors.Is(err, ErrRandomnessOverflow) {
		t.Errorf("incrementing past the top of the random space returned %v, want %v",
			err, ErrRandomnessOverflow)
	}
}

// A clock that steps backwards is not covered by the guarantee, and this records
// what happens instead.
//
// The package says identifiers within the same millisecond are ordered, which is
// true and is a narrower claim than "monotonic" sounds. When the clock moves
// back — an NTP correction, a virtual machine resumed from a snapshot — the
// timestamp is lower and the identifier sorts below its predecessor.
//
// Written down rather than fixed. Holding the previous timestamp would issue
// identifiers stamped with a time they were not created at, and a settlement
// period is drawn from that stamp.
func TestAClockThatStepsBackwardsIsNotOrdered(t *testing.T) {
	g := newGenerator(fixedEntropy{}, true)
	at := time.UnixMilli(1_700_000_000_000)

	first, err := g.generate(at)
	if err != nil {
		t.Fatal(err)
	}
	second, err := g.generate(at.Add(-5 * time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if second.Compare(first) > 0 {
		t.Error("the generator now orders across a backwards clock step. That is an " +
			"improvement, and this test is the wrong shape for it — rewrite it to " +
			"assert the new behaviour rather than deleting it.")
	}
}

// Entropy that fails is reported, not ignored.
func TestEntropyThatFailsIsReported(t *testing.T) {
	g := newGenerator(failingEntropy{}, false)
	if _, err := g.generate(time.UnixMilli(1_700_000_000_000)); err == nil {
		t.Error("a generator whose entropy source fails returned an identifier")
	}
}

// -----------------------------------------------------------------------------
// The pool
// -----------------------------------------------------------------------------

func TestAPoolIssuesUsableIdentifiers(t *testing.T) {
	p := NewPool(4, false)
	seen := map[ID]bool{}
	for i := 0; i < 5000; i++ {
		id := p.New()
		if id.IsZero() {
			t.Fatal("the pool issued the zero identifier")
		}
		if seen[id] {
			t.Fatalf("the pool issued %s twice", id)
		}
		seen[id] = true
	}
	if !IsValid(p.NewString()) {
		t.Error("Pool.NewString did not produce a valid identifier")
	}
}

// A pool asked for fewer than one generator still works.
func TestAPoolOfNothingIsAPoolOfOne(t *testing.T) {
	for _, n := range []int{0, -1, -100} {
		p := NewPool(n, false)
		if p.size != 1 {
			t.Errorf("NewPool(%d) built %d generators", n, p.size)
		}
		if p.New().IsZero() {
			t.Errorf("NewPool(%d) issued the zero identifier", n)
		}
	}
}

// A monotonic pool is not monotonic, and this is the measurement that says so.
//
// The flag is passed to each generator and each keeps its own state, so
// consecutive identifiers come from different generators and are in no order.
// The figure is stated in the Pool documentation and this is where it comes
// from; if somebody makes the pool ordered, this fails and the documentation
// stops being true at the same moment.
func TestAMonotonicPoolIsNotMonotonicAcrossItsGenerators(t *testing.T) {
	p := NewPool(4, true)
	prev := p.New()
	outOfOrder := 0
	const n = 2000
	for i := 0; i < n; i++ {
		next := p.New()
		if next.Compare(prev) <= 0 {
			outOfOrder++
		}
		prev = next
	}
	if outOfOrder == 0 {
		t.Error("every identifier from a four-generator monotonic pool was above its " +
			"predecessor. Either the pool is ordered now — in which case the caveat " +
			"in the Pool documentation is wrong and should go — or this test is not " +
			"measuring what it thinks.")
	}
}

// -----------------------------------------------------------------------------
// Encoding interfaces
// -----------------------------------------------------------------------------

func TestTextAndBinaryAndJSONRoundTrip(t *testing.T) {
	id := New()

	text, err := id.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	var fromText ID
	if err := fromText.UnmarshalText(text); err != nil {
		t.Fatal(err)
	}
	if fromText != id {
		t.Error("the text round trip changed the identifier")
	}

	binary, err := id.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var fromBinary ID
	if err := fromBinary.UnmarshalBinary(binary); err != nil {
		t.Fatal(err)
	}
	if fromBinary != id {
		t.Error("the binary round trip changed the identifier")
	}

	// Through encoding/json, and inside a struct, because that is how it is used.
	type row struct {
		ID ID `json:"id"`
	}
	encoded, err := json.Marshal(row{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf(`{"id":"%s"}`, id); string(encoded) != want {
		t.Errorf("marshalled to %s, want %s", encoded, want)
	}
	var decoded row
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != id {
		t.Error("the JSON round trip changed the identifier")
	}
}

func TestBadInputToTheUnmarshallers(t *testing.T) {
	var id ID
	if err := id.UnmarshalText([]byte("short")); err == nil {
		t.Error("UnmarshalText accepted a string that is not an identifier")
	}
	if err := id.UnmarshalBinary(make([]byte, 3)); err == nil {
		t.Error("UnmarshalBinary accepted three bytes")
	}
	for _, s := range []string{``, `"`, `null`, `123`, `"short"`} {
		if err := id.UnmarshalJSON([]byte(s)); err == nil {
			t.Errorf("UnmarshalJSON accepted %s", s)
		}
	}
}

// -----------------------------------------------------------------------------
// The database boundary
// -----------------------------------------------------------------------------

// Scan takes back what Value wrote, and the three other shapes a driver hands
// over.
func TestScanTakesBackWhatValueWrote(t *testing.T) {
	id := New()

	v, err := id.Value()
	if err != nil {
		t.Fatal(err)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("Value returned %T; the column is CHAR(26)", v)
	}

	var fromValue ID
	if err := fromValue.Scan(s); err != nil {
		t.Fatal(err)
	}
	if fromValue != id {
		t.Error("scanning what Value wrote produced a different identifier")
	}

	// A driver may hand the same text back as bytes, and a BYTEA column hands
	// back sixteen.
	var fromTextBytes, fromBinary ID
	if err := fromTextBytes.Scan([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if fromTextBytes != id {
		t.Error("scanning the 26-byte text form produced a different identifier")
	}
	if err := fromBinary.Scan(id.ValueBytes()); err != nil {
		t.Fatal(err)
	}
	if fromBinary != id {
		t.Error("scanning the 16-byte binary form produced a different identifier")
	}
}

// A NULL column becomes the zero identifier rather than an error.
//
// Deliberate, and worth pinning: a nullable foreign key that is not set reads as
// the zero value, and a caller checks IsZero. An error here would make every
// unset optional reference a failed row scan.
func TestScanningNullGivesTheZeroIdentifier(t *testing.T) {
	id := New()
	if err := id.Scan(nil); err != nil {
		t.Fatalf("scanning NULL: %v", err)
	}
	if !id.IsZero() {
		t.Error("scanning NULL left the identifier unchanged")
	}
}

func TestScanRefusesWhatItCannotRead(t *testing.T) {
	var id ID
	if err := id.Scan(make([]byte, 7)); !errors.Is(err, ErrInvalidLength) {
		t.Errorf("Scan(7 bytes) = %v, want %v", err, ErrInvalidLength)
	}
	for _, src := range []any{42, 3.5, true, time.Now(), struct{}{}} {
		if err := id.Scan(src); !errors.Is(err, ErrScanType) {
			t.Errorf("Scan(%T) = %v, want %v", src, err, ErrScanType)
		}
	}
}

// -----------------------------------------------------------------------------
// Concurrency
// -----------------------------------------------------------------------------

// Every service generates identifiers from many goroutines at once, so this is
// the shape the platform actually uses. Run with -race.
func TestConcurrentGenerationIsSafeAndUnique(t *testing.T) {
	const goroutines, each = 16, 500

	var wg sync.WaitGroup
	out := make([][]ID, goroutines)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			mine := make([]ID, each)
			for i := range mine {
				if g%2 == 0 {
					mine[i] = New()
				} else {
					mine[i] = NewMonotonic()
				}
			}
			out[g] = mine
		}(g)
	}
	wg.Wait()

	seen := make(map[ID]bool, goroutines*each)
	for _, batch := range out {
		for _, id := range batch {
			if id.IsZero() {
				t.Fatal("a goroutine was handed the zero identifier")
			}
			if seen[id] {
				t.Fatalf("%s was issued twice", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != goroutines*each {
		t.Errorf("%d identifiers issued, %d distinct", goroutines*each, len(seen))
	}
}

// -----------------------------------------------------------------------------
// Entropy sources the tests choose
// -----------------------------------------------------------------------------

// fixedEntropy always reads the same bytes, so a test can compare two
// identifiers and know the difference is not the randomness.
type fixedEntropy struct{}

func (fixedEntropy) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0x7F
	}
	return len(p), nil
}

// allOnes puts the generator at the top of the random space, where the next
// increment overflows.
type allOnes struct{}

func (allOnes) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0xFF
	}
	return len(p), nil
}

// failingEntropy is a source that has stopped working.
type failingEntropy struct{}

func (failingEntropy) Read([]byte) (int, error) {
	return 0, errors.New("no entropy available")
}
