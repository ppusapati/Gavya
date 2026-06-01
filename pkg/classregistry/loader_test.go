package classregistry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ───────────────────────────────────────────────────────────────────────────
// Happy path — a minimal well-formed domain loads cleanly.
// ───────────────────────────────────────────────────────────────────────────

func TestLoad_MinimalDomain(t *testing.T) {
	yaml := `
domain: workcenter
classes:
  mfg_discrete:
    label: Discrete Manufacturing Work Center
    attributes:
      machine_type:
        type: string
      theoretical_rate:
        type: decimal
        min: 0
    compliance_checks: [safety_check]
    capacity_metrics: [oee]
    processes: [oee_calculation]
`
	reg, err := LoadBytes(map[string][]byte{"workcenter": []byte(yaml)})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cd, err := reg.GetClass("workcenter", "mfg_discrete")
	if err != nil {
		t.Fatalf("GetClass: %v", err)
	}
	if cd.Label != "Discrete Manufacturing Work Center" {
		t.Errorf("label: %q", cd.Label)
	}
	if _, ok := cd.Attributes["machine_type"]; !ok {
		t.Error("machine_type missing")
	}
	if got := len(cd.Processes); got != 1 || cd.Processes[0] != "oee_calculation" {
		t.Errorf("processes: %v", cd.Processes)
	}
}

// ───────────────────────────────────────────────────────────────────────────
// Inheritance — child gets parent's attributes + unions lists.
// ───────────────────────────────────────────────────────────────────────────

func TestLoad_InheritanceExpandsAttributes(t *testing.T) {
	yaml := `
domain: workcenter
base_classes:
  base_work_center:
    attributes:
      cost_center_id:
        type: reference
        lookup: cost_center
        required: true
      capacity_uom:
        type: enum
        values: [hours, tonnes, liters, m3, kwh, units]
        default: hours
    compliance_checks: [safety_check]

classes:
  water_treatment:
    extends: base_work_center
    attributes:
      filtration_rating:
        type: enum
        values: [micro, ultra, nano, ro]
        required: true
    compliance_checks: [potability_test]
    capacity_metrics: [peak_demand_m3h]
`
	reg, err := LoadBytes(map[string][]byte{"workcenter": []byte(yaml)})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	cd, _ := reg.GetClass("workcenter", "water_treatment")

	// Inherited attribute present.
	if _, ok := cd.Attributes["cost_center_id"]; !ok {
		t.Error("cost_center_id should have been inherited")
	}
	if _, ok := cd.Attributes["capacity_uom"]; !ok {
		t.Error("capacity_uom should have been inherited")
	}
	// Own attribute present.
	if _, ok := cd.Attributes["filtration_rating"]; !ok {
		t.Error("filtration_rating should be on the child")
	}
	// Compliance checks unioned + sorted.
	got := strings.Join(cd.ComplianceChecks, ",")
	want := "potability_test,safety_check"
	if got != want {
		t.Errorf("compliance: got %q, want %q", got, want)
	}

	// Default applied from YAML.
	uom := cd.Attributes["capacity_uom"]
	if uom.Default == nil || uom.Default.String != "hours" {
		t.Errorf("default not applied: %+v", uom.Default)
	}
}

func TestLoad_InheritanceCycleRejected(t *testing.T) {
	yaml := `
domain: workcenter
classes:
  a:
    extends: b
    attributes:
      x:
        type: string
  b:
    extends: a
    attributes:
      y:
        type: string
`
	_, err := LoadBytes(map[string][]byte{"workcenter": []byte(yaml)})
	if err == nil {
		t.Fatal("expected cycle rejection")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error should mention cycle, got: %v", err)
	}
}

func TestLoad_UnknownExtendsRejected(t *testing.T) {
	yaml := `
domain: workcenter
classes:
  orphan:
    extends: ghost
    attributes:
      x:
        type: string
`
	_, err := LoadBytes(map[string][]byte{"workcenter": []byte(yaml)})
	if err == nil {
		t.Fatal("expected unknown-extends rejection")
	}
}

// ───────────────────────────────────────────────────────────────────────────
// Derived attributes — parse at load time, cross-check depends_on.
// ───────────────────────────────────────────────────────────────────────────

