package onboarding

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"p9e.in/samavaya/packages/classregistry"
	"p9e.in/samavaya/packages/classregistry/pgstore"
)

// ----------------------------------------------------------------------------
// Fakes for provisioner unit tests
// ----------------------------------------------------------------------------

type fakeOverrideWriter struct {
	mu    sync.Mutex
	calls []pgstore.UpsertInput
	fail  error
}

func (f *fakeOverrideWriter) UpsertOverride(_ context.Context, in pgstore.UpsertInput) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return "", f.fail
	}
	f.calls = append(f.calls, in)
	return fmt.Sprintf("ov-%d", len(f.calls)), nil
}

type fakeSeedWriter struct {
	mu    sync.Mutex
	calls []struct{ tenant, domain, table string; row map[string]string }
	fail  error
}

func (f *fakeSeedWriter) InsertSeedRow(_ context.Context, tenant, domain, table string, row map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.calls = append(f.calls, struct {
		tenant, domain, table string
		row                   map[string]string
	}{tenant, domain, table, row})
	return nil
}

type fakeEvents struct {
	mu    sync.Mutex
	calls []struct{ tenant, industry, version string }
}

func (f *fakeEvents) TenantProvisioned(_ context.Context, tenant, industry, version string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, struct{ tenant, industry, version string }{tenant, industry, version})
	return nil
}

// memStateStore is an in-memory ProvisioningStateStore. It tracks per-
// (tenant, industry) state and per-step completion so idempotency +
// resume-after-failure tests can run without Postgres.
type memStateStore struct {
	mu         sync.Mutex
	byScope    map[string]*memState    // key = tenant|industry
	byID       map[string]*memState    // key = provisioning_id
	stepsByID  map[string]map[string]string // provID -> stepKey -> state ("completed"/"failed")
	nextSeq    int
}

type memState struct {
	ID            string
	TenantID      string
	IndustryCode  string
	Version       string
	State         string
	LastError     string
}

func newMemStateStore() *memStateStore {
	return &memStateStore{
		byScope:   map[string]*memState{},
		byID:      map[string]*memState{},
		stepsByID: map[string]map[string]string{},
	}
}

func (s *memStateStore) key(t, i string) string { return t + "|" + i }
func (s *memStateStore) stepK(k, kk string) string { return k + "|" + kk }

func (s *memStateStore) LoadOrStart(_ context.Context, tenant, industry, version, _ string) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.byScope[s.key(tenant, industry)]; ok {
		prior := st.State
		if prior != "completed" {
			st.State = "running"
			st.LastError = ""
		}
		return st.ID, prior, nil
	}
	s.nextSeq++
	id := fmt.Sprintf("prov-%d", s.nextSeq)
	st := &memState{ID: id, TenantID: tenant, IndustryCode: industry, Version: version, State: "running"}
	s.byScope[s.key(tenant, industry)] = st
	s.byID[id] = st
	s.stepsByID[id] = map[string]string{}
	return id, "", nil
}

func (s *memStateStore) CompleteStep(_ context.Context, provID, kind, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.stepsByID[provID]; !ok {
		s.stepsByID[provID] = map[string]string{}
	}
	s.stepsByID[provID][s.stepK(kind, key)] = "completed"
	return nil
}

func (s *memStateStore) FailStep(_ context.Context, provID, kind, key, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.stepsByID[provID]; !ok {
		s.stepsByID[provID] = map[string]string{}
	}
	if _, exists := s.stepsByID[provID][s.stepK(kind, key)]; exists {
		return nil // idempotent
	}
	s.stepsByID[provID][s.stepK(kind, key)] = "failed"
	return nil
}

func (s *memStateStore) IsStepCompleted(_ context.Context, provID, kind, key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stepsByID[provID][s.stepK(kind, key)] == "completed", nil
}

func (s *memStateStore) MarkCompleted(_ context.Context, _ /*tenantID*/, provID, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.byID[provID]; ok {
		st.State = "completed"
		st.LastError = ""
	}
	return nil
}

func (s *memStateStore) MarkFailed(_ context.Context, _ /*tenantID*/, provID, _, errorMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.byID[provID]; ok {
		st.State = "failed"
		st.LastError = errorMsg
	}
	return nil
}

// ----------------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------------

