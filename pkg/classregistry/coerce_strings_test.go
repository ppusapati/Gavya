package classregistry

import (
	"strings"
	"testing"
	"time"
)

// ───────────────────────────────────────────────────────────────────────────
// Happy-path coercion for every supported kind
// ───────────────────────────────────────────────────────────────────────────

func TestCoerceStrings_AllKindsRoundTrip(t *testing.T) {
	reg := mustLoad(t, `
domain: workcenter
classes:
  c:
    attributes:
      name:
        type: string
      code:
        type: enum
        values: [alpha, bravo, charlie]
      owner_id:
        type: reference
        lookup: user
      count:
        type: int
      ratio:
        type: decimal
      enabled:
        type: bool
      commissioned_on:
        type: date
      last_audit:
        type: timestamp
      timeout:
        type: duration
`)

	typed, err := reg.ValidateAttributesFromStrings("workcenter", "c", map[string]string{
		"name":            "Plant A",
		"code":            "bravo",
		"owner_id":        "user_abc",
		"count":           "42",
		"ratio":           "3.14",
		"enabled":         "true",
		"commissioned_on": "2024-06-15",
		"last_audit":      "2026-04-20T10:00:00Z",
		"timeout":         "5m30s",
	})
	if err != nil {
		t.Fatalf("coerce: %v", err)
	}
	if typed["name"].String != "Plant A" {
		t.Errorf("string: %q", typed["name"].String)
	}
	if typed["code"].String != "bravo" {
		t.Errorf("enum: %q", typed["code"].String)
	}
	if typed["owner_id"].String != "user_abc" {
		t.Errorf("reference: %q", typed["owner_id"].String)
	}
	if typed["count"].Int != 42 {
		t.Errorf("int: %d", typed["count"].Int)
	}
	if typed["ratio"].Decimal != "3.14" {
		t.Errorf("decimal: %q", typed["ratio"].Decimal)
	}
	if !typed["enabled"].Bool {
		t.Error("bool: should be true")
	}
	expectedDate := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	if !typed["commissioned_on"].Date.Equal(expectedDate) {
		t.Errorf("date: %v", typed["commissioned_on"].Date)
	}
	if typed["timeout"].Duration != 5*time.Minute+30*time.Second {
		t.Errorf("duration: %v", typed["timeout"].Duration)
	}
}

// ───────────────────────────────────────────────────────────────────────────
// Parse-failure paths — each kind's malformed input
// ───────────────────────────────────────────────────────────────────────────

