package classregistry

import (
	"context"
	"fmt"
	"strings"

	"p9e.in/samavaya/packages/errors"
)

// LookupResolver answers foreign-key-style existence checks against
// the Layer 2 class-entity plane. When an attribute declares
// `reference: lookup: <target>` the resolver interprets the value
// as the target's natural_key and verifies that (a) the target class
// exists in the registry, and (b) a live class_entity row with that
// natural_key exists for the tenant.
//
// The resolver is the glue that makes `lookup:` declarations meaningful
// when the target class lives in the generic entity plane (vs a
// dedicated business-service table). Today's registry has 11+ attributes
// pointing at `ppa_contract` with no backing table; once ppa_contract
// is declared as a class and rows are seeded via Provisioner, those
// references become checkable via this resolver.
//
// The resolver does NOT reject unknown lookup targets. If an attribute
// declares `lookup: some_legacy_table` and that table isn't a registered
// class, the resolver returns errLookupTargetNotClass — the caller
// decides whether to fall back to a different check or accept the value.
// This keeps the resolver additive over existing lookup semantics.
type LookupResolver struct {
	registry Registry
	entities EntityStore
}

// NewLookupResolver wires a resolver from the global registry and the
// entity store. Both are required; constructing with a nil store
// degrades Validate into a no-op (useful for pre-Layer-2 compatibility
// paths but surfaces an error on first call so callers notice).
func NewLookupResolver(reg Registry, store EntityStore) *LookupResolver {
	return &LookupResolver{registry: reg, entities: store}
}

// ValidateReference verifies that the attribute value points at a
// live class_entity row of the declared lookup target.
//
// - targetDomain/targetClass: the class that hosts the `reference:
//   lookup: <targetClass>` attribute being validated (for error
//   messages).
// - attrName: the attribute's name in that class.
// - lookupTarget: the value of the spec's Lookup field (from
//   AttributeSpec.Lookup). Conventionally a class name.
// - value: the attribute value being validated (natural_key of the
//   referenced entity).
//
// Returns:
//   - nil if the value is empty AND the spec permits empty (handled
//     by the caller before calling ValidateReference, so we just fail
//     on empty here)
//   - errors.NotFound (typed CLASSREGISTRY_REFERENCE_NOT_FOUND) if the
//     row doesn't exist
//   - errors.InternalServer (CLASSREGISTRY_RESOLVER_NO_STORE) if the
//     store isn't wired
//   - errors.BadRequest (CLASSREGISTRY_REFERENCE_TARGET_UNKNOWN) if
//     the lookup target doesn't resolve to a registered class
func (r *LookupResolver) ValidateReference(
	ctx context.Context,
	tenantID, targetDomain, targetClass, attrName, lookupTarget, value string,
) error {
	if r.entities == nil {
		return errors.InternalServer(
			"CLASSREGISTRY_RESOLVER_NO_STORE",
			"lookup resolver constructed without an EntityStore",
		)
	}
	if strings.TrimSpace(value) == "" {
		return errors.BadRequest(
			"CLASSREGISTRY_REFERENCE_EMPTY",
			fmt.Sprintf("class %q attribute %q: reference value is empty", targetClass, attrName),
		)
	}
	if lookupTarget == "" {
		return errors.BadRequest(
			"CLASSREGISTRY_REFERENCE_NO_TARGET",
			fmt.Sprintf("class %q attribute %q: reference spec declares no lookup target", targetClass, attrName),
		)
	}

	// Resolve the target to a registered class. The convention is
	// that lookup uses the bare class name and defaults the domain
	// to the target's own registry. Class names are globally unique
	// per domain in the registry, but across domains they may repeat
	// (e.g. two domains both declare a `base_*` pattern). Callers who
	// need cross-domain disambiguation use "domain/class" in the
	// lookup target; the resolver accepts both forms.
	domain, class := splitLookupTarget(lookupTarget, targetDomain)
	if _, err := r.registry.GetClass(domain, class); err != nil {
		return errors.BadRequest(
			"CLASSREGISTRY_REFERENCE_TARGET_UNKNOWN",
			fmt.Sprintf(
				"class %q attribute %q: lookup target %q does not resolve to a registered class (tried %q/%q): %v",
				targetClass, attrName, lookupTarget, domain, class, err,
			),
		)
	}

	ok, err := r.entities.Exists(ctx, tenantID, domain, class, value)
	if err != nil {
		return errors.InternalServer(
			"CLASSREGISTRY_RESOLVER_STORE_READ",
			fmt.Sprintf(
				"class %q attribute %q lookup %q: entity store read failed: %v",
				targetClass, attrName, lookupTarget, err,
			),
		)
	}
	if !ok {
		return errors.NotFound(
			"CLASSREGISTRY_REFERENCE_NOT_FOUND",
			fmt.Sprintf(
				"class %q attribute %q: no live %q entity with natural_key %q for tenant %q",
				targetClass, attrName, lookupTarget, value, tenantID,
			),
		)
	}
	return nil
}

// splitLookupTarget parses the Lookup field. Accepts:
//   - "target_class"           → (defaultDomain, "target_class")
//   - "domain/target_class"    → ("domain", "target_class")
func splitLookupTarget(target, defaultDomain string) (domain, class string) {
	if i := strings.Index(target, "/"); i >= 0 {
		return target[:i], target[i+1:]
	}
	return defaultDomain, target
}