func fixtureProvisioner(t *testing.T) (*Provisioner, *fakeOverrideWriter, *fakeSeedWriter, *fakeEvents, *memStateStore) {
	t.Helper()
	reg := tinyRegistry(t)
	profile := &Profile{
		IndustryCode: "manufacturing_discrete",
		Label:        "Discrete Mfg",
		Version:      "1.0.0",
		EnabledClasses: []EnabledClass{
			{Domain: "eq", Class: "cnc_machine"},
		},
		SeedMasters: []SeedMaster{
			{Domain: "eq", Table: "cnc_templates", Rows: []map[string]string{
				{"name": "default_5axis"},
				{"name": "default_3axis"},
			}},
		},
		EnabledProcesses: []string{"depreciation"},
	}
	ow := &fakeOverrideWriter{}
	sw := &fakeSeedWriter{}
	ev := &fakeEvents{}
	state := newMemStateStore()
	pv, err := NewProvisioner(ProvisionerConfig{
		Profiles:  map[string]*Profile{profile.IndustryCode: profile},
		Registry:  reg,
		Overrides: ow,
		State:     state,
		Seeds:     sw,
		Events:    ev,
	})
	if err != nil {
		t.Fatalf("NewProvisioner: %v", err)
	}
	return pv, ow, sw, ev, state
}

func TestProvisioner_HappyPath(t *testing.T) {
	pv, _, sw, ev, state := fixtureProvisioner(t)
	err := pv.Provision(context.Background(), ProvisionInput{
		TenantID:     "t1",
		IndustryCode: "manufacturing_discrete",
		ActorID:      "admin-1",
	})
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if len(sw.calls) != 2 {
		t.Fatalf("expected 2 seed rows, got %d", len(sw.calls))
	}
	if len(ev.calls) != 1 {
		t.Fatalf("expected 1 tenant-provisioned event, got %d", len(ev.calls))
	}
	st := state.byScope["t1|manufacturing_discrete"]
	if st == nil || st.State != "completed" {
		t.Fatalf("expected state=completed, got %+v", st)
	}
}

func TestProvisioner_Idempotent(t *testing.T) {
	pv, _, sw, ev, _ := fixtureProvisioner(t)
	ctx := context.Background()
	in := ProvisionInput{TenantID: "t1", IndustryCode: "manufacturing_discrete", ActorID: "admin-1"}

	if err := pv.Provision(ctx, in); err != nil {
		t.Fatalf("first Provision: %v", err)
	}
	seedsAfterFirst := len(sw.calls)
	eventsAfterFirst := len(ev.calls)

	// Second call — should be a no-op (tenant already 'completed').
	if err := pv.Provision(ctx, in); err != nil {
		t.Fatalf("second Provision: %v", err)
	}
	if len(sw.calls) != seedsAfterFirst {
		t.Fatalf("second Provision wrote more seeds: got %d, want %d", len(sw.calls), seedsAfterFirst)
	}
	if len(ev.calls) != eventsAfterFirst {
		t.Fatalf("second Provision emitted more events: got %d, want %d", len(ev.calls), eventsAfterFirst)
	}
}

func TestProvisioner_ResumeAfterSeedFailure(t *testing.T) {
	pv, _, sw, _, state := fixtureProvisioner(t)
	ctx := context.Background()
	in := ProvisionInput{TenantID: "t1", IndustryCode: "manufacturing_discrete", ActorID: "admin-1"}

	// First run: seed writer fails.
	sw.fail = errors.New("db blip")
	if err := pv.Provision(ctx, in); err == nil {
		t.Fatalf("expected Provision to fail on seed error")
	}
	st := state.byScope["t1|manufacturing_discrete"]
	if st == nil || st.State != "failed" {
		t.Fatalf("expected state=failed after seed error, got %+v", st)
	}
	if len(sw.calls) != 0 {
		t.Fatalf("no seeds should have persisted, got %d", len(sw.calls))
	}

	// Second run: writer recovers.
	sw.fail = nil
	if err := pv.Provision(ctx, in); err != nil {
		t.Fatalf("resumed Provision: %v", err)
	}
	if len(sw.calls) != 2 {
		t.Fatalf("expected 2 seeds on resume, got %d", len(sw.calls))
	}
	st = state.byScope["t1|manufacturing_discrete"]
	if st.State != "completed" {
		t.Fatalf("expected state=completed after resume, got %+v", st)
	}
}

func TestProvisioner_UnknownIndustryRejected(t *testing.T) {
	pv, _, _, _, _ := fixtureProvisioner(t)
	err := pv.Provision(context.Background(), ProvisionInput{
		TenantID: "t1", IndustryCode: "atlantis", ActorID: "admin-1",
	})
	if err == nil || !contains(err.Error(), "ONBOARDING_UNKNOWN_INDUSTRY") {
		t.Fatalf("expected UNKNOWN_INDUSTRY, got %v", err)
	}
}

