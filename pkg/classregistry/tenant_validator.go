package classregistry

import (
	"context"
	"fmt"
)

// TenantValidator bundles a Registry + LookupResolver + tenant context
// into a single object whose ValidateAttributes method runs both the
// base shape validation AND the reference-existence validation against
// the class_entities plane. Callers that want automatic
// `reference: lookup: X` checks use this wrapper; callers that want
// just shape validation use Registry.ValidateAttributes directly.
//
// This is the "wired Layer 2" path of the four-layer plan (see
// docs/VERTICAL_GAP_ANALYSIS.md). Layer 2 MVP shipped the resolver
// standalone; this wrapper is the ergonomic wiring.
//
// Construction is cheap — callers typically build a TenantValidator
// per request (the resolver + registry are singletons; only the
// tenantID and ctx are per-request).
type TenantValidator struct {
	registry Registry
	resolver *LookupResolver
	tenantID string
	ctx      context.Context
}

// NewTenantValidator wires a per-request validator. Registry is
// required; resolver may be nil (then reference attributes fall back
// to the Registry's default reference handling — non-empty check only).
// The context is used for the resolver's EntityStore reads.
func NewTenantValidator(
	ctx context.Context,
	reg Registry,
	resolver *LookupResolver,
	tenantID string,
) *TenantValidator {
	return &TenantValidator{
		registry: reg,
		resolver: resolver,
		tenantID: tenantID,
		ctx:      ctx,
	}
}

// ValidateAttributes runs the Registry's shape validation, then (if
// the resolver is wired and the tenant ID is non-empty) resolves
// every `reference: lookup:` attribute against the class_entities
// plane. Either step failing surfaces a typed error; shape failures
// win (we don't run the resolver on a spec-invalid payload).
func (v *TenantValidator) ValidateAttributes(
	domain, class string,
	attrs map[string]AttributeValue,
) error {
	if err := v.registry.ValidateAttributes(domain, class, attrs); err != nil {
		return err
	}
	if v.resolver == nil || v.tenantID == "" {
		// No tenant-scoped resolver wired — degrade to base behavior.
		// The base behavior already rejected required-empty references
		// in ValidateAttributes; non-required empty references are
		// accepted without existence check (the Layer 2 MVP default).
		return nil
	}

	cd, err := v.registry.GetClass(domain, class)
	if err != nil {
		// Shouldn't happen — ValidateAttributes above would have
		// failed. Defensive.
		return err
	}

	for attrName, spec := range cd.Attributes {
		if spec.Kind != KindReference {
			continue
		}
		if spec.Lookup == "" {
			// No lookup target declared — the resolver cannot check.
			// Base validation already verified required-empty rules.
			continue
		}
		val, present := attrs[attrName]
		if !present || val.String == "" {
			// Empty reference on a non-required attribute is accepted.
			// Required-empty already failed in ValidateAttributes.
			continue
		}
		if err := v.resolver.ValidateReference(
			v.ctx,
			v.tenantID,
			domain, class,
			attrName,
			spec.Lookup,
			val.String,
		); err != nil {
			return fmt.Errorf(
				"class %q attribute %q lookup %q failed: %w",
				class, attrName, spec.Lookup, err,
			)
		}
	}
	return nil
}

// ValidateAttributesFromStrings is the string-map convenience variant
// matching Registry.ValidateAttributesFromStrings + resolver.
func (v *TenantValidator) ValidateAttributesFromStrings(
	domain, class string,
	attrs map[string]string,
) (map[string]AttributeValue, error) {
	typed, err := v.registry.ValidateAttributesFromStrings(domain, class, attrs)
	if err != nil {
		return nil, err
	}
	if v.resolver == nil || v.tenantID == "" {
		return typed, nil
	}

	cd, err := v.registry.GetClass(domain, class)
	if err != nil {
		return nil, err
	}
	for attrName, spec := range cd.Attributes {
		if spec.Kind != KindReference || spec.Lookup == "" {
			continue
		}
		val, ok := typed[attrName]
		if !ok || val.String == "" {
			continue
		}
		if err := v.resolver.ValidateReference(
			v.ctx,
			v.tenantID,
			domain, class,
			attrName,
			spec.Lookup,
			val.String,
		); err != nil {
			return nil, fmt.Errorf(
				"class %q attribute %q lookup %q failed: %w",
				class, attrName, spec.Lookup, err,
			)
		}
	}
	return typed, nil
}
