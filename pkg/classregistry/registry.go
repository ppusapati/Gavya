package classregistry

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"p9e.in/samavaya/packages/errors"
)

// domainIndex holds a single domain's parsed classes (both base and
// leaf) plus a cache of compiled regex patterns.
type domainIndex struct {
	Domain      string
	baseClasses map[string]*ClassDef
	classes     map[string]*ClassDef
	regexCache  map[string]*regexp.Regexp // spec.Pattern → compiled regex; populated lazily
}

// memRegistry is the default, in-memory Registry implementation that
// the Loader produces. Per-tenant overlays are F.6.2 work — they wrap
// a *memRegistry with a tenant-aware facade.
type memRegistry struct {
	byDomain map[string]*domainIndex
}

// GetClass returns the resolved class definition for domain+class.
func (r *memRegistry) GetClass(domain, class string) (*ClassDef, error) {
	idx, ok := r.byDomain[domain]
	if !ok {
		return nil, errors.NotFound(
			"CLASSREGISTRY_DOMAIN_NOT_FOUND",
			fmt.Sprintf("no class registry loaded for domain %q", domain),
		)
	}
	cd, ok := idx.classes[class]
	if !ok {
		return nil, errors.NotFound(
			"CLASSREGISTRY_CLASS_NOT_FOUND",
			fmt.Sprintf("class %q not defined in domain %q", class, domain),
		)
	}
	return cd, nil
}