func TestProvisioner_MissingRequiredInputRejected(t *testing.T) {
	pv, _, _, _, _ := fixtureProvisioner(t)
	cases := []struct {
		name string
		in   ProvisionInput
		code string
	}{
		{"no tenant", ProvisionInput{IndustryCode: "x", ActorID: "a"}, "ONBOARDING_MISSING_TENANT_ID"},
		{"no industry", ProvisionInput{TenantID: "t", ActorID: "a"}, "ONBOARDING_MISSING_INDUSTRY"},
		{"no actor", ProvisionInput{TenantID: "t", IndustryCode: "x"}, "ONBOARDING_MISSING_ACTOR"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := pv.Provision(context.Background(), c.in)
			if err == nil || !contains(err.Error(), c.code) {
				t.Fatalf("expected %s, got %v", c.code, err)
			}
		})
	}
}

func TestProvisioner_SeedMastersWithoutWriter_FailsLoud(t *testing.T) {
	reg := tinyRegistry(t)
	profile := &Profile{
		IndustryCode: "x",
		Label:        "X",
		Version:      "1.0.0",
		SeedMasters: []SeedMaster{
			{Domain: "eq", Table: "t", Rows: []map[string]string{{"a": "b"}}},
		},
	}
	state := newMemStateStore()
	pv, err := NewProvisioner(ProvisionerConfig{
		Profiles:  map[string]*Profile{"x": profile},
		Registry:  reg,
		Overrides: &fakeOverrideWriter{},
		State:     state,
		// Seeds intentionally omitted.
	})
	if err != nil {
		t.Fatalf("NewProvisioner: %v", err)
	}
	err = pv.Provision(context.Background(), ProvisionInput{TenantID: "t", IndustryCode: "x", ActorID: "a"})
	if err == nil || !contains(err.Error(), "ONBOARDING_NO_SEED_WRITER") {
		t.Fatalf("expected NO_SEED_WRITER, got %v", err)
	}
}

func TestProvisioner_NilRequiredDepsRejected(t *testing.T) {
	_, err := NewProvisioner(ProvisionerConfig{})
	if err == nil {
		t.Fatalf("expected error for empty config")
	}
}

func TestProvisioner_WritesOverridesWhenDeclared(t *testing.T) {
	reg := tinyRegistry(t)
	maxRPM := 30000.0
	profile := &Profile{
		IndustryCode: "x",
		Label:        "X",
		Version:      "1.0.0",
		EnabledClasses: []EnabledClass{
			{
				Domain: "eq",
				Class:  "cnc_machine",
				Overrides: &classregistry.ClassOverride{
					AttributeOverrides: map[string]classregistry.AttributeOverride{
						"rpm": {Max: &maxRPM},
					},
				},
			},
		},
	}
	ow := &fakeOverrideWriter{}
	state := newMemStateStore()
	pv, err := NewProvisioner(ProvisionerConfig{
		Profiles:  map[string]*Profile{"x": profile},
		Registry:  reg,
		Overrides: ow,
		State:     state,
	})
	if err != nil {
		t.Fatalf("NewProvisioner: %v", err)
	}
	if err := pv.Provision(context.Background(), ProvisionInput{
		TenantID: "t", IndustryCode: "x", ActorID: "a",
	}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if len(ow.calls) != 1 {
		t.Fatalf("expected 1 override write, got %d", len(ow.calls))
	}
	if ow.calls[0].Domain != "eq" || ow.calls[0].Class != "cnc_machine" {
		t.Fatalf("override targeting wrong class: %+v", ow.calls[0])
	}
}

func TestProvisioner_SkipsOverrideWhenOverrideMapEmpty(t *testing.T) {
	reg := tinyRegistry(t)
	profile := &Profile{
		IndustryCode: "x",
		Label:        "X",
		Version:      "1.0.0",
		EnabledClasses: []EnabledClass{
			// No Overrides field.
			{Domain: "eq", Class: "cnc_machine"},
		},
	}
	ow := &fakeOverrideWriter{}
	state := newMemStateStore()
	pv, _ := NewProvisioner(ProvisionerConfig{
		Profiles:  map[string]*Profile{"x": profile},
		Registry:  reg,
		Overrides: ow,
		State:     state,
	})
	if err := pv.Provision(context.Background(), ProvisionInput{
		TenantID: "t", IndustryCode: "x", ActorID: "a",
	}); err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if len(ow.calls) != 0 {
		t.Fatalf("expected no override writes, got %d", len(ow.calls))
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
