package classregistry

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// inMemoryEntityStore is a map-backed EntityStore used for resolver
// tests. Production code uses packages/classregistry/pgstore.
type inMemoryEntityStore struct {
	mu sync.Mutex
	// key: tenant|domain|class|naturalKey
	rows map[string]*ClassEntity
	fail error
}

func newInMemEntityStore() *inMemoryEntityStore {
	return &inMemoryEntityStore{rows: map[string]*ClassEntity{}}
}

func ekey(t, d, c, nk string) string { return t + "|" + d + "|" + c + "|" + nk }

func (s *inMemoryEntityStore) GetByNaturalKey(_ context.Context, t, d, c, nk string) (*ClassEntity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return nil, s.fail
	}
	e, ok := s.rows[ekey(t, d, c, nk)]
	if !ok {
		return nil, nil
	}
	return e, nil
}

func (s *inMemoryEntityStore) Exists(_ context.Context, t, d, c, nk string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return false, s.fail
	}
	e, ok := s.rows[ekey(t, d, c, nk)]
	return ok && e.Status == "active", nil
}

func (s *inMemoryEntityStore) List(_ context.Context, f EntityListFilter) ([]*ClassEntity, int32, error) {
	return nil, 0, nil
}

func (s *inMemoryEntityStore) Upsert(_ context.Context, e *ClassEntity, _ string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.Status == "" {
		e.Status = "active"
	}
	if e.ID == "" {
		e.ID = "ent-" + e.NaturalKey
	}
	s.rows[ekey(e.TenantID, e.Domain, e.Class, e.NaturalKey)] = e
	return e.ID, nil
}

func (s *inMemoryEntityStore) Archive(_ context.Context, t, d, c, nk, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.rows[ekey(t, d, c, nk)]; ok {
		e.Status = "archived"
		return nil
	}
	return errors.New("not found")
}

func (s *inMemoryEntityStore) Delete(_ context.Context, t, d, c, nk, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, ekey(t, d, c, nk))
	return nil
}

// ---------- Fixtures ----------

// resolverFixture returns a registry with a target class to be looked
// up + a "referencing" class that carries a reference attribute, plus
// an entity store prepopulated with a matching row.
func resolverFixture(t *testing.T, tenant, naturalKey string) (*LookupResolver, *inMemoryEntityStore) {
	t.Helper()
	// Target class: ppa_contract, domain=contracts
	// Referencing class: solar_farm, domain=asset, with attribute ppa_counterparty
	y := map[string][]byte{
		"contracts": []byte(`
domain: contracts
base_classes: {}
classes:
  ppa_contract:
    label: PPA Contract
    attributes:
      tariff_inr_per_kwh:
        type: decimal
`),
		"asset": []byte(`
domain: asset
base_classes: {}
classes:
  solar_farm:
    label: Solar Farm
    attributes:
      ppa_counterparty:
        type: reference
        lookup: contracts/ppa_contract
`),
	}
	reg, err := LoadBytes(y)
	if err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	store := newInMemEntityStore()
	if naturalKey != "" {
		if _, err := store.Upsert(context.Background(), &ClassEntity{
			TenantID:   tenant,
			Domain:     "contracts",
			Class:      "ppa_contract",
			NaturalKey: naturalKey,
			Label:      "PPA — " + naturalKey,
			Attributes: map[string]AttributeValue{},
		}, "test-actor"); err != nil {
			t.Fatalf("seed entity: %v", err)
		}
	}
	return NewLookupResolver(reg, store), store
}

// ---------- Tests ----------

