package onboarding

import (
	"strings"
	"testing"

	"p9e.in/samavaya/packages/classregistry"
)

// tinyRegistry returns a class registry with one domain + class for
// loader tests.
func tinyRegistry(t *testing.T) classregistry.Registry {
	t.Helper()
	y := `
domain: eq
base_classes: {}
classes:
  cnc_machine:
    label: CNC Machine
    attributes:
      rpm:
        type: int
        min: 0
        max: 50000
`
	reg, err := classregistry.LoadBytes(map[string][]byte{"eq": []byte(y)})
	if err != nil {
		t.Fatalf("registry fixture: %v", err)
	}
	return reg
}

func TestLoader_HappyPath(t *testing.T) {
	reg := tinyRegistry(t)
	l := NewLoader("", reg)

	profiles, err := l.LoadBytes(map[string][]byte{
		"manufacturing_discrete": []byte(`
industry_code: manufacturing_discrete
label: Discrete Manufacturing
version: "1.0.0"
description: Unit-assembly manufacturers.
enabled_classes:
  - domain: eq
    class: cnc_machine
enabled_processes:
  - depreciation
`),
	})
	if err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	p := profiles["manufacturing_discrete"]
	if p == nil {
		t.Fatalf("expected profile loaded, got nil")
	}
	if len(p.EnabledClasses) != 1 {
		t.Fatalf("expected 1 enabled class, got %d", len(p.EnabledClasses))
	}
	if p.Label != "Discrete Manufacturing" {
		t.Fatalf("label mismatch: %q", p.Label)
	}
}

func TestLoader_MissingLabelRejected(t *testing.T) {
	reg := tinyRegistry(t)
	l := NewLoader("", reg)
	_, err := l.LoadBytes(map[string][]byte{
		"x": []byte(`industry_code: x
version: "1.0.0"
`),
	})
	if err == nil || !strings.Contains(err.Error(), "ONBOARDING_PROFILE_MISSING_LABEL") {
		t.Fatalf("expected MISSING_LABEL, got %v", err)
	}
}

func TestLoader_MissingVersionRejected(t *testing.T) {
	reg := tinyRegistry(t)
	l := NewLoader("", reg)
	_, err := l.LoadBytes(map[string][]byte{
		"x": []byte(`industry_code: x
label: X
`),
	})
	if err == nil || !strings.Contains(err.Error(), "ONBOARDING_PROFILE_MISSING_VERSION") {
		t.Fatalf("expected MISSING_VERSION, got %v", err)
	}
}

func TestLoader_UnknownClassRejected(t *testing.T) {
	reg := tinyRegistry(t)
	l := NewLoader("", reg)
	_, err := l.LoadBytes(map[string][]byte{
		"pharma": []byte(`
industry_code: pharma
label: Pharma
version: "1.0.0"
enabled_classes:
  - domain: eq
    class: does_not_exist
`),
	})
	if err == nil || !strings.Contains(err.Error(), "ONBOARDING_PROFILE_UNKNOWN_CLASS") {
		t.Fatalf("expected UNKNOWN_CLASS, got %v", err)
	}
}

func TestLoader_DuplicateClassRejected(t *testing.T) {
	reg := tinyRegistry(t)
	l := NewLoader("", reg)
	_, err := l.LoadBytes(map[string][]byte{
		"x": []byte(`
industry_code: x
label: X
version: "1.0.0"
enabled_classes:
  - domain: eq
    class: cnc_machine
  - domain: eq
    class: cnc_machine
`),
	})
	if err == nil || !strings.Contains(err.Error(), "ONBOARDING_PROFILE_DUPLICATE_CLASS") {
		t.Fatalf("expected DUPLICATE_CLASS, got %v", err)
	}
}

func TestLoader_EmptySeedRowRejected(t *testing.T) {
	reg := tinyRegistry(t)
	l := NewLoader("", reg)
	_, err := l.LoadBytes(map[string][]byte{
		"x": []byte(`
industry_code: x
label: X
version: "1.0.0"
seed_masters:
  - domain: eq
    table: cnc_templates
    rows:
      - {}
`),
	})
	if err == nil || !strings.Contains(err.Error(), "ONBOARDING_PROFILE_EMPTY_SEED_ROW") {
		t.Fatalf("expected EMPTY_SEED_ROW, got %v", err)
	}
}

func TestLoader_IndustryCodeMismatchRejected(t *testing.T) {
	reg := tinyRegistry(t)
	l := NewLoader("", reg)
	_, err := l.LoadBytes(map[string][]byte{
		"manufacturing_discrete": []byte(`
industry_code: construction
label: X
version: "1.0.0"
`),
	})
	if err == nil || !strings.Contains(err.Error(), "industry_code mismatch") {
		t.Fatalf("expected industry_code mismatch, got %v", err)
	}
}

func TestLoader_NoRegistryRejected(t *testing.T) {
	l := NewLoader("", nil)
	_, err := l.LoadBytes(map[string][]byte{})
	if err == nil || !strings.Contains(err.Error(), "ONBOARDING_LOADER_NO_REGISTRY") {
		t.Fatalf("expected NO_REGISTRY, got %v", err)
	}
}
