package classregistry

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"p9e.in/samavaya/packages/errors"
)

// ValidateAttributesFromStrings is the string-map variant of
// ValidateAttributes. Many existing domains persist attributes as
// `map<string, string>` (serialized to JSONB as strings); this
// function accepts that representation, coerces each value into the
// typed AttributeValue the class declares, and runs the standard
// validator.
//
// It exists because reshaping every pre-Phase-F domain's attribute
// field to a typed AttributeValue map is a multi-PR churn. The
// coercion layer lets a class registry attach to an existing
// string-map-valued field additively, matching how SAP's
// classification system overlays on top of existing characteristic
// strings.
//
// Unknown attributes fail (same as ValidateAttributes). String-typed
// attributes pass through unchanged. Number / bool / date / timestamp
// / money / enum / reference all parse according to the class's
// declared kind — a parse failure surfaces as a typed
// CLASSREGISTRY_COERCE_* error naming the attribute + its declared
// kind + the offending value. Arrays and objects are not supported
// via this path (string-map callers shouldn't need them); use the
// typed ValidateAttributes directly when those appear.
//
// The typed AttributeValue map is returned alongside so callers can
// use the coerced values downstream (e.g. for derived-attribute
// computation). The original string map is not mutated.
func (r *memRegistry) ValidateAttributesFromStrings(
	domain, class string,
	attrs map[string]string,
) (map[string]AttributeValue, error) {
	cd, err := r.GetClass(domain, class)
	if err != nil {
		return nil, err
	}
	if attrs == nil {
		// Empty map is fine — the underlying validator applies
		// defaults and checks required-ness. Nil becomes empty.
		attrs = map[string]string{}
	}

	typed := make(map[string]AttributeValue, len(cd.Attributes))
	for key, raw := range attrs {
		spec, ok := cd.Attributes[key]
		if !ok {
			// Let the downstream validator produce the standard
			// unknown-attribute error for consistency.
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

	if err := r.ValidateAttributes(domain, class, typed); err != nil {
		return nil, err
	}
	return typed, nil
}

// coerceStringValue turns a raw string into the AttributeValue the
// spec declares. Only the kinds that have a sensible single-string
// representation are supported here; richer types (arrays, objects,
// files, geo-points, money with currency) require the typed
// validator entry point.
func coerceStringValue(raw string, spec AttributeSpec) (AttributeValue, error) {
	out := AttributeValue{Kind: spec.Kind}
	raw = strings.TrimSpace(raw)

	switch spec.Kind {
	case KindString, KindEnum, KindReference:
		out.String = raw
		return out, nil

	case KindInt:
		if raw == "" {
			return out, nil
		}
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return out, fmt.Errorf("not a valid integer")
		}
		out.Int = n
		return out, nil

	case KindDecimal:
		if raw == "" {
			return out, nil
		}
		// Parse to validate, then store the original string to
		// preserve precision — matches how classregistry handles
		// decimals elsewhere.
		if _, err := strconv.ParseFloat(raw, 64); err != nil {
			return out, fmt.Errorf("not a valid decimal")
		}
		out.Decimal = raw
		return out, nil

	case KindBool:
		if raw == "" {
			return out, nil
		}
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return out, fmt.Errorf("not a valid bool (expected true/false)")
		}
		out.Bool = b
		return out, nil

	case KindDate:
		if raw == "" {
			return out, nil
		}
		t, err := time.Parse("2006-01-02", raw)
		if err != nil {
			return out, fmt.Errorf("not a valid date (YYYY-MM-DD)")
		}
		out.Date = t
		return out, nil

	case KindTimestamp:
		if raw == "" {
			return out, nil
		}
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return out, fmt.Errorf("not a valid RFC3339 timestamp")
		}
		out.Timestamp = t
		return out, nil

	case KindDuration:
		if raw == "" {
			return out, nil
		}
		d, err := time.ParseDuration(raw)
		if err != nil {
			return out, fmt.Errorf("not a valid duration")
		}
		out.Duration = d
		return out, nil
	}

	return out, fmt.Errorf("kind %s not supported by string-map coercion (use typed ValidateAttributes)", spec.Kind)
}

// StringAttributesValidator is a narrow convenience interface so
// domain services can depend on just the string-map validation
// method rather than the full Registry surface. Used by Phase F
// consolidated services that retain a map<string, string> wire
// field.
type StringAttributesValidator interface {
	ValidateAttributesFromStrings(domain, class string, attrs map[string]string) (map[string]AttributeValue, error)
}