func TestResolver_HappyPath(t *testing.T) {
	r, _ := resolverFixture(t, "t1", "PPA-2026-001")
	err := r.ValidateReference(
		context.Background(),
		"t1",
		"asset", "solar_farm",
		"ppa_counterparty",
		"contracts/ppa_contract",
		"PPA-2026-001",
	)
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestResolver_MissingEntityRejected(t *testing.T) {
	r, _ := resolverFixture(t, "t1", "")
	err := r.ValidateReference(
		context.Background(),
		"t1",
		"asset", "solar_farm",
		"ppa_counterparty",
		"contracts/ppa_contract",
		"PPA-NOPE",
	)
	if err == nil || !strings.Contains(err.Error(), "CLASSREGISTRY_REFERENCE_NOT_FOUND") {
		t.Fatalf("expected REFERENCE_NOT_FOUND, got %v", err)
	}
}

func TestResolver_ArchivedEntityRejected(t *testing.T) {
	r, store := resolverFixture(t, "t1", "PPA-ARCH-1")
	_ = store.Archive(context.Background(), "t1", "contracts", "ppa_contract", "PPA-ARCH-1", "actor")
	err := r.ValidateReference(
		context.Background(),
		"t1",
		"asset", "solar_farm",
		"ppa_counterparty",
		"contracts/ppa_contract",
		"PPA-ARCH-1",
	)
	if err == nil || !strings.Contains(err.Error(), "CLASSREGISTRY_REFERENCE_NOT_FOUND") {
		t.Fatalf("archived entities must fail reference check, got %v", err)
	}
}

func TestResolver_TenantIsolation(t *testing.T) {
	r, store := resolverFixture(t, "t1", "PPA-ISO-1")
	// Tenant t2 has NO ppa_contract with that natural_key.
	_ = store // keep
	err := r.ValidateReference(
		context.Background(),
		"t2",
		"asset", "solar_farm",
		"ppa_counterparty",
		"contracts/ppa_contract",
		"PPA-ISO-1",
	)
	if err == nil || !strings.Contains(err.Error(), "CLASSREGISTRY_REFERENCE_NOT_FOUND") {
		t.Fatalf("cross-tenant lookup must fail, got %v", err)
	}
}

func TestResolver_UnknownLookupTargetRejected(t *testing.T) {
	r, _ := resolverFixture(t, "t1", "PPA-X")
	err := r.ValidateReference(
		context.Background(),
		"t1",
		"asset", "solar_farm",
		"ppa_counterparty",
		"contracts/atlantis", // unregistered class
		"PPA-X",
	)
	if err == nil || !strings.Contains(err.Error(), "CLASSREGISTRY_REFERENCE_TARGET_UNKNOWN") {
		t.Fatalf("unknown lookup target must be rejected, got %v", err)
	}
}

func TestResolver_EmptyValueRejected(t *testing.T) {
	r, _ := resolverFixture(t, "t1", "PPA-1")
	err := r.ValidateReference(
		context.Background(),
		"t1",
		"asset", "solar_farm",
		"ppa_counterparty",
		"contracts/ppa_contract",
		"",
	)
	if err == nil || !strings.Contains(err.Error(), "CLASSREGISTRY_REFERENCE_EMPTY") {
		t.Fatalf("empty reference value must be rejected, got %v", err)
	}
}

func TestResolver_BareLookupUsesTargetDomain(t *testing.T) {
	// When lookup is bare ("ppa_contract" not "contracts/ppa_contract"),
	// the resolver defaults to the REFERENCING class's own domain.
	// Here that's "asset", which has no ppa_contract — so the call
	// surfaces a target-unknown error. Documents the behavior.
	r, _ := resolverFixture(t, "t1", "PPA-1")
	err := r.ValidateReference(
		context.Background(),
		"t1",
		"asset", "solar_farm",
		"ppa_counterparty",
		"ppa_contract", // bare — resolver tries asset/ppa_contract
		"PPA-1",
	)
	if err == nil || !strings.Contains(err.Error(), "CLASSREGISTRY_REFERENCE_TARGET_UNKNOWN") {
		t.Fatalf("bare target resolves to same-domain, so this should be TARGET_UNKNOWN, got %v", err)
	}
}

func TestResolver_NoStoreRejected(t *testing.T) {
	reg, err := LoadBytes(map[string][]byte{"x": []byte(`domain: x
base_classes: {}
classes:
  y:
    attributes:
      z:
        type: string
`)})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	r := NewLookupResolver(reg, nil)
	err = r.ValidateReference(context.Background(), "t1", "x", "y", "z", "x/y", "v")
	if err == nil || !strings.Contains(err.Error(), "CLASSREGISTRY_RESOLVER_NO_STORE") {
		t.Fatalf("nil store must be rejected on use, got %v", err)
	}
}

func TestResolver_StoreErrorSurfaced(t *testing.T) {
	r, store := resolverFixture(t, "t1", "PPA-1")
	store.fail = errors.New("db down")
	err := r.ValidateReference(
		context.Background(), "t1",
		"asset", "solar_farm", "ppa_counterparty",
		"contracts/ppa_contract", "PPA-1",
	)
	if err == nil || !strings.Contains(err.Error(), "CLASSREGISTRY_RESOLVER_STORE_READ") {
		t.Fatalf("store error must surface as STORE_READ, got %v", err)
	}
}
