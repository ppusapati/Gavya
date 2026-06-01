package classregistry

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// inMemoryOverrideStore is a map-backed OverrideStore used only in
// tests. Real deployments use packages/classregistry/pgstore.
type inMemoryOverrideStore struct {
	// per tenant → per domain → per class
	byTenant map[string]map[string]map[string]ClassOverride
	// fail controls returning an error on the next call, for
	// testing the GetClass failure path.
	fail error
}

func newInMemoryStore() *inMemoryOverrideStore {
	return &inMemoryOverrideStore{byTenant: map[string]map[string]map[string]ClassOverride{}}
}

func (s *inMemoryOverrideStore) set(tenant, domain, class string, ov ClassOverride) {
	if s.byTenant[tenant] == nil {
		s.byTenant[tenant] = map[string]map[string]ClassOverride{}
	}
	if s.byTenant[tenant][domain] == nil {
		s.byTenant[tenant][domain] = map[string]ClassOverride{}
	}
	s.byTenant[tenant][domain][class] = ov
}

func (s *inMemoryOverrideStore) ListForTenantDomain(
	_ context.Context, tenantID, domain string,
) (map[string]ClassOverride, error) {
	if s.fail != nil {
		return nil, s.fail
	}
	byDomain, ok := s.byTenant[tenantID]
	if !ok {
		return nil, nil
	}
	out := map[string]ClassOverride{}
	for k, v := range byDomain[domain] {
		out[k] = v
	}
	return out, nil
}

// -- Fixtures ----------------------------------------------------------------

func fixtureRegistry(t *testing.T) Registry {
	t.Helper()
	yaml := `
domain: wc
base_classes: {}
classes:
  machine:
    label: Machine
    industries: [manufacturing]
    attributes:
      color:
        type: enum
        values: [red, green, blue, yellow]
        required: false
      horsepower:
        type: int
        min: 10
        max: 500
      certified:
        type: bool
`
	reg, err := LoadBytes(map[string][]byte{"wc": []byte(yaml)})
	if err != nil {
		t.Fatalf("fixture load: %v", err)
	}
	return reg
}

func boolPtr(b bool) *bool       { return &b }
func floatPtr(f float64) *float64 { return &f }

// -- Tests -------------------------------------------------------------------

func TestOverlay_NoStore_ReturnsBaseVerbatim(t *testing.T) {
	base := fixtureRegistry(t)
	ta := NewTenantAware(base, nil)

	got := ta.WithTenant(context.Background(), "tenant-1")
	cd, err := got.GetClass("wc", "machine")
	if err != nil {
		t.Fatalf("GetClass: %v", err)
	}
	if len(cd.Attributes["color"].Values) != 4 {
		t.Fatalf("expected base 4-value enum, got %v", cd.Attributes["color"].Values)
	}
}

func TestOverlay_EmptyTenant_ReturnsBase(t *testing.T) {
	base := fixtureRegistry(t)
	store := newInMemoryStore()
	ta := NewTenantAware(base, store)

	got := ta.WithTenant(context.Background(), "")
	if got != base {
		t.Fatalf("empty tenant should return base directly, got %T", got)
	}
}

func TestOverlay_NoOverrides_ReturnsBaseClass(t *testing.T) {
	base := fixtureRegistry(t)
	store := newInMemoryStore()
	ta := NewTenantAware(base, store)

	view := ta.WithTenant(context.Background(), "tenant-with-no-overrides")
	cd, err := view.GetClass("wc", "machine")
	if err != nil {
		t.Fatalf("GetClass: %v", err)
	}
	if got := len(cd.Attributes["color"].Values); got != 4 {
		t.Fatalf("expected 4 enum values, got %d", got)
	}
}

func TestOverlay_Enum_NarrowAllowed(t *testing.T) {
	base := fixtureRegistry(t)
	store := newInMemoryStore()
	store.set("t1", "wc", "machine", ClassOverride{
		AttributeOverrides: map[string]AttributeOverride{
			"color": {Values: []string{"red", "green"}},
		},
	})
	ta := NewTenantAware(base, store)

	cd, err := ta.WithTenant(context.Background(), "t1").GetClass("wc", "machine")
	if err != nil {
		t.Fatalf("GetClass: %v", err)
	}
	got := cd.Attributes["color"].Values
	if len(got) != 2 || got[0] != "red" || got[1] != "green" {
		t.Fatalf("expected [red green], got %v", got)
	}
}

func TestOverlay_Enum_WidenRejected(t *testing.T) {
	base := fixtureRegistry(t)
	store := newInMemoryStore()
	store.set("t1", "wc", "machine", ClassOverride{
		AttributeOverrides: map[string]AttributeOverride{
			"color": {Values: []string{"red", "purple"}}, // purple not in global
		},
	})
	ta := NewTenantAware(base, store)

	_, err := ta.WithTenant(context.Background(), "t1").GetClass("wc", "machine")
	if err == nil || !strings.Contains(err.Error(), "CLASSREGISTRY_OVERRIDE_VALUES_WIDEN") {
		t.Fatalf("expected VALUES_WIDEN error, got %v", err)
	}
}