func TestCoerceStrings_ParseFailures(t *testing.T) {
	reg := mustLoad(t, `
domain: workcenter
classes:
  c:
    attributes:
      count: { type: int }
      ratio: { type: decimal }
      enabled: { type: bool }
      when: { type: date }
      ts: { type: timestamp }
      d: { type: duration }
`)

	cases := []struct {
		attr string
		raw  string
	}{
		{"count", "not-an-int"},
		{"ratio", "also-not-a-number"},
		{"enabled", "maybe"},
		{"when", "2024/06/15"},  // wrong separator
		{"ts", "sometime yesterday"},
		{"d", "two hours"},
	}
	for _, tc := range cases {
		t.Run(tc.attr, func(t *testing.T) {
			_, err := reg.ValidateAttributesFromStrings("workcenter", "c", map[string]string{
				tc.attr: tc.raw,
			})
			if err == nil {
				t.Fatalf("expected coerce failure for %s=%q", tc.attr, tc.raw)
			}
			if !strings.Contains(err.Error(), "coerce") {
				t.Errorf("error should flag coerce failure: %v", err)
			}
			if !strings.Contains(err.Error(), tc.attr) {
				t.Errorf("error should name the attribute %q: %v", tc.attr, err)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────────
// Empty strings are tolerated — absent value, not malformed
// ───────────────────────────────────────────────────────────────────────────

func TestCoerceStrings_EmptyNumericCoercesToZero(t *testing.T) {
	// Empty strings for numeric kinds coerce to zero-valued
	// AttributeValue (Int=0, Decimal=""). Downstream min-checks may
	// reject that, but the coercion itself is permissive — distinct
	// from time fields, where zero-time is a downstream rejection.
	reg := mustLoad(t, `
domain: workcenter
classes:
  c:
    attributes:
      count: { type: int }
      ratio: { type: decimal }
`)
	typed, err := reg.ValidateAttributesFromStrings("workcenter", "c", map[string]string{
		"count": "",
		"ratio": "",
	})
	if err != nil {
		t.Errorf("empty numeric strings should coerce, got: %v", err)
	}
	if typed["count"].Int != 0 {
		t.Errorf("count: %d, want 0", typed["count"].Int)
	}
	if typed["ratio"].Decimal != "" {
		t.Errorf("ratio: %q, want empty", typed["ratio"].Decimal)
	}
}

// ───────────────────────────────────────────────────────────────────────────
// Validation rules from the underlying ValidateAttributes still fire
// (min/max, enum, required, unknown-attr).
// ───────────────────────────────────────────────────────────────────────────

func TestCoerceStrings_UnderlyingValidationRunsAfterCoercion(t *testing.T) {
	reg := mustLoad(t, `
domain: workcenter
classes:
  c:
    attributes:
      grade:
        type: enum
        values: [a, b, c]
      size:
        type: int
        min: 0
        max: 100
      name:
        type: string
        required: true
`)

	// Enum violation after coercion.
	_, err := reg.ValidateAttributesFromStrings("workcenter", "c", map[string]string{
		"name":  "plant",
		"grade": "z",
		"size":  "10",
	})
	if err == nil {
		t.Fatal("expected enum violation to fire")
	}

	// Max violation.
	_, err = reg.ValidateAttributesFromStrings("workcenter", "c", map[string]string{
		"name":  "plant",
		"grade": "a",
		"size":  "150",
	})
	if err == nil {
		t.Fatal("expected max violation")
	}

	// Missing required.
	_, err = reg.ValidateAttributesFromStrings("workcenter", "c", map[string]string{
		"grade": "a",
		"size":  "10",
	})
	if err == nil {
		t.Fatal("expected required-missing violation")
	}

	// Happy path.
	typed, err := reg.ValidateAttributesFromStrings("workcenter", "c", map[string]string{
		"name":  "plant",
		"grade": "a",
		"size":  "10",
	})
	if err != nil {
		t.Fatalf("happy: %v", err)
	}
	if typed["size"].Int != 10 {
		t.Errorf("size: %d", typed["size"].Int)
	}
}

// ───────────────────────────────────────────────────────────────────────────
// Unknown class rejected via underlying GetClass
// ───────────────────────────────────────────────────────────────────────────

func TestCoerceStrings_UnknownClass(t *testing.T) {
	reg := mustLoad(t, `
domain: workcenter
classes:
  c:
    attributes:
      name: { type: string }
`)
	_, err := reg.ValidateAttributesFromStrings("workcenter", "nonexistent", map[string]string{})
	if err == nil {
		t.Fatal("expected unknown-class error")
	}
}

// ───────────────────────────────────────────────────────────────────────────
// Nil map is tolerated — coerces to empty, runs underlying validator
// (which will fire for any required attribute).
// ───────────────────────────────────────────────────────────────────────────

func TestCoerceStrings_NilMap(t *testing.T) {
	reg := mustLoad(t, `
domain: workcenter
classes:
  c:
    attributes:
      optional_field: { type: string }
`)
	typed, err := reg.ValidateAttributesFromStrings("workcenter", "c", nil)
	if err != nil {
		t.Fatalf("nil should be fine if no required fields: %v", err)
	}
	if typed == nil {
		t.Error("expected empty typed map, got nil")
	}
}

// ───────────────────────────────────────────────────────────────────────────
// Whitespace trimming — coercion handles "  42  " as 42
// ───────────────────────────────────────────────────────────────────────────

func TestCoerceStrings_TrimsWhitespace(t *testing.T) {
	reg := mustLoad(t, `
domain: workcenter
classes:
  c:
    attributes:
      n: { type: int }
`)
	typed, err := reg.ValidateAttributesFromStrings("workcenter", "c", map[string]string{
		"n": "  42  ",
	})
	if err != nil {
		t.Fatalf("trim: %v", err)
	}
	if typed["n"].Int != 42 {
		t.Errorf("got %d", typed["n"].Int)
	}
}
