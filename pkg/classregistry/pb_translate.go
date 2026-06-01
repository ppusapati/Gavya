package classregistry

import (
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	classregistryv1 "p9e.in/samavaya/packages/classregistry/api/v1"
)

// ClassDef ↔ proto translation lives here so handlers and adapters
// share one implementation. The Go types in types.go stay
// proto-unaware; translators live in this file.

// ClassDefToPB is the exported form of the ClassDef→proto translator.
// Generic domain handlers scaffolded by tools/vertical_scaffolder use
// this through their mappers package, so handlers don't import
// classregistry's private symbols.
func ClassDefToPB(cd *ClassDef) *classregistryv1.ClassDefinition {
	return classDefToPB(cd)
}

// classDefToPB converts a resolved ClassDef into its wire form.
// Called by GetClassSchema to build the response payload.
func classDefToPB(cd *ClassDef) *classregistryv1.ClassDefinition {
	if cd == nil {
		return nil
	}
	out := &classregistryv1.ClassDefinition{
		Domain:            cd.Domain,
		Name:              cd.Name,
		Label:             cd.Label,
		Description:       cd.Description,
		Industries:        append([]string(nil), cd.Industries...),
		Attributes:        make(map[string]*classregistryv1.AttributeDefinition, len(cd.Attributes)),
		ComplianceChecks:  append([]string(nil), cd.ComplianceChecks...),
		CapacityMetrics:   append([]string(nil), cd.CapacityMetrics...),
		Processes:         append([]string(nil), cd.Processes...),
		DerivedAttributes: make(map[string]*classregistryv1.DerivedAttributeDefinition, len(cd.DerivedAttributes)),
		Workflow:          cd.Workflow,
		Audit: &classregistryv1.AuditPolicy{
			TrackChanges:         cd.Audit.TrackChanges,
			RetainDays:           int32(cd.Audit.RetainDays),
			RegulatoryFrameworks: append([]string(nil), cd.Audit.RegulatoryFrameworks...),
		},
	}
	for name, spec := range cd.Attributes {
		out.Attributes[name] = attributeSpecToPB(spec)
	}
	for name, d := range cd.DerivedAttributes {
		out.DerivedAttributes[name] = &classregistryv1.DerivedAttributeDefinition{
			Name:      d.Name,
			Formula:   d.Formula,
			Unit:      d.Unit,
			DependsOn: append([]string(nil), d.DependsOn...),
		}
	}
	for _, ext := range cd.CustomExtensions {
		out.CustomExtensions = append(out.CustomExtensions, &classregistryv1.CustomExtensionDefinition{
			Name:        ext.Name,
			ServicePath: ext.ServicePath,
			Required:    ext.Required,
			Description: ext.Description,
		})
	}
	return out
}

