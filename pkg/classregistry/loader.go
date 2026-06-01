package classregistry

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Loader reads class-registry YAML from a directory tree and builds a
// Registry. The directory layout is fixed:
//
//	<root>/<domain>.yaml
//
// One file per domain. See docs/GENERIC_INDUSTRY_IMPLEMENTATION.md §5
// for the format.
//
// The loader resolves inheritance (`extends`) before returning. It
// detects extension cycles and rejects them. Derived-attribute
// formulas are parsed at load time, not evaluation time, so misspelled
// attribute references fail fast.
type Loader struct {
	// Root is the directory containing per-domain YAML files.
	Root string
}

// NewLoader constructs a Loader pointing at a directory.
func NewLoader(root string) *Loader {
	return &Loader{Root: root}
}

// Load walks Root, reads every *.yaml file, parses the class-registry
// format, resolves inheritance, and returns a Registry. Returns an
// error describing the first failure encountered; partial registries
// are not returned.
func (l *Loader) Load() (Registry, error) {
	if l.Root == "" {
		return nil, fmt.Errorf("classregistry: loader root is empty")
	}
	info, err := os.Stat(l.Root)
	if err != nil {
		return nil, fmt.Errorf("classregistry: cannot stat root %q: %w", l.Root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("classregistry: root %q is not a directory", l.Root)
	}

	entries, err := os.ReadDir(l.Root)
	if err != nil {
		return nil, fmt.Errorf("classregistry: cannot read root %q: %w", l.Root, err)
	}

	reg := &memRegistry{
		byDomain: make(map[string]*domainIndex),
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		path := filepath.Join(l.Root, name)
		if err := loadFile(path, reg); err != nil {
			return nil, fmt.Errorf("classregistry: %w", err)
		}
	}
	return reg, nil
}

// LoadBytes is a convenience for tests that want to load YAML content
// directly without touching disk. Each (domain, bytes) entry is
// treated as if it were read from `<root>/<domain>.yaml`.
func LoadBytes(contents map[string][]byte) (Registry, error) {
	reg := &memRegistry{byDomain: make(map[string]*domainIndex)}
	for domain, raw := range contents {
		if err := loadBytesInto(domain, raw, reg); err != nil {
			return nil, fmt.Errorf("classregistry: %w", err)
		}
	}
	return reg, nil
}

// ---------------------------------------------------------------------------
// YAML document shape
// ---------------------------------------------------------------------------

type yamlDoc struct {
	Domain      string                  `yaml:"domain"`
	BaseClasses map[string]yamlClassDef `yaml:"base_classes"`
	Classes     map[string]yamlClassDef `yaml:"classes"`
}

type yamlClassDef struct {
	Label             string                       `yaml:"label"`
	Description       string                       `yaml:"description"`
	Extends           string                       `yaml:"extends"`
	Industries        []string                     `yaml:"industries"`
	Attributes        map[string]yamlAttributeSpec `yaml:"attributes"`
	ComplianceChecks  []string                     `yaml:"compliance_checks"`
	CapacityMetrics   []string                     `yaml:"capacity_metrics"`
	Processes         []string                     `yaml:"processes"`
	DerivedAttributes map[string]yamlDerivedAttr   `yaml:"derived_attributes"`
	CustomExtensions  []yamlCustomExt              `yaml:"custom_extensions"`
	Workflow          string                       `yaml:"workflow"`
	Audit             *yamlAudit                   `yaml:"audit"`
}

type yamlAttributeSpec struct {
	Type             string                       `yaml:"type"`
	Required         bool                         `yaml:"required"`
	Default          any                          `yaml:"default"`
	Min              *float64                     `yaml:"min"`
	Max              *float64                     `yaml:"max"`
	Values           []string                     `yaml:"values"`
	Lookup           string                       `yaml:"lookup"`
	Pattern          string                       `yaml:"pattern"`
	Storage          string                       `yaml:"storage"`
	Description      string                       `yaml:"description"`
	DeprecatedSince  string                       `yaml:"deprecated_since"`
	Schema           map[string]yamlAttributeSpec `yaml:"schema"`
}

type yamlDerivedAttr struct {
	Formula   string   `yaml:"formula"`
	Unit      string   `yaml:"unit"`
	DependsOn []string `yaml:"depends_on"`
}

type yamlCustomExt struct {
	Name        string `yaml:"name"`
	Service     string `yaml:"service"`
	Required    bool   `yaml:"required"`
	Description string `yaml:"description"`
}

type yamlAudit struct {
	TrackChanges         bool     `yaml:"track_changes"`
	RetainDays           int      `yaml:"retain_days"`
	RegulatoryFrameworks []string `yaml:"regulatory_frameworks"`
}

// ---------------------------------------------------------------------------
// Parsing → ClassDef
// ---------------------------------------------------------------------------

func loadFile(path string, reg *memRegistry) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %q: %w", path, err)
	}
	base := filepath.Base(path)
	inferredDomain := strings.TrimSuffix(strings.TrimSuffix(base, ".yaml"), ".yml")
	return loadBytesInto(inferredDomain, raw, reg)
}

