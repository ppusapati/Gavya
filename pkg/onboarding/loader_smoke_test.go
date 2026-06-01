package onboarding

import (
	"path/filepath"
	"testing"

	"p9e.in/samavaya/packages/classregistry"
)

// TestLoader_LoadsShippedProfile verifies the committed smoke-test
// profile under config/industry_profiles/ loads cleanly against the
// real global class registry. Catches drift between profile YAML and
// classregistry YAML — e.g. a class renamed in equipment.yaml breaks
// this test if the profile still references the old name.
//
// Test layout: we walk up from packages/onboarding to the repo root
// and point the loader at config/class_registry + config/industry_profiles.
func TestLoader_LoadsShippedProfile(t *testing.T) {
	// packages/onboarding is two levels below repo root.
	root := filepath.Join("..", "..")
	reg, err := classregistry.NewLoader(filepath.Join(root, "config", "class_registry")).Load()
	if err != nil {
		t.Fatalf("load real class registry: %v", err)
	}
	profiles, err := NewLoader(filepath.Join(root, "config", "industry_profiles"), reg).Load()
	if err != nil {
		t.Fatalf("load shipped profiles: %v", err)
	}
	if len(profiles) == 0 {
		t.Fatalf("expected at least one shipped profile, found 0")
	}
	p, ok := profiles["manufacturing_discrete"]
	if !ok {
		t.Fatalf("expected manufacturing_discrete profile to be shipped")
	}
	if len(p.EnabledClasses) == 0 {
		t.Fatalf("manufacturing_discrete should declare enabled_classes")
	}
	if p.Label == "" || p.Version == "" {
		t.Fatalf("manufacturing_discrete missing required fields: %+v", p)
	}
}