// ListClasses returns every class in domain, sorted by Name.
func (r *memRegistry) ListClasses(domain string) []*ClassDef {
	idx, ok := r.byDomain[domain]
	if !ok {
		return nil
	}
	out := make([]*ClassDef, 0, len(idx.classes))
	for _, cd := range idx.classes {
		out = append(out, cd)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Domains returns the list of domains the registry knows about.
func (r *memRegistry) Domains() []string {
	out := make([]string, 0, len(r.byDomain))
	for d := range r.byDomain {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// GetProcesses returns the Layer 3 service names the class opted into.
func (r *memRegistry) GetProcesses(domain, class string) []string {
	cd, err := r.GetClass(domain, class)
	if err != nil {
		return nil
	}
	return append([]string(nil), cd.Processes...)
}

// GetCustomExtensions returns the escape-hatch extensions.
func (r *memRegistry) GetCustomExtensions(domain, class string) []CustomExtension {
	cd, err := r.GetClass(domain, class)
	if err != nil {
		return nil
	}
	return append([]CustomExtension(nil), cd.CustomExtensions...)
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

// ValidateAttributes enforces the class's declared attribute shape
// against the provided map. Defaults for absent attributes are
// applied in-place. On violation, returns an errors.BadRequest carrying
// a CLASSREGISTRY_* reason code plus a message naming the offending
// attribute.
func (r *memRegistry) ValidateAttributes(domain, class string, attrs map[string]AttributeValue) error {
	cd, err := r.GetClass(domain, class)
	if err != nil {
		return err
	}
	if attrs == nil {
		return errors.BadRequest(
			"CLASSREGISTRY_ATTRIBUTES_NIL",
			"attributes map is nil",
		)
	}
	idx := r.byDomain[domain]

	// 1. Apply defaults for absent attributes.
	for name, spec := range cd.Attributes {
		if _, present := attrs[name]; !present && spec.Default != nil {
			attrs[name] = *spec.Default
		}
	}

	// 2. Reject unknown attributes.
	for name := range attrs {
		if _, ok := cd.Attributes[name]; !ok {
			return errors.BadRequest(
				"CLASSREGISTRY_UNKNOWN_ATTRIBUTE",
				fmt.Sprintf("class %q has no attribute %q", class, name),
			)
		}
	}

	// 3. Enforce required + per-attribute rules.
	for name, spec := range cd.Attributes {
		v, present := attrs[name]
		if !present {
			if spec.Required {
				return errors.BadRequest(
					"CLASSREGISTRY_MISSING_REQUIRED",
					fmt.Sprintf("class %q requires attribute %q", class, name),
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

// ComputeDerived evaluates every derived-attribute formula on the
// class against attrs. The returned map is fresh (does not alias
// attrs). Errors bubble typed via errors.Internal — derived formulas
// are supposed to parse cleanly at load time; a runtime failure
// indicates bad data, not bad config.
func (r *memRegistry) ComputeDerived(domain, class string, attrs map[string]AttributeValue) (map[string]AttributeValue, error) {
	cd, err := r.GetClass(domain, class)
	if err != nil {
		return nil, err
	}
	out := make(map[string]AttributeValue, len(cd.DerivedAttributes))
	for name, d := range cd.DerivedAttributes {
		if d.Expression == nil {
			// Defensive: loader should have parsed. Skip rather than
			// panic.
			continue
		}
		v, err := d.Expression.Evaluate(attrs)
		if err != nil {
			return nil, errors.InternalServer(
				"CLASSREGISTRY_DERIVED_EVAL_FAILED",
				fmt.Sprintf("class %q derived attribute %q evaluation failed: %v", class, name, err),
			)
		}
		out[name] = v
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Per-value validation
// ---------------------------------------------------------------------------

func validateValue(idx *domainIndex, name string, spec AttributeSpec, v AttributeValue) error {
	if v.Kind != spec.Kind {
		return errors.BadRequest(
			"CLASSREGISTRY_TYPE_MISMATCH",
			fmt.Sprintf("attribute %q: expected %s, got %s", name, spec.Kind, v.Kind),
		)
	}

	switch spec.Kind {
	case KindString:
		return validateString(idx, name, spec, v.String)
	case KindInt:
		return validateInt(name, spec, v.Int)
	case KindDecimal:
		return validateDecimal(name, spec, v.Decimal)
	case KindBool:
		return nil
	case KindDate, KindTimestamp:
		return validateTime(name, v)
	case KindDuration:
		return nil
	case KindMoney:
		return validateMoney(name, v.Money)
	case KindGeoPoint:
		return validateGeo(name, v.GeoPoint)

	case KindStringArray:
		for i, s := range v.StringArray {
			if err := validateString(idx, fmt.Sprintf("%s[%d]", name, i),
				AttributeSpec{Kind: KindString, Pattern: spec.Pattern}, s); err != nil {
				return err
			}
		}
		return nil
	case KindIntArray:
		for i, n := range v.IntArray {
			if err := validateInt(fmt.Sprintf("%s[%d]", name, i),
				AttributeSpec{Kind: KindInt, Min: spec.Min, Max: spec.Max}, n); err != nil {
				return err
			}
		}
		return nil
	case KindDecimalArray:
		for i, d := range v.DecimalArray {
			if err := validateDecimal(fmt.Sprintf("%s[%d]", name, i),
				AttributeSpec{Kind: KindDecimal, Min: spec.Min, Max: spec.Max}, d); err != nil {
				return err
			}
		}
		return nil

	case KindEnum:
		for _, allowed := range spec.Values {
			if allowed == v.String {
				return nil
			}
		}
		return errors.BadRequest(
			"CLASSREGISTRY_ENUM_VIOLATION",
			fmt.Sprintf("attribute %q: %q is not in enum %v", name, v.String, spec.Values),
		)

	case KindReference:
		// Lookup existence is the caller's responsibility; we just
		// enforce non-empty when required.
		if spec.Required && v.String == "" {
			return errors.BadRequest(
				"CLASSREGISTRY_REFERENCE_EMPTY",
				fmt.Sprintf("attribute %q: required reference is empty", name),
			)
		}
		return nil

	case KindObject:
		for subName, subSpec := range spec.ObjectSchema {
			sub, present := v.Object[subName]
			if !present {
				if subSpec.Required {
					return errors.BadRequest(
						"CLASSREGISTRY_MISSING_REQUIRED",
						fmt.Sprintf("attribute %q.%q: required", name, subName),
					)
				}
				continue
			}
			if err := validateValue(idx, name+"."+subName, subSpec, sub); err != nil {
				return err
			}
		}
		// Reject unknown sub-fields.
		for subName := range v.Object {
			if _, ok := spec.ObjectSchema[subName]; !ok {
				return errors.BadRequest(
					"CLASSREGISTRY_UNKNOWN_ATTRIBUTE",
					fmt.Sprintf("attribute %q.%q is not declared", name, subName),
				)
			}
		}
		return nil

	case KindFile:
		if spec.Required && v.File.BlobRef == "" {
			return errors.BadRequest(
				"CLASSREGISTRY_FILE_EMPTY",
				fmt.Sprintf("attribute %q: required file is missing blob_ref", name),
			)
		}
		return nil
	}

	return errors.InternalServer(
		"CLASSREGISTRY_UNKNOWN_KIND",
		fmt.Sprintf("attribute %q: validator has no case for kind %q", name, spec.Kind),
	)
}

func validateString(idx *domainIndex, name string, spec AttributeSpec, s string) error {
	if spec.Required && s == "" {
		return errors.BadRequest(
			"CLASSREGISTRY_MISSING_REQUIRED",
			fmt.Sprintf("attribute %q: required", name),
		)
	}
	if spec.Pattern != "" && s != "" {
		re, err := compilePattern(idx, spec.Pattern)
		if err != nil {
			return errors.InternalServer(
				"CLASSREGISTRY_BAD_PATTERN",
				fmt.Sprintf("attribute %q: invalid pattern %q: %v", name, spec.Pattern, err),
			)
		}
		if !re.MatchString(s) {
			return errors.BadRequest(
				"CLASSREGISTRY_PATTERN_VIOLATION",
				fmt.Sprintf("attribute %q: value %q does not match pattern %q", name, s, spec.Pattern),
			)
		}
	}
	return nil
}

func validateInt(name string, spec AttributeSpec, n int64) error {
	if spec.Min != nil && float64(n) < *spec.Min {
		return errors.BadRequest(
			"CLASSREGISTRY_MIN_VIOLATION",
			fmt.Sprintf("attribute %q: value %d < min %v", name, n, *spec.Min),
		)
	}
	if spec.Max != nil && float64(n) > *spec.Max {
		return errors.BadRequest(
			"CLASSREGISTRY_MAX_VIOLATION",
			fmt.Sprintf("attribute %q: value %d > max %v", name, n, *spec.Max),
		)
	}
	return nil
}

func validateDecimal(name string, spec AttributeSpec, d string) error {
	if d == "" {
		return nil
	}
	f, err := strconv.ParseFloat(d, 64)
	if err != nil {
		return errors.BadRequest(
			"CLASSREGISTRY_DECIMAL_PARSE",
			fmt.Sprintf("attribute %q: %q is not a valid decimal", name, d),
		)
	}
	if spec.Min != nil && f < *spec.Min {
		return errors.BadRequest(
			"CLASSREGISTRY_MIN_VIOLATION",
			fmt.Sprintf("attribute %q: value %v < min %v", name, f, *spec.Min),
		)
	}
	if spec.Max != nil && f > *spec.Max {
		return errors.BadRequest(
			"CLASSREGISTRY_MAX_VIOLATION",
			fmt.Sprintf("attribute %q: value %v > max %v", name, f, *spec.Max),
		)
	}
	return nil
}

func validateTime(name string, v AttributeValue) error {
	t := v.Timestamp
	if t.IsZero() {
		t = v.Date
	}
	if t.IsZero() {
		return errors.BadRequest(
			"CLASSREGISTRY_TIME_EMPTY",
			fmt.Sprintf("attribute %q: time value is zero", name),
		)
	}
	return nil
}

func validateMoney(name string, m MoneyValue) error {
	if m.Currency == "" {
		return errors.BadRequest(
			"CLASSREGISTRY_MONEY_NO_CURRENCY",
			fmt.Sprintf("attribute %q: money value missing currency", name),
		)
	}
	if _, err := strconv.ParseFloat(m.Amount, 64); err != nil && m.Amount != "" {
		return errors.BadRequest(
			"CLASSREGISTRY_MONEY_BAD_AMOUNT",
			fmt.Sprintf("attribute %q: money amount %q is not a valid number", name, m.Amount),
		)
	}
	return nil
}

func validateGeo(name string, g GeoPointValue) error {
	if g.Lat < -90 || g.Lat > 90 {
		return errors.BadRequest(
			"CLASSREGISTRY_GEO_LAT_RANGE",
			fmt.Sprintf("attribute %q: lat %v out of range [-90, 90]", name, g.Lat),
		)
	}
	if g.Lon < -180 || g.Lon > 180 {
		return errors.BadRequest(
			"CLASSREGISTRY_GEO_LON_RANGE",
			fmt.Sprintf("attribute %q: lon %v out of range [-180, 180]", name, g.Lon),
		)
	}
	return nil
}

func compilePattern(idx *domainIndex, pat string) (*regexp.Regexp, error) {
	if idx.regexCache == nil {
		idx.regexCache = make(map[string]*regexp.Regexp)
	}
	if re, ok := idx.regexCache[pat]; ok {
		return re, nil
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, err
	}
	idx.regexCache[pat] = re
	return re, nil
}

// ---------------------------------------------------------------------------
// Value coercion (for defaults read from raw YAML)
// ---------------------------------------------------------------------------

// coerceValue turns an untyped value from YAML (string, int, float,
// bool, map, slice) into an AttributeValue conforming to the spec's
// kind. Only used for the YAML-declared `default:` field; runtime
// payloads arrive already typed via the proto AttributeValue.
func coerceValue(v any, spec AttributeSpec) (AttributeValue, error) {
	out := AttributeValue{Kind: spec.Kind}
	switch spec.Kind {
	case KindString, KindEnum, KindReference:
		s, ok := v.(string)
		if !ok {
			return out, fmt.Errorf("expected string, got %T", v)
		}
		out.String = s
	case KindInt:
		switch n := v.(type) {
		case int:
			out.Int = int64(n)
		case int64:
			out.Int = n
		case float64:
			out.Int = int64(n)
		default:
			return out, fmt.Errorf("expected int, got %T", v)
		}
	case KindDecimal:
		switch n := v.(type) {
		case string:
			out.Decimal = n
		case int:
			out.Decimal = strconv.Itoa(n)
		case int64:
			out.Decimal = strconv.FormatInt(n, 10)
		case float64:
			out.Decimal = strconv.FormatFloat(n, 'f', -1, 64)
		default:
			return out, fmt.Errorf("expected decimal-compatible, got %T", v)
		}
	case KindBool:
		b, ok := v.(bool)
		if !ok {
			return out, fmt.Errorf("expected bool, got %T", v)
		}
		out.Bool = b
	case KindDate:
		s, ok := v.(string)
		if !ok {
			return out, fmt.Errorf("expected date string, got %T", v)
		}
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return out, fmt.Errorf("invalid date %q: %w", s, err)
		}
		out.Date = t
	case KindTimestamp:
		s, ok := v.(string)
		if !ok {
			return out, fmt.Errorf("expected timestamp string, got %T", v)
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return out, fmt.Errorf("invalid timestamp %q: %w", s, err)
		}
		out.Timestamp = t
	case KindDuration:
		s, ok := v.(string)
		if !ok {
			return out, fmt.Errorf("expected duration string, got %T", v)
		}
		d, err := time.ParseDuration(s)
		if err != nil {
			return out, fmt.Errorf("invalid duration %q: %w", s, err)
		}
		out.Duration = d
	case KindStringArray:
		arr, ok := v.([]any)
		if !ok {
			return out, fmt.Errorf("expected string array, got %T", v)
		}
		for _, el := range arr {
			s, ok := el.(string)
			if !ok {
				return out, fmt.Errorf("string_array element: expected string, got %T", el)
			}
			out.StringArray = append(out.StringArray, s)
		}
	}
	// Other kinds (IntArray, DecimalArray, Money, GeoPoint, Object,
	// File) don't get sensible defaults from YAML — they require
	// structured construction. Callers that need defaults for those
	// kinds set them programmatically.
	_ = strings.TrimSpace // keep unused import honest in case of refactor
	return out, nil
}