func loadBytesInto(inferredDomain string, raw []byte, reg *memRegistry) error {
	var doc yamlDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parse yaml for %q: %w", inferredDomain, err)
	}
	domain := doc.Domain
	if domain == "" {
		domain = inferredDomain
	}
	if domain != inferredDomain && inferredDomain != "" {
		// The file is named <domain>.yaml; the document's `domain:`
		// field must match or the loader rejects. Prevents drift
		// between filename and declared domain.
		return fmt.Errorf("domain mismatch in %q.yaml: file implies %q but document declares %q",
			inferredDomain, inferredDomain, domain)
	}

	idx := &domainIndex{
		Domain:      domain,
		baseClasses: make(map[string]*ClassDef),
		classes:     make(map[string]*ClassDef),
	}
	if prev, ok := reg.byDomain[domain]; ok && prev != nil {
		return fmt.Errorf("duplicate domain %q", domain)
	}
	reg.byDomain[domain] = idx

	// Parse base classes first so `extends` lookups resolve.
	for name, yc := range doc.BaseClasses {
		cd, err := parseClass(domain, name, yc)
		if err != nil {
			return fmt.Errorf("base_class %q: %w", name, err)
		}
		idx.baseClasses[name] = cd
	}

	for name, yc := range doc.Classes {
		cd, err := parseClass(domain, name, yc)
		if err != nil {
			return fmt.Errorf("class %q: %w", name, err)
		}
		idx.classes[name] = cd
	}

	// Resolve inheritance. Detect cycles.
	for name, cd := range idx.classes {
		if err := resolveInheritance(idx, cd, map[string]bool{name: true}); err != nil {
			return fmt.Errorf("class %q: %w", name, err)
		}
	}

	// After inheritance resolution, parse derived-attribute formulas.
	// We do this last so derived formulas can reference attributes
	// inherited from base classes.
	for name, cd := range idx.classes {
		for dname, d := range cd.DerivedAttributes {
			expr, err := ParseExpression(d.Formula, d.DependsOn)
			if err != nil {
				return fmt.Errorf("class %q derived attribute %q: %w", name, dname, err)
			}
			// Cross-check that every dep in `depends_on` is a real
			// attribute on the (resolved) class.
			for _, dep := range d.DependsOn {
				if _, ok := cd.Attributes[dep]; !ok {
					return fmt.Errorf("class %q derived attribute %q: depends_on references unknown attribute %q",
						name, dname, dep)
				}
			}
			d.Expression = expr
			cd.DerivedAttributes[dname] = d
		}
	}

	return nil
}

func parseClass(domain, name string, yc yamlClassDef) (*ClassDef, error) {
	cd := &ClassDef{
		Domain:            domain,
		Name:              name,
		Label:             yc.Label,
		Description:       yc.Description,
		Industries:        append([]string(nil), yc.Industries...),
		Attributes:        make(map[string]AttributeSpec, len(yc.Attributes)),
		ComplianceChecks:  append([]string(nil), yc.ComplianceChecks...),
		CapacityMetrics:   append([]string(nil), yc.CapacityMetrics...),
		Processes:         append([]string(nil), yc.Processes...),
		DerivedAttributes: make(map[string]DerivedAttribute, len(yc.DerivedAttributes)),
		Workflow:          yc.Workflow,
		rawExtends:        yc.Extends,
	}
	if yc.Audit != nil {
		cd.Audit = AuditPolicy{
			TrackChanges:         yc.Audit.TrackChanges,
			RetainDays:           yc.Audit.RetainDays,
			RegulatoryFrameworks: append([]string(nil), yc.Audit.RegulatoryFrameworks...),
		}
	}

	for attrName, ya := range yc.Attributes {
		spec, err := parseAttr(ya)
		if err != nil {
			return nil, fmt.Errorf("attribute %q: %w", attrName, err)
		}
		cd.Attributes[attrName] = spec
	}

	for dname, yd := range yc.DerivedAttributes {
		if yd.Formula == "" {
			return nil, fmt.Errorf("derived_attribute %q: formula is required", dname)
		}
		cd.DerivedAttributes[dname] = DerivedAttribute{
			Name:      dname,
			Formula:   yd.Formula,
			Unit:      yd.Unit,
			DependsOn: append([]string(nil), yd.DependsOn...),
		}
	}

	for _, yc := range yc.CustomExtensions {
		if yc.Name == "" || yc.Service == "" {
			return nil, fmt.Errorf("custom_extension: name and service are required")
		}
		cd.CustomExtensions = append(cd.CustomExtensions, CustomExtension{
			Name:        yc.Name,
			ServicePath: yc.Service,
			Required:    yc.Required,
			Description: yc.Description,
		})
	}

	return cd, nil
}

