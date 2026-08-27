package origin

import (
	"errors"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// What an origin must say about itself
// ---------------------------------------------------------------------------

// An imported record without a payload hash cannot be told apart on re-delivery
// from one that was amended. That is the whole reason the field is required, and
// it is the property the shadow-mode import path rests on.
func TestAnImportedRecordMustCarryItsPayloadHash(t *testing.T) {
	_, err := NewImported("INCUMBENT", "BATCH-1", "REC-1", "")
	if err == nil {
		t.Fatal("an imported record with no payload hash was accepted; a re-delivery of it " +
			"would be indistinguishable from an amendment of it")
	}
	if !strings.Contains(err.Error(), "source_payload_hash") {
		t.Errorf("the refusal is %q and does not name the field that is missing", err)
	}
}

// Every one of the four is required, and the refusal names all the missing ones
// at once rather than sending somebody round the loop four times.
func TestAnImportedRecordNamesEveryMissingField(t *testing.T) {
	o := Origin{Kind: Imported}
	err := o.Validate()
	if err == nil {
		t.Fatal("an imported origin with nothing on it was accepted")
	}
	for _, field := range []string{
		"source_system_id", "import_batch_id", "source_record_id", "source_payload_hash",
	} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("the refusal is %q and does not mention %s, so whoever reads it fixes "+
				"one field and comes straight back", err, field)
		}
	}
}

// A native record needs none of that: nothing was imported, so there is no
// source to name. Without this the import fields would have to be filled in with
// something for every record the platform captured itself, and whatever went in
// would be a lie about provenance.
func TestANativeRecordNeedsNoSource(t *testing.T) {
	if err := NewNative().Validate(); err != nil {
		t.Errorf("a native record was refused: %v", err)
	}
	if !NewNative().IsNative() || NewNative().IsImported() || NewNative().IsDerived() {
		t.Error("a native record does not report itself as native")
	}
}

// A derived record must say what derived it. A computed figure whose derivation
// nobody recorded cannot be recomputed, and a shadow settlement that cannot be
// recomputed is not evidence of anything.
func TestADerivedRecordMustSayWhatDerivedIt(t *testing.T) {
	if _, err := NewDerived(""); err == nil {
		t.Error("a derived record with no derivation was accepted")
	}
	o, err := NewDerived("shadow-settlement-v3")
	if err != nil {
		t.Fatalf("a derived record with a derivation was refused: %v", err)
	}
	if !o.IsDerived() {
		t.Error("a derived record does not report itself as derived")
	}
}

// An origin with no kind at all, or a kind nobody defined, is refused rather
// than treated as one of the three. A record whose provenance defaulted to
// NATIVE would be an imported figure the platform claims it measured itself.
func TestAnUnknownKindIsRefusedRatherThanDefaulted(t *testing.T) {
	for _, k := range []Kind{"", "GUESSED", "native "} {
		if err := (Origin{Kind: k}).Validate(); !errors.Is(err, ErrInvalidKind) {
			t.Errorf("kind %q was accepted (%v); a record whose provenance defaults is an "+
				"imported figure the platform would claim it measured itself", k, err)
		}
	}
}

// Parsing is forgiving about case and surrounding space — an incumbent system's
// export writes "imported", " NATIVE ", whatever it writes — and unforgiving
// about anything else.
func TestParseKindIsForgivingAboutFormAndNotAboutMeaning(t *testing.T) {
	for _, in := range []string{"NATIVE", "native", "  Native  ", "nAtIvE"} {
		got, err := ParseKind(in)
		if err != nil || got != Native {
			t.Errorf("ParseKind(%q) = %q, %v; want NATIVE", in, got, err)
		}
	}
	for _, in := range []string{"", "unknown", "NATIVE RECORD", "IMPORT"} {
		if _, err := ParseKind(in); !errors.Is(err, ErrInvalidKind) {
			t.Errorf("ParseKind(%q) was accepted", in)
		}
	}
}

// The fields are trimmed on the way in, so a source system's trailing space does
// not produce two origins that are the same record and do not compare equal.
func TestSurroundingSpaceIsTrimmedFromImportFields(t *testing.T) {
	a, err := NewImported(" INCUMBENT ", " BATCH-1 ", " REC-1 ", " sha256:abc ")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	b, err := NewImported("INCUMBENT", "BATCH-1", "REC-1", "sha256:abc")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if a != b {
		t.Errorf("the same record delivered with and without surrounding space produced two "+
			"different origins:\n  %+v\n  %+v", a, b)
	}
}