func TestLoad_DerivedAttributeParsedAtLoad(t *testing.T) {
	yaml := `
domain: workcenter
classes:
  mfg_discrete:
    attributes:
      uptime_hours:
        type: decimal
      scheduled_hours:
        type: decimal
    derived_attributes:
      availability_pct:
        formula: uptime_hours / scheduled_hours * 100
        unit: percent
        depends_on: [uptime_hours, scheduled_hours]
`
	reg, err := LoadBytes(map[string][]byte{"workcenter": []byte(yaml)})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	out, err := reg.ComputeDerived("workcenter", "mfg_discrete", map[string]AttributeValue{
		"uptime_hours":    {Kind: KindDecimal, Decimal: "160"},
		"scheduled_hours": {Kind: KindDecimal, Decimal: "200"},
	})
	if err != nil {
		t.Fatalf("ComputeDerived: %v", err)
	}
	av, ok := out["availability_pct"]
	if !ok {
		t.Fatal("availability_pct missing from output")
	}
	if av.Decimal != "80" {
		t.Errorf("got %q, want 80", av.Decimal)
	}
}

func TestLoad_DerivedFormulaReferencingUndeclaredDepRejected(t *testing.T) {
	yaml := `
domain: workcenter
classes:
  c:
    attributes:
      a:
        type: decimal
    derived_attributes:
      bad:
        formula: a + b
        depends_on: [a, b]
`
	// Parse fails because `b` is in depends_on but not an attribute.
	_, err := LoadBytes(map[string][]byte{"workcenter": []byte(yaml)})
	if err == nil {
		t.Fatal("expected error: depends_on references unknown attribute")
	}
	if !strings.Contains(err.Error(), "unknown attribute") {
		t.Errorf("wrong error: %v", err)
	}
}

// ───────────────────────────────────────────────────────────────────────────
// Validation — the actual enforcement surface.
// ───────────────────────────────────────────────────────────────────────────

func TestValidate_MissingRequired(t *testing.T) {
	reg := mustLoad(t, `
domain: workcenter
classes:
  c:
    attributes:
      must_have:
        type: string
        required: true
`)
	err := reg.ValidateAttributes("workcenter", "c", map[string]AttributeValue{})
	if err == nil {
		t.Fatal("expected missing-required error")
	}
	if !strings.Contains(err.Error(), "must_have") {
		t.Errorf("error should name the attr: %v", err)
	}
}

func TestValidate_UnknownAttributeRejected(t *testing.T) {
	reg := mustLoad(t, `
domain: workcenter
classes:
  c:
    attributes:
      known:
        type: string
`)
	err := reg.ValidateAttributes("workcenter", "c", map[string]AttributeValue{
		"known":   {Kind: KindString, String: "hi"},
		"unknown": {Kind: KindString, String: "oops"},
	})
	if err == nil {
		t.Fatal("expected unknown-attribute error")
	}
}

func TestValidate_TypeMismatch(t *testing.T) {
	reg := mustLoad(t, `
domain: workcenter
classes:
  c:
    attributes:
      age:
        type: int
`)
	err := reg.ValidateAttributes("workcenter", "c", map[string]AttributeValue{
		"age": {Kind: KindString, String: "not an int"},
	})
	if err == nil {
		t.Fatal("expected type-mismatch error")
	}
}

func TestValidate_MinMax(t *testing.T) {
	reg := mustLoad(t, `
domain: workcenter
classes:
  c:
    attributes:
      percent:
        type: decimal
        min: 0
        max: 100
`)
	err := reg.ValidateAttributes("workcenter", "c", map[string]AttributeValue{
		"percent": {Kind: KindDecimal, Decimal: "150"},
	})
	if err == nil || !strings.Contains(err.Error(), "max") {
		t.Fatalf("expected max-violation, got: %v", err)
	}
	err = reg.ValidateAttributes("workcenter", "c", map[string]AttributeValue{
		"percent": {Kind: KindDecimal, Decimal: "-5"},
	})
	if err == nil || !strings.Contains(err.Error(), "min") {
		t.Fatalf("expected min-violation, got: %v", err)
	}
	// Happy path.
	if err := reg.ValidateAttributes("workcenter", "c", map[string]AttributeValue{
		"percent": {Kind: KindDecimal, Decimal: "42"},
	}); err != nil {
		t.Errorf("happy path: %v", err)
	}
}

func TestValidate_EnumRejectsOutOfSet(t *testing.T) {
	reg := mustLoad(t, `
domain: workcenter
classes:
  c:
    attributes:
      grade:
        type: enum
        values: [a, b, c]
`)
	err := reg.ValidateAttributes("workcenter", "c", map[string]AttributeValue{
		"grade": {Kind: KindEnum, String: "d"},
	})
	if err == nil {
		t.Fatal("expected enum violation")
	}
}