func parseAttr(ya yamlAttributeSpec) (AttributeSpec, error) {
	kind := AttributeKind(ya.Type)
	if !kind.Valid() {
		return AttributeSpec{}, fmt.Errorf("unknown type %q", ya.Type)
	}
	spec := AttributeSpec{
		Kind:            kind,
		Required:        ya.Required,
		Min:             ya.Min,
		Max:             ya.Max,
		Values:          append([]string(nil), ya.Values...),
		Lookup:          ya.Lookup,
		Pattern:         ya.Pattern,
		Description:     ya.Description,
		DeprecatedSince: ya.DeprecatedSince,
	}

	// Storage.
	spec.Storage = DefaultStorage()
	if ya.Storage != "" {
		if strings.HasPrefix(ya.Storage, "typed_column:") {
			col := strings.TrimSpace(strings.TrimPrefix(ya.Storage, "typed_column:"))
			if col == "" {
				return AttributeSpec{}, fmt.Errorf("storage: typed_column requires a column name")
			}
			spec.Storage = StorageStrategy{TypedColumn: col}
		} else if ya.Storage == "attributes_jsonb" {
			spec.Storage = StorageStrategy{JSONB: true}
		} else {
			return AttributeSpec{}, fmt.Errorf("storage: unknown value %q (use attributes_jsonb or typed_column:<name>)", ya.Storage)
		}
	}

	// Enum sanity.
	if spec.Kind == KindEnum && len(spec.Values) == 0 {
		return AttributeSpec{}, fmt.Errorf("enum requires at least one value")
	}

	// Array element kinds (derived from the collection kind).
	switch spec.Kind {
	case KindStringArray:
		spec.ArrayElementKind = KindString
	case KindIntArray:
		spec.ArrayElementKind = KindInt
	case KindDecimalArray:
		spec.ArrayElementKind = KindDecimal
	}

	// Object schema.
	if spec.Kind == KindObject {
		if len(ya.Schema) == 0 {
			return AttributeSpec{}, fmt.Errorf("object type requires a schema")
		}
		spec.ObjectSchema = make(map[string]AttributeSpec, len(ya.Schema))
		for sn, ss := range ya.Schema {
			sub, err := parseAttr(ss)
			if err != nil {
				return AttributeSpec{}, fmt.Errorf("schema.%s: %w", sn, err)
			}
			spec.ObjectSchema[sn] = sub
		}
	}

	// Default. Kept as the raw YAML value; coerced to AttributeValue
	// via coerceValue on first use. The coercion may fail, so we only
	// validate shape now.
	if ya.Default != nil {
		dv, err := coerceValue(ya.Default, spec)
		if err != nil {
			return AttributeSpec{}, fmt.Errorf("default: %w", err)
		}
		spec.Default = &dv
	}

	return spec, nil
}

// resolveInheritance expands the class's extends chain in place.
// Attributes from base classes are added if not already declared on
// the child (child overrides win). Compliance/metrics/processes are
// unioned and deduplicated.
func resolveInheritance(idx *domainIndex, cd *ClassDef, seen map[string]bool) error {
	if cd.rawExtends == "" {
		return nil
	}
	parentName := cd.rawExtends
	if seen[parentName] {
		return fmt.Errorf("extends cycle: %v", sortedKeys(seen))
	}
	parent, ok := idx.baseClasses[parentName]
	if !ok {
		// Allow extending another class in the same file as well —
		// chain resolves either way. Tried base_classes first; fall
		// through to classes.
		parent, ok = idx.classes[parentName]
		if !ok {
			return fmt.Errorf("extends: unknown class %q", parentName)
		}
	}
	seen[parentName] = true
	if err := resolveInheritance(idx, parent, seen); err != nil {
		return err
	}

	for n, a := range parent.Attributes {
		if _, ok := cd.Attributes[n]; !ok {
			cd.Attributes[n] = a
		}
	}
	cd.ComplianceChecks = mergeStrings(cd.ComplianceChecks, parent.ComplianceChecks)
	cd.CapacityMetrics = mergeStrings(cd.CapacityMetrics, parent.CapacityMetrics)
	cd.Processes = mergeStrings(cd.Processes, parent.Processes)
	for n, d := range parent.DerivedAttributes {
		if _, ok := cd.DerivedAttributes[n]; !ok {
			cd.DerivedAttributes[n] = d
		}
	}
	if cd.Workflow == "" {
		cd.Workflow = parent.Workflow
	}
	// Audit: child overrides parent if set; otherwise inherit. Empty
	// child audit is detected by a zero TrackChanges + RetainDays +
	// absence of frameworks, because AuditPolicy contains a slice
	// field that isn't directly comparable.
	if !cd.Audit.TrackChanges && cd.Audit.RetainDays == 0 && len(cd.Audit.RegulatoryFrameworks) == 0 {
		cd.Audit = parent.Audit
	}
	return nil
}

func mergeStrings(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	for _, s := range a {
		seen[s] = struct{}{}
	}
	for _, s := range b {
		if _, ok := seen[s]; ok {
			continue
		}
		a = append(a, s)
		seen[s] = struct{}{}
	}
	sort.Strings(a)
	return a
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
