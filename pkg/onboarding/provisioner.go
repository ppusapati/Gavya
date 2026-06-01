package onboarding

import (
	"context"
	"fmt"

	"p9e.in/samavaya/packages/classregistry"
	"p9e.in/samavaya/packages/classregistry/pgstore"
	"p9e.in/samavaya/packages/errors"
)

// OverrideWriter is the narrow write-side port the provisioner needs
// from classregistry/pgstore. Extracted as an interface so tests can
// substitute an in-memory fake without wiring pgxpool. The production
// implementation is *pgstore.Store.
type OverrideWriter interface {
	UpsertOverride(ctx context.Context, in pgstore.UpsertInput) (string, error)
}

// SeedMasterWriter is the narrow port packages/onboarding uses to
// insert master-data rows. The composition root binds a fan-out
// implementation that dispatches by domain to the right business
// service's client port (e.g. `agriculture` seed masters go to
// `business/inventory/core/client`, `pharmaceutical` DEA schedule
// rows go to `business/compliance/regulatory_traceability/client`).
//
// Keeping this as an interface in packages/onboarding — rather than
// importing every domain's client here — inverts the dependency: the
// composition root owns the fan-out, not onboarding.
type SeedMasterWriter interface {
	// InsertSeedRow inserts one master-data row for the given tenant.
	// Idempotency is the writer's responsibility: if the row already
	// exists (by the domain's natural key), the writer should no-op
	// and return nil. The provisioner records the step as completed
	// either way.
	InsertSeedRow(
		ctx context.Context,
		tenantID, domain, table string,
		row map[string]string,
	) error
}

// ProcessEnabler is the narrow port for writing per-tenant process
// enablement. Like SeedMasterWriter, the composition root owns the
// dispatch to whatever per-domain or per-tenant process-registry
// landing happens to exist (this is separate from the per-class
// `processes:` list in classregistry YAML — it's the profile-level
// cross-cutting enablement).
//
// A deployment that hasn't built process-enablement infrastructure
// yet can bind a no-op implementation; the provisioner still records
// the step in its progress table so future runs know not to retry.
type ProcessEnabler interface {
	EnableProcess(ctx context.Context, tenantID, processName string) error
}

// EventEmitter is the narrow port for publishing the
// tenant.provisioned event. The composition root binds it to
// packages/events/bus in monolith mode or to the Kafka producer in
// split mode. A nil emitter is accepted — the provisioner skips the
// emit step and records a 'completed' step so future runs don't loop.
type EventEmitter interface {
	TenantProvisioned(ctx context.Context, tenantID, industryCode, profileVersion string) error
}

// ProvisioningStateStore tracks per-(tenant, industry) provisioning
// progress. The SQL-backed impl lives in pgstate.go.
type ProvisioningStateStore interface {
	// LoadOrStart returns the existing state row if one exists for
	// (tenantID, industryCode), or creates a fresh one in 'running'
	// state. Returns the provisioning ID either way.
	LoadOrStart(
		ctx context.Context,
		tenantID, industryCode, profileVersion, actorID string,
	) (provisioningID, existingState string, err error)

	// CompleteStep records a per-step completion. Called after every
	// individual provisioning step lands. Idempotent — re-recording
	// an already-completed step is a no-op.
	CompleteStep(
		ctx context.Context,
		provisioningID, stepKind, stepKey string,
	) error

	// FailStep records a per-step failure and its error. Completing
	// or failing the same (provisioningID, stepKind, stepKey) twice
	// is a no-op.
	FailStep(
		ctx context.Context,
		provisioningID, stepKind, stepKey, errorMsg string,
	) error

	// IsStepCompleted returns true if the step has already completed
	// successfully — lets the provisioner skip already-done work on
	// a re-run.
	IsStepCompleted(
		ctx context.Context,
		provisioningID, stepKind, stepKey string,
	) (bool, error)

	// MarkCompleted flips the overall provisioning row to 'completed'
	// after every step succeeded.
	MarkCompleted(ctx context.Context, tenantID, provisioningID, actorID string) error

	// MarkFailed flips the overall provisioning row to 'failed' with
	// the given error message.
	MarkFailed(ctx context.Context, tenantID, provisioningID, actorID, errorMsg string) error
}

// Provisioner orchestrates a tenant-onboarding run against an
// industry profile. See package doc for the step-by-step contract.
type Provisioner struct {
	profiles  map[string]*Profile
	registry  classregistry.Registry
	overrides OverrideWriter
	seeds     SeedMasterWriter
	processes ProcessEnabler
	events    EventEmitter
	state     ProvisioningStateStore
}

// ProvisionerConfig bundles Provisioner dependencies. Seeds, processes,
// and events are all optional — a nil dependency simply skips the
// corresponding step class. Registry and overrides + state are required.
type ProvisionerConfig struct {
	Profiles  map[string]*Profile
	Registry  classregistry.Registry
	Overrides OverrideWriter
	State     ProvisioningStateStore

	Seeds     SeedMasterWriter // optional
	Processes ProcessEnabler   // optional
	Events    EventEmitter     // optional
}