func TestOverlay_Numeric_RaiseMinLowerMaxAllowed(t *testing.T) {
	base := fixtureRegistry(t)
	store := newInMemoryStore()
	store.set("t1", "wc", "machine", ClassOverride{
		AttributeOverrides: map[string]AttributeOverride{
			"horsepower": {Min: floatPtr(50), Max: floatPtr(250)},
		},
	})
	ta := NewTenantAware(base, store)

	cd, err := ta.WithTenant(context.Background(), "t1").GetClass("wc", "machine")
	if err != nil {
		t.Fatalf("GetClass: %v", err)
	}
	hp := cd.Attributes["horsepower"]
	if hp.Min == nil || *hp.Min != 50 {
		t.Fatalf("expected narrowed min=50, got %v", hp.Min)
	}
	if hp.Max == nil || *hp.Max != 250 {
		t.Fatalf("expected narrowed max=250, got %v", hp.Max)
	}
}

func TestOverlay_Numeric_LowerMinRejected(t *testing.T) {
	base := fixtureRegistry(t)
	store := newInMemoryStore()
	store.set("t1", "wc", "machine", ClassOverride{
		AttributeOverrides: map[string]AttributeOverride{
			"horsepower": {Min: floatPtr(5)}, // global min is 10
		},
	})
	ta := NewTenantAware(base, store)

	_, err := ta.WithTenant(context.Background(), "t1").GetClass("wc", "machine")
	if err == nil || !strings.Contains(err.Error(), "CLASSREGISTRY_OVERRIDE_MIN_WIDENS") {
		t.Fatalf("expected MIN_WIDENS, got %v", err)
	}
}

func TestOverlay_Numeric_HigherMaxRejected(t *testing.T) {
	base := fixtureRegistry(t)
	store := newInMemoryStore()
	store.set("t1", "wc", "machine", ClassOverride{
		AttributeOverrides: map[string]AttributeOverride{
			"horsepower": {Max: floatPtr(1000)},
		},
	})
	ta := NewTenantAware(base, store)

	_, err := ta.WithTenant(context.Background(), "t1").GetClass("wc", "machine")
	if err == nil || !strings.Contains(err.Error(), "CLASSREGISTRY_OVERRIDE_MAX_WIDENS") {
		t.Fatalf("expected MAX_WIDENS, got %v", err)
	}
}

func TestOverlay_Numeric_MinExceedsMaxRejected(t *testing.T) {
	base := fixtureRegistry(t)
	store := newInMemoryStore()
	store.set("t1", "wc", "machine", ClassOverride{
		AttributeOverrides: map[string]AttributeOverride{
			"horsepower": {Min: floatPtr(400), Max: floatPtr(100)},
		},
	})
	ta := NewTenantAware(base, store)

	_, err := ta.WithTenant(context.Background(), "t1").GetClass("wc", "machine")
	if err == nil || !strings.Contains(err.Error(), "CLASSREGISTRY_OVERRIDE_MIN_EXCEEDS_MAX") {
		t.Fatalf("expected MIN_EXCEEDS_MAX, got %v", err)
	}
}

func TestOverlay_Required_TightenAllowed_RelaxRejected(t *testing.T) {
	base := fixtureRegistry(t)
	store := newInMemoryStore()
	// Tighten: color is not required globally; tenant flips to true.
	store.set("t1", "wc", "machine", ClassOverride{
		AttributeOverrides: map[string]AttributeOverride{
			"color": {Required: boolPtr(true)},
		},
	})
	ta := NewTenantAware(base, store)
	cd, err := ta.WithTenant(context.Background(), "t1").GetClass("wc", "machine")
	if err != nil {
		t.Fatalf("tighten: %v", err)
	}
	if !cd.Attributes["color"].Required {
		t.Fatalf("expected required=true after tighten")
	}

	// Reversed: build a fresh fixture where color IS globally required,
	// then try to relax it.
	reqYAML := `
domain: wc
base_classes: {}
classes:
  machine:
    label: Machine
    attributes:
      color:
        type: enum
        values: [red, green]
        required: true
`
	base2, _ := LoadBytes(map[string][]byte{"wc": []byte(reqYAML)})
	store2 := newInMemoryStore()
	store2.set("t1", "wc", "machine", ClassOverride{
		AttributeOverrides: map[string]AttributeOverride{
			"color": {Required: boolPtr(false)},
		},
	})
	ta2 := NewTenantAware(base2, store2)
	_, err = ta2.WithTenant(context.Background(), "t1").GetClass("wc", "machine")
	if err == nil || !strings.Contains(err.Error(), "CLASSREGISTRY_OVERRIDE_WIDENS_REQUIRED") {
		t.Fatalf("expected WIDENS_REQUIRED, got %v", err)
	}
}

