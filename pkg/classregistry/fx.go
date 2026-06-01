package classregistry

import (
	"fmt"
	"os"

	"go.uber.org/fx"
)

// Module is the default fx wiring for classregistry. It reads the path
// from the CLASS_REGISTRY_DIR env var (or falls back to
// ./config/class_registry) and provides a loaded Registry to the DI
// graph.
//
// Consumers take Registry as a constructor argument:
//
//	func NewMyService(reg classregistry.Registry) *MyService { ... }
//
// Or fx.Annotate-tag the class-registry root directory for alternate
// wiring (e.g. tests pointing at a fixture tree).
var Module = fx.Module("classregistry",
	fx.Provide(newRegistryFromEnv),
)

// ProvideRegistry lets callers pass an explicit directory rather than
// relying on the env var. Useful for tests and for split deployments
// that ship the class registry as a bundled asset at a known path.
func ProvideRegistry(dir string) fx.Option {
	return fx.Module("classregistry-with-dir",
		fx.Provide(func() (Registry, error) {
			return NewLoader(dir).Load()
		}),
	)
}

// newRegistryFromEnv is the default provider used by Module.
func newRegistryFromEnv() (Registry, error) {
	dir := os.Getenv("CLASS_REGISTRY_DIR")
	if dir == "" {
		dir = "config/class_registry"
	}
	// Allow empty-directory bootstrap — early in Phase F rollout most
	// domains don't have a registry file yet, and we want the app to
	// start regardless. The registry simply reports empty ListClasses
	// for those domains.
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return emptyRegistry(), nil
	}
	reg, err := NewLoader(dir).Load()
	if err != nil {
		return nil, fmt.Errorf("classregistry: load %s: %w", dir, err)
	}
	return reg, nil
}

// emptyRegistry returns a Registry that serves no domains. Used when
// CLASS_REGISTRY_DIR points at a missing path — early Phase F state.
func emptyRegistry() Registry {
	return &memRegistry{byDomain: map[string]*domainIndex{}}
}

// TenantAwareModule wires a TenantAware Registry on top of the base
// Registry and an OverrideStore. Consumers that need per-tenant
// narrowing inject TenantAware and call WithTenant(ctx, tenantID);
// consumers that don't care continue to inject Registry directly.
//
// If the OverrideStore is unavailable at startup (F.6.2 not yet rolled
// out for this deployment), callers can omit this module and rely on
// the plain Module — every WithTenant call then returns the base
// registry verbatim via NewTenantAware's nil-store path.
var TenantAwareModule = fx.Module("classregistry-tenant-aware",
	fx.Provide(func(base Registry, store OverrideStore) TenantAware {
		return NewTenantAware(base, store)
	}),
)

// HookRegistryModule exposes a singleton HookRegistry (F.6.L3 / Layer 3
// of the four-layer plan). Composition roots register hooks via
// fx.Invoke handlers that depend on the HookRegistry:
//
//	fx.Invoke(func(hooks classregistry.HookRegistry) {
//	    hooks.RegisterPreWrite("lotserial", "pharma_batch",
//	        pharmaPHIEnforcementHook)
//	})
//
// The registry is process-wide — every pgstore EntityStore write
// consults it. Registrations are thread-safe and can be added at
// startup or dynamically.
var HookRegistryModule = fx.Module("classregistry-hooks",
	fx.Provide(NewHookRegistry),
)
