// Package onboarding provisions a fresh (or existing) tenant against
// an industry profile. It reads `config/industry_profiles/<industry>.yaml`,
// resolves the profile's declared classes against the class registry,
// writes per-tenant overrides through packages/classregistry/pgstore,
// inserts seed masters via the SeedMasterWriter port, and emits a
// tenant.provisioned event.
//
// The Provisioner is idempotent: re-running onboarding against a tenant
// that already has a given industry provisioned is a no-op; adding a
// second industry to an existing tenant only applies the new industry's
// delta.
//
// Provisioning state lives in `onboarding.tenant_provisioning_state`
// (the coarse per-industry row) + `onboarding.tenant_provisioning_step`
// (per-step progress for resume-after-failure). See migration 000071.
//
// This package does NOT define how master data is inserted — each
// business domain exposes its own seed-master entry point through its
// client port, and the composition root wires a SeedMasterWriter that
// fans out to the right domain. packages/onboarding only knows the
// generic shape (domain, table, rows).
package onboarding

import "p9e.in/samavaya/packages/classregistry"

// Profile is the declarative bundle an industry ships. Loaded from
// `config/industry_profiles/<industry>.yaml` by Loader.Load.
//
// Every field except IndustryCode, Version, and Label is optional —
// an industry may opt into only the classes it needs (e.g. a pure-
// service consultancy profile might have zero seed masters).
type Profile struct {
	// IndustryCode is the stable identifier used by the tenant table's
	// industry column and by all downstream filtering. lowercase
	// snake_case; immutable across profile versions.
	IndustryCode string `yaml:"industry_code"`

	// Label is the human-readable display name shown in the onboarding
	// UI. May change across versions.
	Label string `yaml:"label"`

	// Description is shown in the onboarding UI to help an admin
	// understand what provisioning this profile will do.
	Description string `yaml:"description"`

	// Version is the profile's semver string. The provisioner records
	// this against the tenant_provisioning_state row so we can detect
	// when a tenant was provisioned against an older profile and surface
	// that for operational review.
	Version string `yaml:"version"`

	// EnabledClasses declares which classes this industry's tenants
	// get by default. Each entry must resolve to a real class in the
	// class registry; unknown classes fail load-time validation.
	EnabledClasses []EnabledClass `yaml:"enabled_classes"`

	// SeedMasters declares the master-data rows the provisioner
	// inserts during onboarding. Each entry names a domain + table
	// plus a list of rows. The SeedMasterWriter is responsible for
	// actually inserting them via the correct domain port.
	SeedMasters []SeedMaster `yaml:"seed_masters"`

	// EnabledProcesses declares which shared calculation-library
	// processes this industry's tenants opt into on top of what
	// individual classes already declared. The provisioner writes
	// these as per-tenant process-enablement rows via the
	// ProcessEnabler port.
	//
	// Keeping process enablement at the profile level (in addition to
	// per-class `processes:` in the registry YAML) lets an industry
	// turn on a cross-cutting calculation (e.g. fx_revaluation for
	// pharmaceutical's multi-currency intercompany flows) without
	// having to thread it through every relevant class definition.
	EnabledProcesses []string `yaml:"enabled_processes"`
}

// EnabledClass is one entry in Profile.EnabledClasses. At minimum it
// names the domain + class; optionally it carries a per-tenant
// classregistry override for that class (attribute defaults narrowed,
// enum values narrowed, min/max narrowed). The override payload is
// exactly the classregistry.ClassOverride shape so the provisioner can
// hand it to pgstore.Store.UpsertOverride without translation.
type EnabledClass struct {
	Domain string `yaml:"domain"`
	Class  string `yaml:"class"`

	// Overrides, when present, are applied as a per-tenant overlay
	// via the classregistry pgstore. The merge semantics (narrow-only,
	// can't widen the global class) are enforced by the overlay at
	// read time, so a profile that declares a widening override will
	// fail loudly when the provisioner tries to apply it.
	//
	// Absent / zero-value Overrides means "enable this class as-is" —
	// the tenant sees the global class shape without modification.
	Overrides *classregistry.ClassOverride `yaml:"overrides,omitempty"`
}

// SeedMaster is a batch of rows to insert into a domain's master-data
// table during onboarding. The Domain + Table pair identifies the
// target; Rows is a list of attribute maps that the SeedMasterWriter
// translates to the domain's specific insert call.
//
// The "rows are attribute maps" shape is deliberate: we do not bind
// profile YAML to a particular domain's Go struct. Each domain
// receives the attribute map through its SeedMasterWriter adapter and
// translates to its own insert contract. This keeps packages/onboarding
// free of domain-specific imports.
type SeedMaster struct {
	Domain string `yaml:"domain"`
	Table  string `yaml:"table"`

	// Description is human-readable; not consumed by Go code.
	Description string `yaml:"description,omitempty"`

	// Rows are attribute-map records. Each row's keys are the
	// domain-defined column names; values are scalar strings that
	// the SeedMasterWriter coerces into the target schema.
	Rows []map[string]string `yaml:"rows"`
}
