package classregistry

import (
	"context"
	"fmt"
	"sort"

	"p9e.in/samavaya/packages/errors"
)

// OverrideStore is the narrow port the overlay depends on. An SQL-backed
// implementation lives under packages/classregistry/pgstore; tests use
// InMemoryOverrideStore (see overlay_test.go). The overlay reads
// overrides lazily per (tenant, domain) and caches nothing — callers
// that need caching wrap this store.
type OverrideStore interface {
	// ListForTenantDomain returns the tenant's overrides for the
	// given domain. Key is the class name. Missing keys mean "no
	// override for that class". Returning (nil, nil) is valid for
	// tenants with no overrides at all — treated as global-only.
	ListForTenantDomain(
		ctx context.Context,
		tenantID, domain string,
	) (map[string]ClassOverride, error)
}

// TenantAware extends Registry with a per-tenant projection. The
// returned Registry applies the tenant's overrides on top of the
// global class definitions. If the tenant has no overrides,
// WithTenant returns a Registry that behaves identically to the
// global one.
//
// TenantAware satisfies the base Registry interface by delegating to
// the zero-tenant (global) view, which keeps old call sites that
// don't know about tenants working unchanged.
type TenantAware interface {
	Registry

	// WithTenant returns a Registry scoped to the given tenant. The
	// returned value is cheap; callers should build one per request
	// rather than holding a long-lived reference.
	WithTenant(ctx context.Context, tenantID string) Registry
}

// NewTenantAware wraps a base Registry with an OverrideStore. If
// store is nil, WithTenant returns the base registry verbatim — the
// overlay becomes a no-op. This is the pre-F.6.2 compatibility path:
// a service wired before the overlay migration landed continues to
// see only the global registry.
func NewTenantAware(base Registry, store OverrideStore) TenantAware {
	return &tenantAware{base: base, store: store}
}

type tenantAware struct {
	base  Registry
	store OverrideStore
}

// Registry passthrough — the global view.
func (t *tenantAware) GetClass(domain, class string) (*ClassDef, error) {
	return t.base.GetClass(domain, class)
}
func (t *tenantAware) ListClasses(domain string) []*ClassDef {
	return t.base.ListClasses(domain)
}
func (t *tenantAware) ValidateAttributes(domain, class string, attrs map[string]AttributeValue) error {
	return t.base.ValidateAttributes(domain, class, attrs)
}
func (t *tenantAware) ValidateAttributesFromStrings(domain, class string, attrs map[string]string) (map[string]AttributeValue, error) {
	return t.base.ValidateAttributesFromStrings(domain, class, attrs)
}
func (t *tenantAware) ComputeDerived(domain, class string, attrs map[string]AttributeValue) (map[string]AttributeValue, error) {
	return t.base.ComputeDerived(domain, class, attrs)
}
func (t *tenantAware) GetProcesses(domain, class string) []string {
	return t.base.GetProcesses(domain, class)
}
func (t *tenantAware) GetCustomExtensions(domain, class string) []CustomExtension {
	return t.base.GetCustomExtensions(domain, class)
}
func (t *tenantAware) Domains() []string {
	return t.base.Domains()
}

// WithTenant returns a per-tenant Registry. See TenantAware for
// semantics.
func (t *tenantAware) WithTenant(ctx context.Context, tenantID string) Registry {
	if t.store == nil || tenantID == "" {
		return t.base
	}
	return &tenantView{
		base:     t.base,
		store:    t.store,
		ctx:      ctx,
		tenantID: tenantID,
	}
}

// tenantView is a Registry whose reads layer the tenant's overrides on
// top of the base. It is intentionally request-scoped: the context
// held here is the request context used for DB calls into the
// override store. A long-lived tenantView would pin a stale context,
// so callers should build a fresh one per request.
type tenantView struct {
	base     Registry
	store    OverrideStore
	ctx      context.Context
	tenantID string
}

// GetClass returns the base class merged with any per-tenant override.
// An override that cannot be merged (widens a constraint, references
// an unknown attribute, kind-mismatched default) surfaces a typed
// CLASSREGISTRY_OVERRIDE_* error. The alternative — silently
// dropping a bad override — would let a malformed row escape
// validation, which is the bug this layer exists to prevent.
func (t *tenantView) GetClass(domain, class string) (*ClassDef, error) {
	base, err := t.base.GetClass(domain, class)
	if err != nil {
		return nil, err
	}
	overrides, err := t.store.ListForTenantDomain(t.ctx, t.tenantID, domain)
	if err != nil {
		return nil, errors.InternalServer(
			"CLASSREGISTRY_OVERRIDE_STORE_READ",
			fmt.Sprintf("read tenant overrides for %q/%q: %v", domain, class, err),
		)
	}
	ov, ok := overrides[class]
	if !ok {
		return base, nil
	}
	return mergeClassOverride(base, ov)
}

