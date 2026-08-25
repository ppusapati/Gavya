package domain

import "testing"

// calf_gender is VARCHAR(1). Nothing validated it, so "female" — the obvious
// value for a string field of that name — reached PostgreSQL as a length
// violation and came back to the caller as an internal failure.
func TestCalfSexAcceptsWhatAPersonWouldType(t *testing.T) {
	for _, in := range []string{"female", "Female", "F", "f", "heifer", " FEMALE "} {
		got, ok := NormaliseCalfSex(in)
		if !ok || got != CalfFemale {
			t.Errorf("NormaliseCalfSex(%q) = %q, %v; want %q, true", in, got, ok, CalfFemale)
		}
	}
	for _, in := range []string{"male", "M", "m", "bull", "Male"} {
		got, ok := NormaliseCalfSex(in)
		if !ok || got != CalfMale {
			t.Errorf("NormaliseCalfSex(%q) = %q, %v; want %q, true", in, got, ok, CalfMale)
		}
	}
}

// The column is nullable and a stillbirth may genuinely have no sex recorded,
// so absence is allowed — but it is absence, not a value.
func TestAnUnrecordedCalfSexIsAllowed(t *testing.T) {
	got, ok := NormaliseCalfSex("")
	if !ok || got != "" {
		t.Errorf("NormaliseCalfSex(\"\") = %q, %v; want empty, true", got, ok)
	}
}

func TestNonsenseIsRefusedRatherThanTruncated(t *testing.T) {
	// Truncating "unknown" to "u" would store a sex nobody asked for.
	for _, in := range []string{"unknown", "x", "both", "1"} {
		if _, ok := NormaliseCalfSex(in); ok {
			t.Errorf("NormaliseCalfSex(%q) was accepted", in)
		}
	}
}

// Whatever comes out must fit the column, or the validation has achieved
// nothing.
func TestEveryAcceptedSexFitsTheColumn(t *testing.T) {
	for _, in := range []string{"female", "male", "F", "M", "heifer", "bull", ""} {
		got, ok := NormaliseCalfSex(in)
		if !ok {
			t.Fatalf("NormaliseCalfSex(%q) was refused", in)
		}
		if len(got) > 1 {
			t.Errorf("NormaliseCalfSex(%q) = %q, which does not fit VARCHAR(1)", in, got)
		}
	}
}

func TestCalvingStatusIsAClosedVocabulary(t *testing.T) {
	for _, ok := range []string{CalvingNormal, CalvingAssisted, CalvingEmergency} {
		if !ValidCalvingStatus(ok) {
			t.Errorf("%q was rejected", ok)
		}
	}
	for _, bad := range []string{"", "live", "Normal", "difficult"} {
		if ValidCalvingStatus(bad) {
			t.Errorf("%q was accepted", bad)
		}
	}
}
