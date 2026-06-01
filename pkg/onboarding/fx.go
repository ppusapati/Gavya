package onboarding

import (
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"p9e.in/samavaya/packages/classregistry"
	"p9e.in/samavaya/packages/classregistry/pgstore"
)

// Module wires packages/onboarding into the fx graph. It expects a
// pgxpool.Pool (for the provisioning-state store), a
// classregistry.Registry (for profile load-time validation), and a
// *pgstore.Store (for writing per-tenant overrides).
//
// SeedMasterWriter, ProcessEnabler, and EventEmitter are OPTIONAL. The
// composition root binds them when available; absent bindings cause
// the provisioner to fail fast (seed masters) or no-op (processes,
// events) per the Provisioner contract.
//
// The profile directory defaults to `config/industry_profiles`; set
// INDUSTRY_PROFILES_DIR to override. A missing directory loads zero
// profiles — the provisioner then rejects every Provision call with
// ONBOARDING_UNKNOWN_INDUSTRY, which is the correct pre-rollout
// behavior (better than silently succeeding without doing anything).
var Module = fx.Module("onboarding",
	fx.Provide(
		newProfilesFromEnv,
		newProvisioner,
	),
)

// ProvideProfiles lets callers pass an explicit directory rather than
// relying on INDUSTRY_PROFILES_DIR. Useful for tests + split
// deployments that bundle profiles at a known path.
func ProvideProfiles(dir string) fx.Option {
	return fx.Module("onboarding-with-dir",
		fx.Provide(func(reg classregistry.Registry) (map[string]*Profile, error) {
			return NewLoader(dir, reg).Load()
		}),
		fx.Provide(newProvisioner),
	)
}

// newProfilesFromEnv is the default profile provider. Reads
// INDUSTRY_PROFILES_DIR (or falls back to config/industry_profiles).
func newProfilesFromEnv(reg classregistry.Registry) (map[string]*Profile, error) {
	dir := os.Getenv("INDUSTRY_PROFILES_DIR")
	if dir == "" {
		dir = "config/industry_profiles"
	}
	profiles, err := NewLoader(dir, reg).Load()
	if err != nil {
		return nil, fmt.Errorf("onboarding: load %s: %w", dir, err)
	}
	return profiles, nil
}

// onboardingDeps bundles the pieces the provisioner constructor pulls
// from the fx graph. Optional dependencies carry `optional:"true"`
// tags so a pre-F.6.20 deployment that hasn't wired event emission or
// seed writing yet still starts.
type onboardingDeps struct {
	fx.In

	Pool     *pgxpool.Pool
	Registry classregistry.Registry
	Overrides *pgstore.Store
	Profiles map[string]*Profile

	Seeds     SeedMasterWriter `optional:"true"`
	Processes ProcessEnabler   `optional:"true"`
	Events    EventEmitter     `optional:"true"`
}

func newProvisioner(d onboardingDeps) (*Provisioner, error) {
	return NewProvisioner(ProvisionerConfig{
		Profiles:  d.Profiles,
		Registry:  d.Registry,
		Overrides: d.Overrides,
		State:     NewPgStateStore(d.Pool),
		Seeds:     d.Seeds,
		Processes: d.Processes,
		Events:    d.Events,
	})
}
