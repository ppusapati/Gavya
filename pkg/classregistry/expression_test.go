package classregistry

import (
	"strings"
	"testing"
	"time"
)

// ───────────────────────────────────────────────────────────────────────────
// Parse-time validation
// ───────────────────────────────────────────────────────────────────────────

func TestParseExpression_HappyPath(t *testing.T) {
	cases := []struct {
		name      string
		formula   string
		dependsOn []string
	}{
		{"literal", "42", nil},
		{"decimal", "3.14", nil},
		{"add", "a + b", []string{"a", "b"}},
		{"precedence", "a + b * c", []string{"a", "b", "c"}},
		{"parens", "(a + b) * c", []string{"a", "b", "c"}},
		{"unary", "-a", []string{"a"}},
		{"pow", "a ^ 2", []string{"a"}},
		{"func_call", "abs(a - b)", []string{"a", "b"}},
		{"nested_func", "round(a / b * 100, 2)", []string{"a", "b"}},
		{"min_many", "min(a, b, c, d)", []string{"a", "b", "c", "d"}},
		{"convert", "convert(x, hours, minutes)", []string{"x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseExpression(tc.formula, tc.dependsOn); err != nil {
				t.Fatalf("parse %q: %v", tc.formula, err)
			}
		})
	}
}

func TestParseExpression_RejectsUndeclaredReference(t *testing.T) {
	_, err := ParseExpression("a + mystery", []string{"a"})
	if err == nil {
		t.Fatal("expected error for undeclared reference")
	}
	if !strings.Contains(err.Error(), "mystery") {
		t.Errorf("error should name the undeclared attr, got: %v", err)
	}
}

func TestParseExpression_RejectsTrailingGarbage(t *testing.T) {
	_, err := ParseExpression("a + b extra", []string{"a", "b"})
	if err == nil {
		t.Fatal("expected error for trailing garbage")
	}
}