func attributeSpecToPB(spec AttributeSpec) *classregistryv1.AttributeDefinition {
	out := &classregistryv1.AttributeDefinition{
		Kind:             kindToPB(spec.Kind),
		Required:         spec.Required,
		Values:           append([]string(nil), spec.Values...),
		Lookup:           spec.Lookup,
		Pattern:          spec.Pattern,
		Description:      spec.Description,
		DeprecatedSince:  spec.DeprecatedSince,
		ArrayElementKind: kindToPB(spec.ArrayElementKind),
	}
	if spec.Min != nil {
		out.HasMin = true
		out.Min = *spec.Min
	}
	if spec.Max != nil {
		out.HasMax = true
		out.Max = *spec.Max
	}
	if spec.Default != nil {
		out.DefaultValue = attributeValueToPB(*spec.Default)
	}
	switch {
	case spec.Storage.TypedColumn != "":
		out.Storage = "typed_column:" + spec.Storage.TypedColumn
	case spec.Storage.JSONB:
		out.Storage = "attributes_jsonb"
	}
	if len(spec.ObjectSchema) > 0 {
		out.ObjectSchema = make(map[string]*classregistryv1.AttributeDefinition, len(spec.ObjectSchema))
		for name, sub := range spec.ObjectSchema {
			out.ObjectSchema[name] = attributeSpecToPB(sub)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// AttributeValue ⇄ proto
// ---------------------------------------------------------------------------

// attributeValueToPB converts a Go AttributeValue into its wire form.
// Used by defaults travelling through GetClassSchema.
func attributeValueToPB(v AttributeValue) *classregistryv1.AttributeValue {
	out := &classregistryv1.AttributeValue{Kind: kindToPB(v.Kind)}
	switch v.Kind {
	case KindString, KindEnum, KindReference:
		out.Value = &classregistryv1.AttributeValue_StringValue{StringValue: v.String}
	case KindInt:
		out.Value = &classregistryv1.AttributeValue_IntValue{IntValue: v.Int}
	case KindDecimal:
		out.Value = &classregistryv1.AttributeValue_DecimalValue{DecimalValue: v.Decimal}
	case KindBool:
		out.Value = &classregistryv1.AttributeValue_BoolValue{BoolValue: v.Bool}
	case KindDate:
		if !v.Date.IsZero() {
			out.Value = &classregistryv1.AttributeValue_DateValue{DateValue: v.Date.Format("2006-01-02")}
		}
	case KindTimestamp:
		if !v.Timestamp.IsZero() {
			out.Value = &classregistryv1.AttributeValue_TimestampValue{TimestampValue: timestamppb.New(v.Timestamp)}
		}
	case KindDuration:
		out.Value = &classregistryv1.AttributeValue_DurationValue{DurationValue: durationpb.New(v.Duration)}
	case KindMoney:
		out.Value = &classregistryv1.AttributeValue_MoneyValue{
			MoneyValue: &classregistryv1.MoneyValue{Amount: v.Money.Amount, Currency: v.Money.Currency},
		}
	case KindGeoPoint:
		out.Value = &classregistryv1.AttributeValue_GeoPointValue{
			GeoPointValue: &classregistryv1.GeoPointValue{Lat: v.GeoPoint.Lat, Lon: v.GeoPoint.Lon},
		}
	case KindStringArray:
		out.Value = &classregistryv1.AttributeValue_StringArray{
			StringArray: &classregistryv1.StringArray{Values: append([]string(nil), v.StringArray...)},
		}
	case KindIntArray:
		out.Value = &classregistryv1.AttributeValue_IntArray{
			IntArray: &classregistryv1.IntArray{Values: append([]int64(nil), v.IntArray...)},
		}
	case KindDecimalArray:
		out.Value = &classregistryv1.AttributeValue_DecimalArray{
			DecimalArray: &classregistryv1.DecimalArray{Values: append([]string(nil), v.DecimalArray...)},
		}
	case KindObject:
		obj := &classregistryv1.AttributeObject{Fields: make(map[string]*classregistryv1.AttributeValue, len(v.Object))}
		for k, sub := range v.Object {
			obj.Fields[k] = attributeValueToPB(sub)
		}
		out.Value = &classregistryv1.AttributeValue_ObjectValue{ObjectValue: obj}
	case KindFile:
		out.Value = &classregistryv1.AttributeValue_FileValue{
			FileValue: &classregistryv1.FileRef{
				BlobRef:     v.File.BlobRef,
				Filename:    v.File.Filename,
				ContentType: v.File.ContentType,
				SizeBytes:   v.File.SizeBytes,
			},
		}
	}
	return out
}

// attributeValueFromPB converts a wire AttributeValue into the Go
// type. Used on RPC entry where generic domain services receive
// attribute maps.
func attributeValueFromPB(pb *classregistryv1.AttributeValue) AttributeValue {
	if pb == nil {
		return AttributeValue{}
	}
	out := AttributeValue{Kind: kindFromPB(pb.GetKind())}
	switch v := pb.GetValue().(type) {
	case *classregistryv1.AttributeValue_StringValue:
		out.String = v.StringValue
	case *classregistryv1.AttributeValue_IntValue:
		out.Int = v.IntValue
	case *classregistryv1.AttributeValue_DecimalValue:
		out.Decimal = v.DecimalValue
	case *classregistryv1.AttributeValue_BoolValue:
		out.Bool = v.BoolValue
	case *classregistryv1.AttributeValue_DateValue:
		if t, err := time.Parse("2006-01-02", v.DateValue); err == nil {
			out.Date = t
		}
	case *classregistryv1.AttributeValue_TimestampValue:
		if ts := v.TimestampValue; ts != nil {
			out.Timestamp = ts.AsTime()
		}
	case *classregistryv1.AttributeValue_DurationValue:
		if d := v.DurationValue; d != nil {
			out.Duration = d.AsDuration()
		}
	case *classregistryv1.AttributeValue_MoneyValue:
		if m := v.MoneyValue; m != nil {
			out.Money = MoneyValue{Amount: m.Amount, Currency: m.Currency}
		}
	case *classregistryv1.AttributeValue_GeoPointValue:
		if g := v.GeoPointValue; g != nil {
			out.GeoPoint = GeoPointValue{Lat: g.Lat, Lon: g.Lon}
		}
	case *classregistryv1.AttributeValue_StringArray:
		if arr := v.StringArray; arr != nil {
			out.StringArray = append([]string(nil), arr.Values...)
		}
	case *classregistryv1.AttributeValue_IntArray:
		if arr := v.IntArray; arr != nil {
			out.IntArray = append([]int64(nil), arr.Values...)
		}
	case *classregistryv1.AttributeValue_DecimalArray:
		if arr := v.DecimalArray; arr != nil {
			out.DecimalArray = append([]string(nil), arr.Values...)
		}
	case *classregistryv1.AttributeValue_ObjectValue:
		if obj := v.ObjectValue; obj != nil {
			out.Object = make(map[string]AttributeValue, len(obj.Fields))
			for k, sub := range obj.Fields {
				out.Object[k] = attributeValueFromPB(sub)
			}
		}
	case *classregistryv1.AttributeValue_FileValue:
		if f := v.FileValue; f != nil {
			out.File = FileRef{
				BlobRef:     f.BlobRef,
				Filename:    f.Filename,
				ContentType: f.ContentType,
				SizeBytes:   f.SizeBytes,
			}
		}
	}
	return out
}

// AttributeMapFromPB is the public helper domain handlers use to
// convert incoming wire attribute maps into the Go form consumed by
// ValidateAttributes and downstream repositories.
func AttributeMapFromPB(pb map[string]*classregistryv1.AttributeValue) map[string]AttributeValue {
	if pb == nil {
		return nil
	}
	out := make(map[string]AttributeValue, len(pb))
	for k, v := range pb {
		out[k] = attributeValueFromPB(v)
	}
	return out
}

// AttributeMapToPB is the outbound counterpart. Domain handlers use
// it to emit entity payloads on List/Get/Update responses.
func AttributeMapToPB(attrs map[string]AttributeValue) map[string]*classregistryv1.AttributeValue {
	if attrs == nil {
		return nil
	}
	out := make(map[string]*classregistryv1.AttributeValue, len(attrs))
	for k, v := range attrs {
		out[k] = attributeValueToPB(v)
	}
	return out
}

// ---------------------------------------------------------------------------
// Kind ⇄ proto enum
// ---------------------------------------------------------------------------

func kindToPB(k AttributeKind) classregistryv1.AttributeKind {
	switch k {
	case KindString:
		return classregistryv1.AttributeKind_ATTRIBUTE_KIND_STRING
	case KindInt:
		return classregistryv1.AttributeKind_ATTRIBUTE_KIND_INT
	case KindDecimal:
		return classregistryv1.AttributeKind_ATTRIBUTE_KIND_DECIMAL
	case KindBool:
		return classregistryv1.AttributeKind_ATTRIBUTE_KIND_BOOL
	case KindDate:
		return classregistryv1.AttributeKind_ATTRIBUTE_KIND_DATE
	case KindTimestamp:
		return classregistryv1.AttributeKind_ATTRIBUTE_KIND_TIMESTAMP
	case KindDuration:
		return classregistryv1.AttributeKind_ATTRIBUTE_KIND_DURATION
	case KindMoney:
		return classregistryv1.AttributeKind_ATTRIBUTE_KIND_MONEY
	case KindGeoPoint:
		return classregistryv1.AttributeKind_ATTRIBUTE_KIND_GEO_POINT
	case KindStringArray:
		return classregistryv1.AttributeKind_ATTRIBUTE_KIND_STRING_ARRAY
	case KindIntArray:
		return classregistryv1.AttributeKind_ATTRIBUTE_KIND_INT_ARRAY
	case KindDecimalArray:
		return classregistryv1.AttributeKind_ATTRIBUTE_KIND_DECIMAL_ARRAY
	case KindEnum:
		return classregistryv1.AttributeKind_ATTRIBUTE_KIND_ENUM
	case KindReference:
		return classregistryv1.AttributeKind_ATTRIBUTE_KIND_REFERENCE
	case KindObject:
		return classregistryv1.AttributeKind_ATTRIBUTE_KIND_OBJECT
	case KindFile:
		return classregistryv1.AttributeKind_ATTRIBUTE_KIND_FILE
	}
	return classregistryv1.AttributeKind_ATTRIBUTE_KIND_UNSPECIFIED
}

// AttributeKindFromPB is the exported form of the proto→canonical
// AttributeKind translator. Client/port pb_translate.go files use
// this to restore a lowercase canonical kind string (e.g. "string",
// "enum") from the proto enum variant (e.g. ATTRIBUTE_KIND_STRING).
//
// Before this helper existed, every port's pb_translate.go called
// spec.GetKind().String() directly, which returns the proto enum
// name ("ATTRIBUTE_KIND_STRING"). The in-proc path uses
// string(spec.Kind) which returns the canonical form ("string").
// That divergence is caught by packages/classregistry/contract and
// fixed by replacing the String() call with this helper.
func AttributeKindFromPB(k classregistryv1.AttributeKind) string {
	return string(kindFromPB(k))
}

func kindFromPB(k classregistryv1.AttributeKind) AttributeKind {
	switch k {
	case classregistryv1.AttributeKind_ATTRIBUTE_KIND_STRING:
		return KindString
	case classregistryv1.AttributeKind_ATTRIBUTE_KIND_INT:
		return KindInt
	case classregistryv1.AttributeKind_ATTRIBUTE_KIND_DECIMAL:
		return KindDecimal
	case classregistryv1.AttributeKind_ATTRIBUTE_KIND_BOOL:
		return KindBool
	case classregistryv1.AttributeKind_ATTRIBUTE_KIND_DATE:
		return KindDate
	case classregistryv1.AttributeKind_ATTRIBUTE_KIND_TIMESTAMP:
		return KindTimestamp
	case classregistryv1.AttributeKind_ATTRIBUTE_KIND_DURATION:
		return KindDuration
	case classregistryv1.AttributeKind_ATTRIBUTE_KIND_MONEY:
		return KindMoney
	case classregistryv1.AttributeKind_ATTRIBUTE_KIND_GEO_POINT:
		return KindGeoPoint
	case classregistryv1.AttributeKind_ATTRIBUTE_KIND_STRING_ARRAY:
		return KindStringArray
	case classregistryv1.AttributeKind_ATTRIBUTE_KIND_INT_ARRAY:
		return KindIntArray
	case classregistryv1.AttributeKind_ATTRIBUTE_KIND_DECIMAL_ARRAY:
		return KindDecimalArray
	case classregistryv1.AttributeKind_ATTRIBUTE_KIND_ENUM:
		return KindEnum
	case classregistryv1.AttributeKind_ATTRIBUTE_KIND_REFERENCE:
		return KindReference
	case classregistryv1.AttributeKind_ATTRIBUTE_KIND_OBJECT:
		return KindObject
	case classregistryv1.AttributeKind_ATTRIBUTE_KIND_FILE:
		return KindFile
	}
	return ""
}