// NewProvisioner wires a Provisioner. Returns an error if the required
// dependencies are missing so misconfiguration fails at startup, not
// at first tenant onboarding.
func NewProvisioner(cfg ProvisionerConfig) (*Provisioner, error) {
	if cfg.Profiles == nil {
		return nil, errors.BadRequest(
			"ONBOARDING_NO_PROFILES",
			"ProvisionerConfig.Profiles is required",
		)
	}
	if cfg.Registry == nil {
		return nil, errors.BadRequest(
			"ONBOARDING_NO_REGISTRY",
			"ProvisionerConfig.Registry is required",
		)
	}
	if cfg.Overrides == nil {
		return nil, errors.BadRequest(
			"ONBOARDING_NO_OVERRIDE_STORE",
			"ProvisionerConfig.Overrides is required",
		)
	}
	if cfg.State == nil {
		return nil, errors.BadRequest(
			"ONBOARDING_NO_STATE_STORE",
			"ProvisionerConfig.State is required",
		)
	}
	return &Provisioner{
		profiles:  cfg.Profiles,
		registry:  cfg.Registry,
		overrides: cfg.Overrides,
		state:     cfg.State,
		seeds:     cfg.Seeds,
		processes: cfg.Processes,
		events:    cfg.Events,
	}, nil
}

// ProvisionInput is the per-call payload for Provision.
type ProvisionInput struct {
	TenantID     string
	IndustryCode string
	ActorID      string // admin user initiating onboarding
}