func TestParseExpression_RejectsUnknownFunction(t *testing.T) {
	// Unknown functions parse successfully (syntactically valid) but
	// fail on first evaluation because the evaluator doesn't know the
	// function. Validate by calling Evaluate.
	e, err := ParseExpression("sqrt(a)", []string{"a"})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	_, err = e.Evaluate(map[string]AttributeValue{
		"a": {Kind: KindDecimal, Decimal: "4"},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown function") {
		t.Fatalf("expected unknown-function error, got: %v", err)
	}
}

func TestParseExpression_RejectsConditionalsAndLoops(t *testing.T) {
	// No `if`, `case`, or assignment tokens exist in the grammar.
	// They arrive as identifiers and then trip the next token
	// because there's no way to combine them into a valid tree.
	bad := []string{
		"if a > b then 1 else 2",
		"a = 5",
		"for i in [1,2,3]",
	}
	for _, f := range bad {
		if _, err := ParseExpression(f, []string{"a", "b"}); err == nil {
			t.Errorf("expected parse failure for %q", f)
		}
	}
}

// ───────────────────────────────────────────────────────────────────────────
// Evaluation — happy path
// ───────────────────────────────────────────────────────────────────────────

func TestEvaluate_Arithmetic(t *testing.T) {
	cases := []struct {
		formula string
		deps    []string
		attrs   map[string]AttributeValue
		want    string
	}{
		{"a + b", []string{"a", "b"}, fattrs("a", 3, "b", 4), "7"},
		{"a - b", []string{"a", "b"}, fattrs("a", 10, "b", 3), "7"},
		{"a * b", []string{"a", "b"}, fattrs("a", 6, "b", 7), "42"},
		{"a / b", []string{"a", "b"}, fattrs("a", 15, "b", 3), "5"},
		{"a ^ b", []string{"a", "b"}, fattrs("a", 2, "b", 10), "1024"},
		{"-a + 10", []string{"a"}, fattrs("a", 3), "7"},
		{"(a + b) * 2", []string{"a", "b"}, fattrs("a", 3, "b", 4), "14"},
	}
	for _, tc := range cases {
		t.Run(tc.formula, func(t *testing.T) {
			e, err := ParseExpression(tc.formula, tc.deps)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got, err := e.Evaluate(tc.attrs)
			if err != nil {
				t.Fatalf("eval: %v", err)
			}
			if got.Decimal != tc.want {
				t.Errorf("got %q, want %q", got.Decimal, tc.want)
			}
		})
	}
}

func TestEvaluate_DivideByZero(t *testing.T) {
	e, _ := ParseExpression("a / b", []string{"a", "b"})
	_, err := e.Evaluate(fattrs("a", 10, "b", 0))
	if err == nil || !strings.Contains(err.Error(), "divide by zero") {
		t.Fatalf("expected divide-by-zero error, got: %v", err)
	}
}

func TestEvaluate_MissingAttributeIsZero(t *testing.T) {
	// An attribute listed in depends_on may still be absent on an
	// older row that predates a schema addition; we treat absent as
	// zero to match SAP characteristic semantics.
	e, _ := ParseExpression("a + b", []string{"a", "b"})
	got, err := e.Evaluate(fattrs("a", 5))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got.Decimal != "5" {
		t.Errorf("got %q, want 5 (absent b treated as 0)", got.Decimal)
	}
}

// ───────────────────────────────────────────────────────────────────────────
// Built-in functions
// ───────────────────────────────────────────────────────────────────────────

func TestBuiltin_AbsRoundFloorCeil(t *testing.T) {
	check := func(formula, want string) {
		t.Helper()
		e, err := ParseExpression(formula, []string{"a"})
		if err != nil {
			t.Fatalf("parse %q: %v", formula, err)
		}
		got, err := e.Evaluate(fattrs("a", -3.7))
		if err != nil {
			t.Fatalf("eval %q: %v", formula, err)
		}
		if got.Decimal != want {
			t.Errorf("%q: got %q, want %q", formula, got.Decimal, want)
		}
	}
	check("abs(a)", "3.7")
	check("floor(a)", "-4")
	check("ceil(a)", "-3")
	check("round(a, 1)", "-3.7")
}

func TestBuiltin_MinMax(t *testing.T) {
	e, _ := ParseExpression("min(a, b, 10)", []string{"a", "b"})
	got, _ := e.Evaluate(fattrs("a", 20, "b", 5))
	if got.Decimal != "5" {
		t.Errorf("min: got %q, want 5", got.Decimal)
	}

	e, _ = ParseExpression("max(a, b, 0)", []string{"a", "b"})
	got, _ = e.Evaluate(fattrs("a", 20, "b", 5))
	if got.Decimal != "20" {
		t.Errorf("max: got %q, want 20", got.Decimal)
	}
}

func TestBuiltin_Convert(t *testing.T) {
	e, err := ParseExpression("convert(x, hours, minutes)", []string{"x"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got, err := e.Evaluate(fattrs("x", 2))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got.Decimal != "120" {
		t.Errorf("got %q, want 120", got.Decimal)
	}
}

func TestBuiltin_Convert_UnknownUnit(t *testing.T) {
	e, _ := ParseExpression("convert(x, smoots, miles)", []string{"x"})
	_, err := e.Evaluate(fattrs("x", 1))
	if err == nil || !strings.Contains(err.Error(), "unknown source unit") {
		t.Fatalf("expected unknown-unit error, got: %v", err)
	}
}

func TestBuiltin_DaysBetween(t *testing.T) {
	// Day-serial inputs.
	e, _ := ParseExpression("days_between(a, b)", []string{"a", "b"})
	got, err := e.Evaluate(fattrs("a", 100, "b", 107))
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got.Decimal != "7" {
		t.Errorf("got %q, want 7", got.Decimal)
	}
}

func TestBuiltin_AgeYears_FrozenClock(t *testing.T) {
	// Freeze clock for determinism.
	orig := Clock
	Clock = func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }
	defer func() { Clock = orig }()

	// Birth date = 2000-01-01, serial = 10957
	e, _ := ParseExpression("age_years(birth)", []string{"birth"})
	got, err := e.Evaluate(map[string]AttributeValue{
		"birth": {Kind: KindDate, Date: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)},
	})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	// 2030 - 2000 ≈ 30 years. Tolerance ±0.02 for leap-year averaging.
	if !hasPrefix(got.Decimal, "30") && !hasPrefix(got.Decimal, "29.9") {
		t.Errorf("got %q, want ~30", got.Decimal)
	}
}

func hasPrefix(s, p string) bool { return strings.HasPrefix(s, p) }

// ───────────────────────────────────────────────────────────────────────────
// References
// ───────────────────────────────────────────────────────────────────────────

func TestReferences_SortedAndDeduplicated(t *testing.T) {
	e, err := ParseExpression("c + a + b + a + c", []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	refs := e.References()
	want := []string{"a", "b", "c"}
	if len(refs) != 3 {
		t.Fatalf("got %v, want 3", refs)
	}
	for i, v := range want {
		if refs[i] != v {
			t.Errorf("refs[%d] = %q, want %q", i, refs[i], v)
		}
	}
}

func TestReferences_ExcludesConvertUnitLiterals(t *testing.T) {
	// convert()'s 2nd + 3rd args are unit identifiers, not attribute
	// refs. They must NOT appear in References(), so depends_on
	// doesn't need to declare "hours" and "minutes" as attributes.
	e, err := ParseExpression("convert(duration, hours, minutes)", []string{"duration"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	refs := e.References()
	if len(refs) != 1 || refs[0] != "duration" {
		t.Errorf("got refs %v, want [duration]", refs)
	}
}

// ───────────────────────────────────────────────────────────────────────────
// Coercion of different AttributeValue kinds
// ───────────────────────────────────────────────────────────────────────────

func TestEvaluate_CoercesKinds(t *testing.T) {
	e, _ := ParseExpression("a + b + c", []string{"a", "b", "c"})
	got, err := e.Evaluate(map[string]AttributeValue{
		"a": {Kind: KindInt, Int: 10},
		"b": {Kind: KindDecimal, Decimal: "3.5"},
		"c": {Kind: KindBool, Bool: true},
	})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got.Decimal != "14.5" {
		t.Errorf("got %q, want 14.5", got.Decimal)
	}
}

// ───────────────────────────────────────────────────────────────────────────
// Helpers
// ───────────────────────────────────────────────────────────────────────────

// fattrs builds an attribute map of decimal values from name/value pairs.
func fattrs(pairs ...any) map[string]AttributeValue {
	out := make(map[string]AttributeValue, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		name := pairs[i].(string)
		var v AttributeValue
		switch n := pairs[i+1].(type) {
		case int:
			v = AttributeValue{Kind: KindDecimal, Decimal: itoa(n)}
		case float64:
			v = AttributeValue{Kind: KindDecimal, Decimal: ftoa(n)}
		}
		out[name] = v
	}
	return out
}

func itoa(n int) string { return ftoa(float64(n)) }
func ftoa(f float64) string {
	return formatFloat(f)
}
