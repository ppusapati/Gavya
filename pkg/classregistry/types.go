package classregistry

import (
	"time"
)

// AttributeKind enumerates the primitive types a class attribute may
// declare in YAML. The enumeration is closed — adding a kind requires
// updating the parser, validator, and expression evaluator together.
//
// Kinds map to AttributeValue fields one-to-one. See AttributeValue
// below. Wire representation (proto) mirrors this list; see
// packages/classregistry/proto/AttributeValue.
type AttributeKind string

const (
	KindString    AttributeKind = "string"
	KindInt       AttributeKind = "int"
	KindDecimal   AttributeKind = "decimal"
	KindBool      AttributeKind = "bool"
	KindDate      AttributeKind = "date"
	KindTimestamp AttributeKind = "timestamp"
	KindDuration  AttributeKind = "duration"
	KindMoney     AttributeKind = "money"
	KindGeoPoint  AttributeKind = "geo_point"

	KindStringArray  AttributeKind = "string_array"
	KindIntArray     AttributeKind = "int_array"
	KindDecimalArray AttributeKind = "decimal_array"

	KindEnum      AttributeKind = "enum"
	KindReference AttributeKind = "reference"

	KindObject AttributeKind = "object"
	KindFile   AttributeKind = "file"
)

// Valid reports whether k is a known kind.
func (k AttributeKind) Valid() bool {
	switch k {
	case KindString, KindInt, KindDecimal, KindBool,
		KindDate, KindTimestamp, KindDuration, KindMoney, KindGeoPoint,
		KindStringArray, KindIntArray, KindDecimalArray,
		KindEnum, KindReference, KindObject, KindFile:
		return true
	}
	return false
}

// AttributeSpec is the YAML-declared shape of one attribute on a
// class. The parser fills this from the YAML node and the validator
// enforces it on every write. Zero values mean "not declared" for the
// constraint fields (Min, Max, Pattern, Values, …).
//
// Semantics per field:
//
//   - Required: write fails if the attribute is absent.
//   - Default: applied when absent on write; validated like any value.
//   - Min/Max: numeric bounds (decimal-safe); ignored for non-numeric
//     kinds.
//   - Values: closed enumeration for KindEnum; exact match required.
//   - Lookup: foreign-key target table name for KindReference. Existence
//     is not verified by this package — the domain service calling
//     Validate is responsible for the FK check.
//   - Pattern: regex applied to string values.
//   - Storage: "attributes_jsonb" (default) or "typed_column:<name>"
//     for audit-critical fields that must live in a real column. The
//     validator does not enforce storage routing; the domain's
//     repository does when it maps attributes onto the row shape.
//   - Description: shown in form UI. Not consumed by Go code.
//   - Deprecated: sourced from YAML's deprecated_since; the validator
//     emits a warning but does not reject writes so existing data can
//     still be updated.
type AttributeSpec struct {
	Kind             AttributeKind
	Required         bool
	Default          *AttributeValue
	Min              *float64
	Max              *float64
	Values           []string
	Lookup           string
	Pattern          string
	Storage          StorageStrategy
	Description      string
	DeprecatedSince  string
	ObjectSchema     map[string]AttributeSpec // populated for KindObject
	ArrayElementKind AttributeKind            // populated for KindStringArray / KindIntArray / KindDecimalArray
}

// StorageStrategy tells the repository how an attribute is persisted.
// It's a declaration, not enforced by this package — the domain
// repository reads Storage and routes accordingly.
type StorageStrategy struct {
	// JSONB is true when the attribute is stored in the entity's
	// attributes JSONB column. Default.
	JSONB bool
	// TypedColumn names a dedicated SQL column when the attribute must
	// be stored as a typed column (for audit-critical fields). Empty
	// unless the YAML specified `storage: typed_column: <name>`.
	TypedColumn string
}

// DefaultStorage returns the implicit storage used when YAML omits the
// `storage:` key — JSONB-backed, the common case.
func DefaultStorage() StorageStrategy {
	return StorageStrategy{JSONB: true}
}

// DerivedAttribute is a YAML-declared computed field. The formula is
// parsed at load time and evaluated server-side on reads; derived
// values are never stored.
type DerivedAttribute struct {
	Name       string
	Formula    string      // original text, kept for diagnostics
	Expression *Expression // parsed AST; non-nil when load succeeded
	Unit       string
	DependsOn  []string // must list every attribute referenced; enforced at load
}

// CustomExtension is the explicit, YAML-declared escape hatch for
// one-off class logic that doesn't fit a shared calculation library
// service. Required extensions are verified to exist by
// tools/check_custom_extensions at CI time.
type CustomExtension struct {
	Name        string
	ServicePath string // e.g. "business/pharma/dea_tracking"
	Required    bool
	Description string
}

// AuditPolicy describes how a class's entities are audited. Attached
// metadata the repository consumes to route audit events and enforce
// retention.
type AuditPolicy struct {
	TrackChanges         bool
	RetainDays           int
	RegulatoryFrameworks []string
}