// Provision applies an industry profile to a tenant. Idempotent: a
// second call against the same (tenant, industry) is a no-op if the
// first completed; a call that resumes a failed run picks up from
// the last successful step.
//
// On a fresh run every step lands; on a resumed run any step already
// marked 'completed' is skipped. Failures record a step-level error
// and flip the parent row to 'failed'. The caller (typically a tenant
// service admin RPC handler) surfaces the error; retrying is just
// calling Provision again.
func (p *Provisioner) Provision(ctx context.Context, in ProvisionInput) error {
	if in.TenantID == "" {
		return errors.BadRequest("ONBOARDING_MISSING_TENANT_ID", "tenant_id is required")
	}
	if in.IndustryCode == "" {
		return errors.BadRequest("ONBOARDING_MISSING_INDUSTRY", "industry_code is required")
	}
	if in.ActorID == "" {
		return errors.BadRequest("ONBOARDING_MISSING_ACTOR", "actor_id is required for audit")
	}

	profile, ok := p.profiles[in.IndustryCode]
	if !ok {
		return errors.NotFound(
			"ONBOARDING_UNKNOWN_INDUSTRY",
			fmt.Sprintf("no profile loaded for industry %q", in.IndustryCode),
		)
	}

	provID, prior, err := p.state.LoadOrStart(ctx, in.TenantID, in.IndustryCode, profile.Version, in.ActorID)
	if err != nil {
		return fmt.Errorf("load provisioning state: %w", err)
	}
	if prior == "completed" {
		// Idempotency: a completed row means this industry is fully
		// provisioned. No further work.
		return nil
	}

	// Step 1: enable classes + write overrides.
	for _, ec := range profile.EnabledClasses {
		stepKey := ec.Domain + "/" + ec.Class
		if done, err := p.state.IsStepCompleted(ctx, provID, "enable_class", stepKey); err != nil {
			return p.fail(ctx, in.TenantID, provID, in.ActorID, "enable_class", stepKey, err)
		} else if !done {
			if _, err := p.registry.GetClass(ec.Domain, ec.Class); err != nil {
				return p.fail(ctx, in.TenantID, provID, in.ActorID, "enable_class", stepKey, err)
			}
			if err := p.state.CompleteStep(ctx, provID, "enable_class", stepKey); err != nil {
				return p.fail(ctx, in.TenantID, provID, in.ActorID, "enable_class", stepKey, err)
			}
		}

		if ec.Overrides != nil && len(ec.Overrides.AttributeOverrides) > 0 {
			if done, err := p.state.IsStepCompleted(ctx, provID, "write_override", stepKey); err != nil {
				return p.fail(ctx, in.TenantID, provID, in.ActorID, "write_override", stepKey, err)
			} else if !done {
				if _, err := p.overrides.UpsertOverride(ctx, pgstore.UpsertInput{
					TenantID: in.TenantID,
					Domain:   ec.Domain,
					Class:    ec.Class,
					Override: *ec.Overrides,
					ActorID:  in.ActorID,
					Reason:   fmt.Sprintf("onboarding: industry=%s profile_version=%s", profile.IndustryCode, profile.Version),
				}); err != nil {
					return p.fail(ctx, in.TenantID, provID, in.ActorID, "write_override", stepKey, err)
				}
				if err := p.state.CompleteStep(ctx, provID, "write_override", stepKey); err != nil {
					return p.fail(ctx, in.TenantID, provID, in.ActorID, "write_override", stepKey, err)
				}
			}
		}
	}

	// Step 2: insert seed masters.
	if len(profile.SeedMasters) > 0 {
		if p.seeds == nil {
			// Profile declares seed masters but no writer is wired.
			// Fail loudly rather than silently skipping — the tenant
			// would otherwise end up with an incomplete starter data
			// set and no signal.
			err := errors.InternalServer(
				"ONBOARDING_NO_SEED_WRITER",
				fmt.Sprintf("profile %q declares seed_masters but no SeedMasterWriter is wired", in.IndustryCode),
			)
			return p.fail(ctx, in.TenantID, provID, in.ActorID, "seed_master", "_config_", err)
		}
		for i, sm := range profile.SeedMasters {
			for ri, row := range sm.Rows {
				stepKey := fmt.Sprintf("%s/%s/%d/%d", sm.Domain, sm.Table, i, ri)
				if done, err := p.state.IsStepCompleted(ctx, provID, "seed_master", stepKey); err != nil {
					return p.fail(ctx, in.TenantID, provID, in.ActorID, "seed_master", stepKey, err)
				} else if done {
					continue
				}
				if err := p.seeds.InsertSeedRow(ctx, in.TenantID, sm.Domain, sm.Table, row); err != nil {
					return p.fail(ctx, in.TenantID, provID, in.ActorID, "seed_master", stepKey, err)
				}
				if err := p.state.CompleteStep(ctx, provID, "seed_master", stepKey); err != nil {
					return p.fail(ctx, in.TenantID, provID, in.ActorID, "seed_master", stepKey, err)
				}
			}
		}
	}

	// Step 3: enable processes.
	for _, procName := range profile.EnabledProcesses {
		if done, err := p.state.IsStepCompleted(ctx, provID, "enable_process", procName); err != nil {
			return p.fail(ctx, in.TenantID, provID, in.ActorID, "enable_process", procName, err)
		} else if done {
			continue
		}
		if p.processes == nil {
			// Process enablement infra not yet built — record as completed
			// so re-runs don't loop, but log nothing. This is the
			// pre-infrastructure path: profiles can declare processes
			// before the ProcessEnabler backing store exists, and
			// provisioning still completes.
			if err := p.state.CompleteStep(ctx, provID, "enable_process", procName); err != nil {
				return p.fail(ctx, in.TenantID, provID, in.ActorID, "enable_process", procName, err)
			}
			continue
		}
		if err := p.processes.EnableProcess(ctx, in.TenantID, procName); err != nil {
			return p.fail(ctx, in.TenantID, provID, in.ActorID, "enable_process", procName, err)
		}
		if err := p.state.CompleteStep(ctx, provID, "enable_process", procName); err != nil {
			return p.fail(ctx, in.TenantID, provID, in.ActorID, "enable_process", procName, err)
		}
	}

	// Step 4: emit tenant.provisioned event.
	const emitKey = "tenant.provisioned"
	if done, err := p.state.IsStepCompleted(ctx, provID, "emit_event", emitKey); err != nil {
		return p.fail(ctx, in.TenantID, provID, in.ActorID, "emit_event", emitKey, err)
	} else if !done {
		if p.events != nil {
			if err := p.events.TenantProvisioned(ctx, in.TenantID, profile.IndustryCode, profile.Version); err != nil {
				return p.fail(ctx, in.TenantID, provID, in.ActorID, "emit_event", emitKey, err)
			}
		}
		if err := p.state.CompleteStep(ctx, provID, "emit_event", emitKey); err != nil {
			return p.fail(ctx, in.TenantID, provID, in.ActorID, "emit_event", emitKey, err)
		}
	}

	return p.state.MarkCompleted(ctx, in.TenantID, provID, in.ActorID)
}

// fail records a step failure, flips the parent row to 'failed', and
// returns the original error so the caller surfaces it. Deliberately
// two writes (step + parent) — we want both the granular step error
// and the quick "is this provisioning done?" lookup to agree.
func (p *Provisioner) fail(
	ctx context.Context,
	tenantID, provID, actorID, stepKind, stepKey string,
	cause error,
) error {
	msg := cause.Error()
	if fErr := p.state.FailStep(ctx, provID, stepKind, stepKey, msg); fErr != nil {
		// Best-effort: don't mask the original error if the audit
		// write itself fails — log the audit failure by wrapping.
		return fmt.Errorf("provisioning step failed (%s/%s): %w; audit write also failed: %v",
			stepKind, stepKey, cause, fErr)
	}
	if mErr := p.state.MarkFailed(ctx, tenantID, provID, actorID, msg); mErr != nil {
		return fmt.Errorf("provisioning step failed (%s/%s): %w; parent mark-failed also failed: %v",
			stepKind, stepKey, cause, mErr)
	}
	return cause
}

// Profiles exposes the loaded profile map (read-only). Useful for
// admin listing of what profiles are available.
func (p *Provisioner) Profiles() map[string]*Profile {
	// Return a copy to prevent accidental mutation.
	out := make(map[string]*Profile, len(p.profiles))
	for k, v := range p.profiles {
		out[k] = v
	}
	return out
}