// ---------------------------------------------------------------------------
// The payload hash
// ---------------------------------------------------------------------------

// The same bytes hash the same, different bytes do not. Without the first a
// re-delivery looks like an amendment; without the second an amendment looks
// like a re-delivery and is silently discarded.
func TestHashPayloadIsStableAndDiscriminating(t *testing.T) {
	one := HashPayload([]byte(`{"producer":"P-001","litres":"12.500"}`))
	same := HashPayload([]byte(`{"producer":"P-001","litres":"12.500"}`))
	other := HashPayload([]byte(`{"producer":"P-001","litres":"12.501"}`))

	if one != same {
		t.Error("the same payload hashed twice gave two answers, so every re-delivery would " +
			"be admitted as an amendment")
	}
	if one == other {
		t.Error("a payload differing by one thousandth of a litre hashed identically, so an " +
			"amendment would be discarded as a duplicate")
	}
	if !strings.HasPrefix(one, "sha256:") {
		t.Errorf("hash %q carries no algorithm prefix; when the algorithm changes there is no "+
			"way to tell old hashes from new", one)
	}
	// An empty payload still hashes, and to something specific. A source that
	// delivers an empty record is delivering something, and the platform has to
	// be able to say it saw the same nothing twice.
	if HashPayload(nil) != HashPayload([]byte{}) {
		t.Error("nil and empty payloads hash differently")
	}
	if HashPayload(nil) == one {
		t.Error("an empty payload hashes the same as a real one")
	}
}

// Field order must not change the hash. A source that delivers its columns in a
// different order on Tuesday has not amended anything.
func TestHashFieldsDoesNotDependOnOrder(t *testing.T) {
	a := HashFields(map[string]string{"producer": "P-001", "litres": "12.500", "shift": "AM"})
	b := HashFields(map[string]string{"shift": "AM", "litres": "12.500", "producer": "P-001"})
	if a != b {
		t.Error("the same fields in a different order hashed differently; Go's map iteration " +
			"order is deliberately random, so this would fail intermittently in production " +
			"and never in a test run twice")
	}
	if HashFields(map[string]string{"producer": "P-002"}) == a {
		t.Error("different fields hashed the same")
	}
}

// Two structurally different records must not hash the same.
//
// This is a regression test for a real defect. The first version separated keys
// from values with 0x1f and records with 0x1e, and both pairs below collided —
// the delimiter appearing inside a field is indistinguishable from the delimiter
// between fields. Two source records with one payload hash means a re-import of
// one cannot be told from an amendment of the other, which is the exact property
// the hash exists to provide.
//
// Length-prefixing fixes it because the length is written outside the bytes it
// describes and no content can forge it.
func TestFieldContentCannotForgeTheStructure(t *testing.T) {
	for _, c := range []struct {
		what string
		a, b map[string]string
	}{
		{
			"a separator inside a key against one inside a value",
			map[string]string{"producer\x1fP-001": "x"},
			map[string]string{"producer": "P-001\x1fx"},
		},
		{
			"one field carrying a record separator against two real fields",
			map[string]string{"k": "v\x1ek2\x1fv2"},
			map[string]string{"k": "v", "k2": "v2"},
		},
		{
			"an empty value against a shorter key",
			map[string]string{"ab": ""},
			map[string]string{"a": "b"},
		},
		{
			"a field whose value is another field's name",
			map[string]string{"a": "b", "c": "d"},
			map[string]string{"a": "bc", "": "d"},
		},
	} {
		if HashFields(c.a) == HashFields(c.b) {
			t.Errorf("%s: two different records share one payload hash\n  %v\n  %v",
				c.what, c.a, c.b)
		}
	}
}

// A record is not a prefix of a larger one. Without the field count in the hash,
// a source that starts sending an extra column could produce a row that hashes
// as the row it used to send.
func TestARecordDoesNotHashAsAPrefixOfALargerOne(t *testing.T) {
	small := HashFields(map[string]string{"producer": "P-001"})
	large := HashFields(map[string]string{"producer": "P-001", "shift": "AM"})
	if small == large {
		t.Error("a record and the same record with a column added hashed identically")
	}
}

func TestHashFieldsOfNothingIsStable(t *testing.T) {
	if HashFields(nil) != HashFields(map[string]string{}) {
		t.Error("a nil field map and an empty one hash differently")
	}
	if HashFields(nil) == HashFields(map[string]string{"": ""}) {
		t.Error("no fields hashes the same as one empty field")
	}
}