// ClassDef is the resolved, inheritance-expanded class definition. This
// is the shape downstream code sees — child classes already have their
// base class's attributes merged in, so there's no `extends` pointer
// here. Lookups by domain+class return one of these.
type ClassDef struct {
	Domain            string
	Name              string
	Label             string
	Description       string
	Industries        []string // profiles that pre-enable this class
	Attributes        map[string]AttributeSpec
	ComplianceChecks  []string
	CapacityMetrics   []string
	Processes         []string
	DerivedAttributes map[string]DerivedAttribute
	CustomExtensions  []CustomExtension
	Workflow          string
	Audit             AuditPolicy

	// rawExtends records the YAML's `extends` pointer before resolution.
	// Kept for diagnostics + inheritance-cycle detection during load.
	rawExtends string
}

// AttributeValue is the discriminated-union value type carried by
// entity attribute maps on the wire and in the validator. Exactly one
// of the typed fields is populated per value; the Kind field
// identifies which. Unmarshalers (proto + JSON + YAML) agree on this
// shape so validators can treat attribute payloads uniformly.
//
// This is the Go counterpart of the wire-side
// packages.classregistry.v1.AttributeValue proto message (F.0.3).
type AttributeValue struct {
	Kind AttributeKind

	String    string
	Int       int64
	Decimal   string // decimal-safe; parsed on demand via math/big.Rat
	Bool      bool
	Date      time.Time // normalized to UTC, zero time of day
	Timestamp time.Time
	Duration  time.Duration
	Money     MoneyValue
	GeoPoint  GeoPointValue

	StringArray  []string
	IntArray     []int64
	DecimalArray []string

	Object map[string]AttributeValue
	File   FileRef
}

// MoneyValue carries an amount in a specific currency. Amount is a
// decimal string to preserve precision across the wire without
// float drift; repositories convert to NUMERIC on write.
type MoneyValue struct {
	Amount   string
	Currency string // ISO-4217, e.g. "INR", "USD"
}

// GeoPointValue is a WGS-84 latitude/longitude pair.
type GeoPointValue struct {
	Lat float64
	Lon float64
}

// FileRef points to a blob stored via packages/filestorage. Attribute
// validation enforces presence of BlobRef; the actual blob is fetched
// through the file-storage client when needed.
type FileRef struct {
	BlobRef     string
	Filename    string
	ContentType string
	SizeBytes   int64
}

// Registry is the read interface every downstream consumer depends on.
// Implementations (the load-from-disk one in loader.go; a test one in
// tests) satisfy this interface. The interface deliberately omits
// mutation — class definitions are deployment-time artifacts.
type Registry interface {
	// GetClass returns the resolved class definition for a domain+class
	// pair. Returns a *ClassDef or an error (typed as
	// errors.NotFound when the class is unknown).
	GetClass(domain, class string) (*ClassDef, error)

	// ListClasses returns every class defined for the domain, sorted
	// by Name. Returns empty slice + nil error when the domain has no
	// registry file (treated as "domain not yet consolidated").
	ListClasses(domain string) []*ClassDef

	// ValidateAttributes enforces the class's declared shape against
	// the provided attribute map. Missing required attrs, unknown
	// attrs, type mismatches, min/max violations, enum violations, and
	// pattern violations all produce typed errors. Defaults are
	// applied in-place to the map when absent.
	//
	// A successful return does not mean the attribute map is
	// sufficient for every downstream check (e.g. foreign-key
	// existence): those are the caller's responsibility. It means the
	// map conforms to the class's declared shape.
	ValidateAttributes(domain, class string, attrs map[string]AttributeValue) error

	// ValidateAttributesFromStrings is the string-map variant used by
	// domains whose wire field is `map<string, string> attributes`.
	// It coerces each string into the class's declared AttributeValue
	// kind, then delegates to ValidateAttributes. See
	// coerce_strings.go for the supported kinds and their parse
	// rules. Returns the coerced typed map so callers can use it for
	// downstream derived-attribute computation without re-parsing.
	ValidateAttributesFromStrings(domain, class string, attrs map[string]string) (map[string]AttributeValue, error)

	// ComputeDerived evaluates every derived-attribute formula on the
	// class against the provided attribute map and returns the
	// results. The caller merges results into read responses; derived
	// values are never written.
	ComputeDerived(domain, class string, attrs map[string]AttributeValue) (map[string]AttributeValue, error)

	// GetProcesses returns the Layer 3 calculation service names the
	// class opted into. Empty slice when none.
	GetProcesses(domain, class string) []string

	// GetCustomExtensions returns the escape-hatch extensions the
	// class declared. check_custom_extensions reads these at CI time.
	GetCustomExtensions(domain, class string) []CustomExtension

	// Domains returns the list of domains the registry holds YAML for,
	// sorted. Used by CLI tooling.
	Domains() []string
}
