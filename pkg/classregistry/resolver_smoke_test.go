package classregistry

import (
	"context"
	"path/filepath"
	"testing"
)

// TestResolver_PilotLoadsContractsAndResolvesPPA is the end-to-end
// smoke test for Layer 2: load the real shipped contracts.yaml
// against the real class registry, seed a ppa_contract row into the
// in-memory entity store, and prove the resolver answers a
// cross-class lookup correctly. Documents the "close the 11+ dangling
// ppa_contract references" win from docs/VERTICAL_GAP_ANALYSIS.md §3.
func TestResolver_PilotLoadsContractsAndResolvesPPA(t *testing.T) {
	root := filepath.Join("..", "..")
	reg, err := NewLoader(filepath.Join(root, "config", "class_registry")).Load()
	if err != nil {
		t.Fatalf("load real class registry: %v", err)
	}
	if _, err := reg.GetClass("contracts", "ppa_contract"); err != nil {
		t.Fatalf("contracts/ppa_contract not registered: %v", err)
	}

	store := newInMemEntityStore()
	_, err = store.Upsert(context.Background(), &ClassEntity{
		TenantID:   "tenant-solar-ind-1",
		Domain:     "contracts",
		Class:      "ppa_contract",
		NaturalKey: "PPA-2026-SOLAR-001",
		Label:      "PPA 25-yr @ ₹2.75/kWh — NTPC offtake",
		Attributes: map[string]AttributeValue{
			"contract_number":   {Kind: KindString, String: "PPA-2026-SOLAR-001"},
			"counterparty_name": {Kind: KindString, String: "NTPC Ltd"},
			"effective_date":    {Kind: KindDate},
			"tariff_inr_per_kwh": {Kind: KindDecimal, Decimal: "2.75"},
			"tenure_years":      {Kind: KindInt, Int: 25},
			"offtaker_type":     {Kind: KindEnum, String: "cpsu"},
			"status":            {Kind: KindEnum, String: "active"},
		},
	}, "admin-seed")
	if err != nil {
		t.Fatalf("seed PPA: %v", err)
	}

	r := NewLookupResolver(reg, store)
	if err := r.ValidateReference(
		context.Background(),
		"tenant-solar-ind-1",
		"asset", "solar_farm",
		"ppa_counterparty",
		"contracts/ppa_contract",
		"PPA-2026-SOLAR-001",
	); err != nil {
		t.Fatalf("resolver failed for seeded PPA: %v", err)
	}

	// Negative control: different tenant must NOT see tenant-solar-ind-1's PPA.
	if err := r.ValidateReference(
		context.Background(),
		"tenant-other",
		"asset", "solar_farm",
		"ppa_counterparty",
		"contracts/ppa_contract",
		"PPA-2026-SOLAR-001",
	); err == nil {
		t.Fatalf("cross-tenant lookup must fail")
	}
}