func TestOverlay_UnknownAttributeRejected(t *testing.T) {
	base := fixtureRegistry(t)
	store := newInMemoryStore()
	store.set("t1", "wc", "machine", ClassOverride{
		AttributeOverrides: map[string]AttributeOverride{
			"nonexistent": {Required: boolPtr(true)},
		},
	})
	ta := NewTenantAware(base, store)
	_, err := ta.WithTenant(context.Background(), "t1").GetClass("wc", "machine")
	if err == nil || !strings.Contains(err.Error(), "CLASSREGISTRY_OVERRIDE_UNKNOWN_ATTRIBUTE") {
		t.Fatalf("expected UNKNOWN_ATTRIBUTE, got %v", err)
	}
}

func TestOverlay_DefaultKindMismatchRejected(t *testing.T) {
	base := fixtureRegistry(t)
	store := newInMemoryStore()
	store.set("t1", "wc", "machine", ClassOverride{
		AttributeOverrides: map[string]AttributeOverride{
			"horsepower": {Default: &AttributeValue{Kind: KindString, String: "fast"}},
		},
	})
	ta := NewTenantAware(base, store)
	_, err := ta.WithTenant(context.Background(), "t1").GetClass("wc", "machine")
	if err == nil || !strings.Contains(err.Error(), "CLASSREGISTRY_OVERRIDE_DEFAULT_KIND_MISMATCH") {
		t.Fatalf("expected DEFAULT_KIND_MISMATCH, got %v", err)
	}
}

func TestOverlay_TenantIsolation(t *testing.T) {
	base := fixtureRegistry(t)
	store := newInMemoryStore()
	store.set("t1", "wc", "machine", ClassOverride{
		AttributeOverrides: map[string]AttributeOverride{
			"color": {Values: []string{"red"}},
		},
	})
	store.set("t2", "wc", "machine", ClassOverride{
		AttributeOverrides: map[string]AttributeOverride{
			"color": {Values: []string{"blue"}},
		},
	})
	ta := NewTenantAware(base, store)

	cd1, _ := ta.WithTenant(context.Background(), "t1").GetClass("wc", "machine")
	cd2, _ := ta.WithTenant(context.Background(), "t2").GetClass("wc", "machine")
	if cd1.Attributes["color"].Values[0] != "red" {
		t.Fatalf("t1: expected red, got %v", cd1.Attributes["color"].Values)
	}
	if cd2.Attributes["color"].Values[0] != "blue" {
		t.Fatalf("t2: expected blue, got %v", cd2.Attributes["color"].Values)
	}

	// Global-view passthrough (no WithTenant) still shows all 4.
	cdGlobal, _ := ta.GetClass("wc", "machine")
	if len(cdGlobal.Attributes["color"].Values) != 4 {
		t.Fatalf("global: expected 4 values, got %v", cdGlobal.Attributes["color"].Values)
	}
}

func TestOverlay_ValidateAttributes_UsesNarrowedSpec(t *testing.T) {
	base := fixtureRegistry(t)
	store := newInMemoryStore()
	store.set("t1", "wc", "machine", ClassOverride{
		AttributeOverrides: map[string]AttributeOverride{
			"color": {Values: []string{"red", "green"}},
		},
	})
	ta := NewTenantAware(base, store)
	view := ta.WithTenant(context.Background(), "t1")

	// "blue" was valid globally but the tenant narrowed to {red, green}.
	err := view.ValidateAttributes("wc", "machine", map[string]AttributeValue{
		"color": {Kind: KindEnum, String: "blue"},
	})
	if err == nil || !strings.Contains(err.Error(), "CLASSREGISTRY_ENUM_VIOLATION") {
		t.Fatalf("expected ENUM_VIOLATION under narrowed spec, got %v", err)
	}

	// "red" is still valid.
	if err := view.ValidateAttributes("wc", "machine", map[string]AttributeValue{
		"color": {Kind: KindEnum, String: "red"},
	}); err != nil {
		t.Fatalf("red should pass narrowed spec, got %v", err)
	}
}

func TestOverlay_ListClasses_AppliesOverrides(t *testing.T) {
	base := fixtureRegistry(t)
	store := newInMemoryStore()
	store.set("t1", "wc", "machine", ClassOverride{
		AttributeOverrides: map[string]AttributeOverride{
			"horsepower": {Max: floatPtr(250)},
		},
	})
	ta := NewTenantAware(base, store)
	view := ta.WithTenant(context.Background(), "t1")

	classes := view.ListClasses("wc")
	if len(classes) != 1 {
		t.Fatalf("expected 1 class, got %d", len(classes))
	}
	got := classes[0].Attributes["horsepower"].Max
	if got == nil || *got != 250 {
		t.Fatalf("ListClasses didn't apply narrowing: got %v", got)
	}
}

func TestOverlay_StoreError_SurfacedOnGetClass(t *testing.T) {
	base := fixtureRegistry(t)
	store := newInMemoryStore()
	store.fail = errors.New("db down")
	ta := NewTenantAware(base, store)

	_, err := ta.WithTenant(context.Background(), "t1").GetClass("wc", "machine")
	if err == nil || !strings.Contains(err.Error(), "CLASSREGISTRY_OVERRIDE_STORE_READ") {
		t.Fatalf("expected STORE_READ error, got %v", err)
	}
}
