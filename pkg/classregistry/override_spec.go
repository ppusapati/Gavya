package classregistry

// ClassOverride is the tenant-scoped narrowing of a single class. All
// fields are optional; a zero-valued ClassOverride is a no-op. The
// overlay applies overrides at request time on top of the global
// ClassDef produced by the YAML loader.
//
// Invariants enforced by the overlay (see overlay.go):
//
//   - Unknown attribute names in AttributeOverrides are rejected.
//   - `Values` (enum narrowing) must be a subset of the global enum.
//   - `Min` may only raise the global min; `Max` may only lower the
//     global max.
//   - `Required` may only transition from false → true; false → false
//     and true → true are accepted as no-ops. true → false is rejected.
//   - `Default` is applied verbatim; its kind must match the global
//     AttributeSpec.Kind and its value must satisfy the post-narrowing
//     constraints (checked by ValidateAttributes after merge).
//
// Attempting to widen any of the above returns an error from
// GetClass/ValidateAttributes so tenants cannot escape the global
// contract through their overrides.
type ClassOverride struct {
	// AttributeOverrides narrow individual attribute specs. Key is
	// the attribute name declared by the global ClassDef.
	AttributeOverrides map[string]AttributeOverride
}

// AttributeOverride is the per-attribute narrowing payload. All
// pointer / slice fields are optional; nil means "defer to global".
type AttributeOverride struct {
	// Required flips required=false → required=true. The reverse
	// direction is rejected at merge time.
	Required *bool

	// Default overrides the global default for this attribute. The
	// value's Kind must match the global AttributeSpec.Kind.
	Default *AttributeValue

	// Values narrows an enum. Every entry must be present in the
	// global `Values` list; unknown entries are rejected.
	Values []string

	// Min raises the global min (numeric-kind attributes only). A
	// value lower than the global min is rejected.
	Min *float64

	// Max lowers the global max (numeric-kind attributes only). A
	// value higher than the global max is rejected.
	Max *float64
}
