package classregistry

import (
	"context"
	"strings"
	"testing"
)

func tvFixture(t *testing.T, tenant, naturalKey string) (*TenantValidator, *inMemoryEntityStore) {
	t.Helper()
	y := map[string][]byte{
		"contracts": []byte(`
domain: contracts
base_classes: {}
classes:
  ppa_contract:
    label: PPA Contract
    attributes:
      tariff:
        type: decimal
`),
		"asset": []byte(`
domain: asset
base_classes: {}
classes:
  solar_farm:
    label: Solar Farm
    attributes:
      name:
        type: string
        required: true
      ppa_counterparty:
        type: reference
        lookup: contracts/ppa_contract
`),
	}
	reg, err := LoadBytes(y)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	store := newInMemEntityStore()
	if naturalKey != "" {
		if _, err := store.Upsert(context.Background(), &ClassEntity{
			TenantID:   tenant,
			Domain:     "contracts",
			Class:      "ppa_contract",
			NaturalKey: naturalKey,
			Label:      "PPA " + naturalKey,
			Attributes: map[string]AttributeValue{},
		}, "test"); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	v := NewTenantValidator(
		context.Background(),
		reg,
		NewLookupResolver(reg, store),
		tenant,
	)
	return v, store
}

func TestTenantValidator_ReferenceExists_Passes(t *testing.T) {
	v, _ := tvFixture(t, "t1", "PPA-1")
	err := v.ValidateAttributes("asset", "solar_farm", map[string]AttributeValue{
		"name":             {Kind: KindString, String: "Plant A"},
		"ppa_counterparty": {Kind: KindReference, String: "PPA-1"},
	})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestTenantValidator_ReferenceMissing_Fails(t *testing.T) {
	v, _ := tvFixture(t, "t1", "PPA-1")
	err := v.ValidateAttributes("asset", "solar_farm", map[string]AttributeValue{
		"name":             {Kind: KindString, String: "Plant A"},
		"ppa_counterparty": {Kind: KindReference, String: "PPA-DOES-NOT-EXIST"},
	})
	if err == nil || !strings.Contains(err.Error(), "CLASSREGISTRY_REFERENCE_NOT_FOUND") {
		t.Fatalf("expected REFERENCE_NOT_FOUND, got %v", err)
	}
}

func TestTenantValidator_ShapeErrorPreemptsResolver(t *testing.T) {
	v, _ := tvFixture(t, "t1", "PPA-1")
	// Missing required "name" attribute — shape validation must fail
	// before the resolver runs (we don't query the DB for a payload
	// that's already known-invalid).
	err := v.ValidateAttributes("asset", "solar_farm", map[string]AttributeValue{
		"ppa_counterparty": {Kind: KindReference, String: "PPA-DOES-NOT-EXIST"},
	})
	if err == nil {
		t.Fatalf("expected shape error")
	}
	if !strings.Contains(err.Error(), "CLASSREGISTRY_MISSING_REQUIRED") {
		t.Fatalf("expected MISSING_REQUIRED (shape error), got %v", err)
	}
}

func TestTenantValidator_EmptyReferenceAccepted(t *testing.T) {
	// ppa_counterparty is not required, so empty is ok.
	v, _ := tvFixture(t, "t1", "")
	err := v.ValidateAttributes("asset", "solar_farm", map[string]AttributeValue{
		"name": {Kind: KindString, String: "Plant A"},
		// ppa_counterparty intentionally absent
	})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestTenantValidator_NilResolverFallsBackToShapeOnly(t *testing.T) {
	y := map[string][]byte{
		"asset": []byte(`
domain: asset
base_classes: {}
classes:
  solar_farm:
    label: Solar Farm
    attributes:
      name:
        type: string
      ppa_counterparty:
        type: reference
        lookup: contracts/ppa_contract
`),
	}
	reg, _ := LoadBytes(y)
	v := NewTenantValidator(context.Background(), reg, nil, "t1")
	// Without a resolver, any non-empty reference value is accepted
	// (the Layer 2 MVP default).
	err := v.ValidateAttributes("asset", "solar_farm", map[string]AttributeValue{
		"name":             {Kind: KindString, String: "Plant A"},
		"ppa_counterparty": {Kind: KindReference, String: "PPA-WHATEVER"},
	})
	if err != nil {
		t.Fatalf("nil resolver should accept any non-empty ref, got %v", err)
	}
}

func TestTenantValidator_EmptyTenantIDFallsBack(t *testing.T) {
	y := map[string][]byte{
		"asset": []byte(`
domain: asset
base_classes: {}
classes:
  solar_farm:
    label: Solar Farm
    attributes:
      name:
        type: string
      ppa_counterparty:
        type: reference
        lookup: contracts/ppa_contract
`),
	}
	reg, _ := LoadBytes(y)
	store := newInMemEntityStore()
	v := NewTenantValidator(context.Background(), reg, NewLookupResolver(reg, store), "")
	// Empty tenant — degrades to base behavior.
	err := v.ValidateAttributes("asset", "solar_farm", map[string]AttributeValue{
		"name":             {Kind: KindString, String: "Plant A"},
		"ppa_counterparty": {Kind: KindReference, String: "PPA-WHATEVER"},
	})
	if err != nil {
		t.Fatalf("empty tenant should fall back to base, got %v", err)
	}
}

func TestTenantValidator_StringsVariantChecksReferences(t *testing.T) {
	v, _ := tvFixture(t, "t1", "PPA-1")
	typed, err := v.ValidateAttributesFromStrings("asset", "solar_farm", map[string]string{
		"name":             "Plant A",
		"ppa_counterparty": "PPA-1",
	})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if len(typed) != 2 {
		t.Fatalf("expected 2 typed values, got %d", len(typed))
	}
}

func TestTenantValidator_StringsVariantFailsOnMissingRef(t *testing.T) {
	v, _ := tvFixture(t, "t1", "PPA-1")
	_, err := v.ValidateAttributesFromStrings("asset", "solar_farm", map[string]string{
		"name":             "Plant A",
		"ppa_counterparty": "PPA-NOPE",
	})
	if err == nil || !strings.Contains(err.Error(), "CLASSREGISTRY_REFERENCE_NOT_FOUND") {
		t.Fatalf("expected REFERENCE_NOT_FOUND, got %v", err)
	}
}