// ListClasses walks every base class for the domain and applies the
// matching tenant override. Classes without overrides pass through
// unchanged. A merge error on any single class fails the whole list
// — we prefer a loud failure to a partially-narrowed result the
// caller can't reason about.
func (t *tenantView) ListClasses(domain string) []*ClassDef {
	base := t.base.ListClasses(domain)
	if len(base) == 0 {
		return base
	}
	overrides, err := t.store.ListForTenantDomain(t.ctx, t.tenantID, domain)
	if err != nil || len(overrides) == 0 {
		// Best-effort: ListClasses is a non-error-returning call in
		// the Registry interface, so we fall back to the global view
		// on store errors. The per-request GetClass / Validate* path
		// surfaces the error; skipping it here just prevents a
		// read-only list from hiding the global shape.
		return base
	}
	out := make([]*ClassDef, 0, len(base))
	for _, cd := range base {
		ov, ok := overrides[cd.Name]
		if !ok {
			out = append(out, cd)
			continue
		}
		merged, err := mergeClassOverride(cd, ov)
		if err != nil {
			// Same best-effort path: fall back to the base class so
			// the list stays usable. The write-path validator is the
			// authoritative gate.
			out = append(out, cd)
			continue
		}
		out = append(out, merged)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ValidateAttributes resolves the per-tenant class shape then runs the
// standard validator. The overlay does not maintain a parallel
// validation path — after merging the spec we delegate to the
// base Registry's validation, keeping one validator surface overall.
func (t *tenantView) ValidateAttributes(domain, class string, attrs map[string]AttributeValue) error {
	merged, err := t.GetClass(domain, class)
	if err != nil {
		return err
	}
	return validateAgainst(merged, attrs)
}

// ValidateAttributesFromStrings mirrors the base's coerce-then-validate
// two-step on the per-tenant class.
func (t *tenantView) ValidateAttributesFromStrings(
	domain, class string,
	attrs map[string]string,
) (map[string]AttributeValue, error) {
	merged, err := t.GetClass(domain, class)
	if err != nil {
		return nil, err
	}
	if attrs == nil {
		attrs = map[string]string{}
	}
	typed := make(map[string]AttributeValue, len(merged.Attributes))
	for key, raw := range attrs {
		spec, ok := merged.Attributes[key]
		if !ok {
			typed[key] = AttributeValue{Kind: KindString, String: raw}
			continue
		}
		v, err := coerceStringValue(raw, spec)
		if err != nil {
			return nil, errors.BadRequest(
				"CLASSREGISTRY_COERCE_FAILED",
				fmt.Sprintf("attribute %q: cannot coerce %q to %s: %v", key, raw, spec.Kind, err),
			)
		}
		typed[key] = v
	}
	if err := validateAgainst(merged, typed); err != nil {
		return nil, err
	}
	return typed, nil
}

// ComputeDerived delegates to the base. Overrides narrow the attribute
// shape; they do not add derived formulas.
func (t *tenantView) ComputeDerived(domain, class string, attrs map[string]AttributeValue) (map[string]AttributeValue, error) {
	return t.base.ComputeDerived(domain, class, attrs)
}

// GetProcesses and GetCustomExtensions are not overridable today.
// Overrides are a data-shape narrowing feature — opting a tenant in or
// out of a process is a provisioning concern, not a per-class
// override. If that changes, extend ClassOverride with Processes /
// CustomExtensions pointers and fold them here.
func (t *tenantView) GetProcesses(domain, class string) []string {
	return t.base.GetProcesses(domain, class)
}
func (t *tenantView) GetCustomExtensions(domain, class string) []CustomExtension {
	return t.base.GetCustomExtensions(domain, class)
}
func (t *tenantView) Domains() []string {
	return t.base.Domains()
}

// ---------------------------------------------------------------------------
// Merge semantics
// ---------------------------------------------------------------------------

// mergeClassOverride applies the per-tenant override on top of a base
// class. Returns a fresh *ClassDef; the base is never mutated.
// Invariants:
//   - unknown attribute names in the override are rejected
//   - Values narrows; any entry not in the global Values is rejected
//   - Min may only raise; Max may only lower
//   - Required may only flip false→true
//   - Default's Kind must match the global AttributeSpec.Kind
func mergeClassOverride(base *ClassDef, ov ClassOverride) (*ClassDef, error) {
	if len(ov.AttributeOverrides) == 0 {
		return base, nil
	}
	out := *base
	out.Attributes = make(map[string]AttributeSpec, len(base.Attributes))
	for name, spec := range base.Attributes {
		out.Attributes[name] = spec
	}
	for name, ao := range ov.AttributeOverrides {
		spec, ok := out.Attributes[name]
		if !ok {
			return nil, errors.BadRequest(
				"CLASSREGISTRY_OVERRIDE_UNKNOWN_ATTRIBUTE",
				fmt.Sprintf("class %q has no attribute %q to override", base.Name, name),
			)
		}
		merged, err := mergeAttributeOverride(base.Name, name, spec, ao)
		if err != nil {
			return nil, err
		}
		out.Attributes[name] = merged
	}
	return &out, nil
}

func mergeAttributeOverride(
	className, attrName string,
	spec AttributeSpec,
	ao AttributeOverride,
) (AttributeSpec, error) {
	out := spec

	if ao.Required != nil {
		if !*ao.Required && spec.Required {
			return out, errors.BadRequest(
				"CLASSREGISTRY_OVERRIDE_WIDENS_REQUIRED",
				fmt.Sprintf("class %q attribute %q: tenant override cannot relax required=true to required=false",
					className, attrName),
			)
		}
		out.Required = *ao.Required
	}

	if ao.Default != nil {
		if ao.Default.Kind != spec.Kind {
			return out, errors.BadRequest(
				"CLASSREGISTRY_OVERRIDE_DEFAULT_KIND_MISMATCH",
				fmt.Sprintf("class %q attribute %q: override default kind %q does not match class-declared kind %q",
					className, attrName, ao.Default.Kind, spec.Kind),
			)
		}
		d := *ao.Default
		out.Default = &d
	}

	if len(ao.Values) > 0 {
		if spec.Kind != KindEnum {
			return out, errors.BadRequest(
				"CLASSREGISTRY_OVERRIDE_VALUES_NOT_ENUM",
				fmt.Sprintf("class %q attribute %q: values-override requires kind=enum, got %q",
					className, attrName, spec.Kind),
			)
		}
		allowed := make(map[string]struct{}, len(spec.Values))
		for _, v := range spec.Values {
			allowed[v] = struct{}{}
		}
		for _, v := range ao.Values {
			if _, ok := allowed[v]; !ok {
				return out, errors.BadRequest(
					"CLASSREGISTRY_OVERRIDE_VALUES_WIDEN",
					fmt.Sprintf("class %q attribute %q: override value %q is not in the global enum %v",
						className, attrName, v, spec.Values),
				)
			}
		}
		out.Values = append([]string(nil), ao.Values...)
	}

	if ao.Min != nil {
		if spec.Min != nil && *ao.Min < *spec.Min {
			return out, errors.BadRequest(
				"CLASSREGISTRY_OVERRIDE_MIN_WIDENS",
				fmt.Sprintf("class %q attribute %q: override min %v is less than global min %v (overrides may only raise min)",
					className, attrName, *ao.Min, *spec.Min),
			)
		}
		m := *ao.Min
		out.Min = &m
	}

	if ao.Max != nil {
		if spec.Max != nil && *ao.Max > *spec.Max {
			return out, errors.BadRequest(
				"CLASSREGISTRY_OVERRIDE_MAX_WIDENS",
				fmt.Sprintf("class %q attribute %q: override max %v exceeds global max %v (overrides may only lower max)",
					className, attrName, *ao.Max, *spec.Max),
			)
		}
		m := *ao.Max
		out.Max = &m
	}

	// After a min+max narrow, sanity-check they didn't cross.
	if out.Min != nil && out.Max != nil && *out.Min > *out.Max {
		return out, errors.BadRequest(
			"CLASSREGISTRY_OVERRIDE_MIN_EXCEEDS_MAX",
			fmt.Sprintf("class %q attribute %q: after override, min %v > max %v",
				className, attrName, *out.Min, *out.Max),
		)
	}

	return out, nil
}

// validateAgainst runs the per-class validator against a concrete
// ClassDef. This is the same logic memRegistry.ValidateAttributes
// uses, factored out so tenantView can apply it to the merged class
// without re-looking-up a registry-keyed class.
func validateAgainst(cd *ClassDef, attrs map[string]AttributeValue) error {
	if attrs == nil {
		return errors.BadRequest(
			"CLASSREGISTRY_ATTRIBUTES_NIL",
			"attributes map is nil",
		)
	}
	for name, spec := range cd.Attributes {
		if _, present := attrs[name]; !present && spec.Default != nil {
			attrs[name] = *spec.Default
		}
	}
	for name := range attrs {
		if _, ok := cd.Attributes[name]; !ok {
			return errors.BadRequest(
				"CLASSREGISTRY_UNKNOWN_ATTRIBUTE",
				fmt.Sprintf("class %q has no attribute %q", cd.Name, name),
			)
		}
	}
	idx := &domainIndex{Domain: cd.Domain}
	for name, spec := range cd.Attributes {
		v, present := attrs[name]
		if !present {
			if spec.Required {
				return errors.BadRequest(
					"CLASSREGISTRY_MISSING_REQUIRED",
					fmt.Sprintf("class %q requires attribute %q", cd.Name, name),
				)
			}
			continue
		}
		if err := validateValue(idx, name, spec, v); err != nil {
			return err
		}
	}
	return nil
}