func TestValidate_DefaultApplied(t *testing.T) {
	reg := mustLoad(t, `
domain: workcenter
classes:
  c:
    attributes:
      uom:
        type: enum
        values: [hours, tonnes]
        default: hours
`)
	attrs := map[string]AttributeValue{}
	if err := reg.ValidateAttributes("workcenter", "c", attrs); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if v, ok := attrs["uom"]; !ok {
		t.Error("default should be applied in-place")
	} else if v.String != "hours" {
		t.Errorf("default value: got %q", v.String)
	}
}

func TestValidate_PatternEnforced(t *testing.T) {
	reg := mustLoad(t, `
domain: workcenter
classes:
  c:
    attributes:
      code:
        type: string
        pattern: ^[A-Z]{3}-[0-9]{4}$
`)
	if err := reg.ValidateAttributes("workcenter", "c", map[string]AttributeValue{
		"code": {Kind: KindString, String: "ABC-1234"},
	}); err != nil {
		t.Fatalf("happy path: %v", err)
	}
	if err := reg.ValidateAttributes("workcenter", "c", map[string]AttributeValue{
		"code": {Kind: KindString, String: "wrong"},
	}); err == nil {
		t.Fatal("expected pattern violation")
	}
}

// ───────────────────────────────────────────────────────────────────────────
// Filesystem loader — round-trip through an actual directory.
// ───────────────────────────────────────────────────────────────────────────

func TestLoader_FromDirectory(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, filepath.Join(dir, "workcenter.yaml"), `
domain: workcenter
classes:
  mfg_discrete:
    attributes:
      x:
        type: string
`)
	writeYAML(t, filepath.Join(dir, "asset.yaml"), `
domain: asset
classes:
  vehicle:
    attributes:
      plate:
        type: string
`)
	reg, err := NewLoader(dir).Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	got := reg.Domains()
	want := []string{"asset", "workcenter"}
	if len(got) != len(want) {
		t.Fatalf("domains: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("domains[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLoader_DomainFilenameMismatchRejected(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, filepath.Join(dir, "workcenter.yaml"), `
domain: asset
classes:
  c:
    attributes: {}
`)
	_, err := NewLoader(dir).Load()
	if err == nil {
		t.Fatal("expected mismatch rejection")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Errorf("error should mention mismatch: %v", err)
	}
}

func TestLoader_MissingDirectoryIsError(t *testing.T) {
	_, err := NewLoader("/no/such/path/classregistry").Load()
	if err == nil {
		t.Fatal("expected error for missing dir")
	}
}

// ───────────────────────────────────────────────────────────────────────────
// Miscellaneous — edge cases.
// ───────────────────────────────────────────────────────────────────────────

func TestListClasses_SortedByName(t *testing.T) {
	reg := mustLoad(t, `
domain: workcenter
classes:
  zebra: { attributes: {} }
  apple: { attributes: {} }
  mango: { attributes: {} }
`)
	got := reg.ListClasses("workcenter")
	want := []string{"apple", "mango", "zebra"}
	if len(got) != len(want) {
		t.Fatalf("list: got %v, want %v", got, want)
	}
	for i, cd := range got {
		if cd.Name != want[i] {
			t.Errorf("got %q, want %q", cd.Name, want[i])
		}
	}
}

func TestGetClass_UnknownReturnsNotFound(t *testing.T) {
	reg := mustLoad(t, `
domain: workcenter
classes:
  known: { attributes: {} }
`)
	_, err := reg.GetClass("workcenter", "unknown")
	if err == nil {
		t.Fatal("expected not-found")
	}
}

func TestGetClass_UnknownDomainReturnsNotFound(t *testing.T) {
	reg := mustLoad(t, `
domain: workcenter
classes:
  known: { attributes: {} }
`)
	_, err := reg.GetClass("nonexistent", "any")
	if err == nil {
		t.Fatal("expected not-found")
	}
}

func TestGetCustomExtensions_ReturnedVerbatim(t *testing.T) {
	reg := mustLoad(t, `
domain: workcenter
classes:
  pharma:
    attributes: {}
    custom_extensions:
      - name: dea_tracking
        service: business/pharma/dea_tracking
        required: true
        description: DEA tracking
`)
	ext := reg.GetCustomExtensions("workcenter", "pharma")
	if len(ext) != 1 {
		t.Fatalf("got %v", ext)
	}
	if ext[0].Name != "dea_tracking" {
		t.Errorf("name: %q", ext[0].Name)
	}
	if !ext[0].Required {
		t.Error("required flag dropped")
	}
}

// ───────────────────────────────────────────────────────────────────────────
// Test helpers
// ───────────────────────────────────────────────────────────────────────────

func mustLoad(t *testing.T, yaml string) Registry {
	t.Helper()
	reg, err := LoadBytes(map[string][]byte{"workcenter": []byte(yaml)})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return reg
}

func writeYAML(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}
