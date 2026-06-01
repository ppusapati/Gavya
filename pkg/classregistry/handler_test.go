package classregistry

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"

	classregistryv1 "p9e.in/samavaya/packages/classregistry/api/v1"
)

// ===========================================================================
// ListClasses / GetClassSchema / ListDomains
// ===========================================================================

func TestHandler_ListClasses(t *testing.T) {
	reg := mustLoad(t, `
domain: workcenter
classes:
  agriculture_processing:
    label: Agriculture Processing
    industries: [agriculture]
    attributes: {}
    processes: [seasonal_planning]
  water_treatment:
    label: Water Treatment
    industries: [water_utility]
    attributes: {}
`)
	h := NewHandler(reg)

	resp, err := h.ListClasses(context.Background(), connect.NewRequest(&classregistryv1.ListClassesRequest{
		Domain: "workcenter",
	}))
	if err != nil {
		t.Fatalf("ListClasses: %v", err)
	}
	if len(resp.Msg.Classes) != 2 {
		t.Fatalf("got %d classes, want 2", len(resp.Msg.Classes))
	}
	// Sorted by name: agriculture first, water second.
	if resp.Msg.Classes[0].Name != "agriculture_processing" {
		t.Errorf("expected agriculture first, got %q", resp.Msg.Classes[0].Name)
	}
	if !resp.Msg.Classes[0].HasProcesses {
		t.Error("agriculture should have HasProcesses=true")
	}
	if resp.Msg.Classes[1].HasProcesses {
		t.Error("water should have HasProcesses=false")
	}
	// Industries passed through.
	if got := resp.Msg.Classes[0].Industries; len(got) != 1 || got[0] != "agriculture" {
		t.Errorf("industries: %v", got)
	}
}

func TestHandler_ListClasses_EmptyDomain(t *testing.T) {
	reg := mustLoad(t, `
domain: workcenter
classes: {}
`)
	h := NewHandler(reg)

	// Asking for a domain the registry doesn't know returns empty, not error.
	resp, err := h.ListClasses(context.Background(), connect.NewRequest(&classregistryv1.ListClassesRequest{
		Domain: "nonexistent",
	}))
	if err != nil {
		t.Fatalf("ListClasses should not error on unknown domain: %v", err)
	}
	if len(resp.Msg.Classes) != 0 {
		t.Errorf("unknown domain should return empty, got %d", len(resp.Msg.Classes))
	}
}

func TestHandler_ListClasses_MissingDomainRejected(t *testing.T) {
	reg := mustLoad(t, `
domain: workcenter
classes: {}
`)
	h := NewHandler(reg)

	_, err := h.ListClasses(context.Background(), connect.NewRequest(&classregistryv1.ListClassesRequest{
		Domain: "",
	}))
	if err == nil {
		t.Fatal("expected missing-domain rejection")
	}
	var ce *connect.Error
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("expected InvalidArgument, got %v (%T)", err, ce)
	}
}

func TestHandler_GetClassSchema_FullRoundTrip(t *testing.T) {
	reg := mustLoad(t, `
domain: workcenter
classes:
  mfg_discrete:
    label: Discrete Manufacturing Work Center
    description: Generic discrete manufacturing line.
    industries: [manufacturing_discrete, automotive]
    attributes:
      machine_type:
        type: string
        required: true
        pattern: ^[A-Z].*
      theoretical_rate:
        type: decimal
        min: 0
        max: 100000
      changeover_minutes:
        type: int
        min: 0
        default: 15
      uptime_hours:
        type: decimal
      scheduled_hours:
        type: decimal
    compliance_checks: [iso_9001_audit]
    capacity_metrics: [oee]
    processes: [oee_calculation]
    derived_attributes:
      availability_pct:
        formula: uptime_hours / scheduled_hours * 100
        unit: percent
        depends_on: [uptime_hours, scheduled_hours]
    custom_extensions:
      - name: special_cert
        service: business/custom/cert
        required: true
        description: Special certification
    workflow: mfg_lifecycle
    audit:
      track_changes: true
      retain_days: 2555
      regulatory_frameworks: [iso_9001]
`)
	h := NewHandler(reg)
	resp, err := h.GetClassSchema(context.Background(), connect.NewRequest(&classregistryv1.GetClassSchemaRequest{
		Domain: "workcenter",
		Class:  "mfg_discrete",
	}))
	if err != nil {
		t.Fatalf("GetClassSchema: %v", err)
	}

	cd := resp.Msg.Class
	if cd.Name != "mfg_discrete" {
		t.Errorf("name: %q", cd.Name)
	}
	if cd.Label != "Discrete Manufacturing Work Center" {
		t.Errorf("label: %q", cd.Label)
	}

	// Attribute translation.
	mt, ok := cd.Attributes["machine_type"]
	if !ok {
		t.Fatal("machine_type attribute missing")
	}
	if mt.Kind != classregistryv1.AttributeKind_ATTRIBUTE_KIND_STRING {
		t.Errorf("machine_type kind: %v", mt.Kind)
	}
	if !mt.Required {
		t.Error("machine_type should be required")
	}
	if mt.Pattern != "^[A-Z].*" {
		t.Errorf("machine_type pattern: %q", mt.Pattern)
	}

	rate := cd.Attributes["theoretical_rate"]
	if !rate.HasMin || rate.Min != 0 {
		t.Errorf("theoretical_rate min: %v (has=%v)", rate.Min, rate.HasMin)
	}
	if !rate.HasMax || rate.Max != 100000 {
		t.Errorf("theoretical_rate max: %v", rate.Max)
	}

	// Default travels through wire form.
	changeover := cd.Attributes["changeover_minutes"]
	if changeover.DefaultValue == nil {
		t.Fatal("default should be attached")
	}
	if changeover.DefaultValue.GetIntValue() != 15 {
		t.Errorf("default: %v", changeover.DefaultValue.GetIntValue())
	}

	// Derived attributes.
	der, ok := cd.DerivedAttributes["availability_pct"]
	if !ok {
		t.Fatal("derived attribute missing")
	}
	if der.Formula != "uptime_hours / scheduled_hours * 100" {
		t.Errorf("formula: %q", der.Formula)
	}
	if got := der.DependsOn; len(got) != 2 {
		t.Errorf("depends_on: %v", got)
	}

	// Custom extensions.
	if len(cd.CustomExtensions) != 1 {
		t.Fatalf("custom_extensions: %v", cd.CustomExtensions)
	}
	if !cd.CustomExtensions[0].Required {
		t.Error("custom extension required flag lost")
	}

	// Audit.
	if cd.Audit == nil || !cd.Audit.TrackChanges {
		t.Error("audit not populated")
	}
	if cd.Audit.RetainDays != 2555 {
		t.Errorf("retain_days: %d", cd.Audit.RetainDays)
	}

	// Workflow.
	if cd.Workflow != "mfg_lifecycle" {
		t.Errorf("workflow: %q", cd.Workflow)
	}

	// Processes.
	if len(cd.Processes) != 1 || cd.Processes[0] != "oee_calculation" {
		t.Errorf("processes: %v", cd.Processes)
	}
}

func TestHandler_GetClassSchema_UnknownClass(t *testing.T) {
	reg := mustLoad(t, `
domain: workcenter
classes:
  known: { attributes: {} }
`)
	h := NewHandler(reg)

	_, err := h.GetClassSchema(context.Background(), connect.NewRequest(&classregistryv1.GetClassSchemaRequest{
		Domain: "workcenter",
		Class:  "nonexistent",
	}))
	if err == nil {
		t.Fatal("expected not-found")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("expected NotFound code, got %v", connect.CodeOf(err))
	}
}

func TestHandler_ListDomains(t *testing.T) {
	reg, err := LoadBytes(map[string][]byte{
		"workcenter": []byte(`domain: workcenter
classes: {}
`),
		"asset": []byte(`domain: asset
classes: {}
`),
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	h := NewHandler(reg)

	resp, err := h.ListDomains(context.Background(), connect.NewRequest(&classregistryv1.ListDomainsRequest{}))
	if err != nil {
		t.Fatalf("ListDomains: %v", err)
	}
	if len(resp.Msg.Domains) != 2 {
		t.Fatalf("got %v, want 2 domains", resp.Msg.Domains)
	}
	// Sorted.
	if resp.Msg.Domains[0] != "asset" || resp.Msg.Domains[1] != "workcenter" {
		t.Errorf("sort: %v", resp.Msg.Domains)
	}
}

// ===========================================================================
// AttributeValue ⇄ proto round-trips
// ===========================================================================

func TestAttributeValue_Roundtrip_AllKinds(t *testing.T) {
	cases := []struct {
		name string
		in   AttributeValue
	}{
		{"string", AttributeValue{Kind: KindString, String: "hello"}},
		{"int", AttributeValue{Kind: KindInt, Int: 42}},
		{"decimal", AttributeValue{Kind: KindDecimal, Decimal: "3.14"}},
		{"bool_true", AttributeValue{Kind: KindBool, Bool: true}},
		{"bool_false", AttributeValue{Kind: KindBool, Bool: false}},
		{"date", AttributeValue{Kind: KindDate, Date: time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)}},
		{"timestamp", AttributeValue{Kind: KindTimestamp, Timestamp: time.Date(2026, 4, 20, 12, 30, 0, 0, time.UTC)}},
		{"duration", AttributeValue{Kind: KindDuration, Duration: 2 * time.Hour}},
		{"money", AttributeValue{Kind: KindMoney, Money: MoneyValue{Amount: "1000.50", Currency: "INR"}}},
		{"geo", AttributeValue{Kind: KindGeoPoint, GeoPoint: GeoPointValue{Lat: 12.9716, Lon: 77.5946}}},
		{"enum", AttributeValue{Kind: KindEnum, String: "fssai"}},
		{"reference", AttributeValue{Kind: KindReference, String: "cc_123"}},
		{"string_array", AttributeValue{Kind: KindStringArray, StringArray: []string{"a", "b", "c"}}},
		{"int_array", AttributeValue{Kind: KindIntArray, IntArray: []int64{1, 2, 3}}},
		{"decimal_array", AttributeValue{Kind: KindDecimalArray, DecimalArray: []string{"1.1", "2.2"}}},
		{"file", AttributeValue{Kind: KindFile, File: FileRef{
			BlobRef:     "blob_abc",
			Filename:    "report.pdf",
			ContentType: "application/pdf",
			SizeBytes:   12345,
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pb := attributeValueToPB(tc.in)
			out := attributeValueFromPB(pb)
			if out.Kind != tc.in.Kind {
				t.Errorf("kind: got %q, want %q", out.Kind, tc.in.Kind)
			}
			if !equalAttributeValue(out, tc.in) {
				t.Errorf("roundtrip diverged:\n in:  %+v\n out: %+v", tc.in, out)
			}
		})
	}
}

func TestAttributeValue_ObjectRoundtrip(t *testing.T) {
	in := AttributeValue{
		Kind: KindObject,
		Object: map[string]AttributeValue{
			"nested_int":    {Kind: KindInt, Int: 99},
			"nested_string": {Kind: KindString, String: "hi"},
		},
	}
	pb := attributeValueToPB(in)
	out := attributeValueFromPB(pb)
	if len(out.Object) != 2 {
		t.Fatalf("object size: %d", len(out.Object))
	}
	if out.Object["nested_int"].Int != 99 {
		t.Error("nested_int lost")
	}
	if out.Object["nested_string"].String != "hi" {
		t.Error("nested_string lost")
	}
}

func TestAttributeMapFromPB_Helpers(t *testing.T) {
	// Verify the public map helpers work.
	in := map[string]AttributeValue{
		"a": {Kind: KindString, String: "foo"},
		"b": {Kind: KindInt, Int: 10},
	}
	pb := AttributeMapToPB(in)
	out := AttributeMapFromPB(pb)
	if len(out) != 2 {
		t.Fatalf("size: %d", len(out))
	}
	if out["a"].String != "foo" || out["b"].Int != 10 {
		t.Errorf("roundtrip: %+v", out)
	}
}

func TestAttributeMap_NilSafe(t *testing.T) {
	if got := AttributeMapToPB(nil); got != nil {
		t.Errorf("nil in → nil out expected, got %v", got)
	}
	if got := AttributeMapFromPB(nil); got != nil {
		t.Errorf("nil in → nil out expected, got %v", got)
	}
}

// ===========================================================================
// helpers
// ===========================================================================

func equalAttributeValue(a, b AttributeValue) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case KindString, KindEnum, KindReference:
		return a.String == b.String
	case KindInt:
		return a.Int == b.Int
	case KindDecimal:
		return a.Decimal == b.Decimal
	case KindBool:
		return a.Bool == b.Bool
	case KindDate:
		return a.Date.Equal(b.Date)
	case KindTimestamp:
		return a.Timestamp.Equal(b.Timestamp)
	case KindDuration:
		return a.Duration == b.Duration
	case KindMoney:
		return a.Money == b.Money
	case KindGeoPoint:
		return a.GeoPoint == b.GeoPoint
	case KindStringArray:
		return equalStrSlice(a.StringArray, b.StringArray)
	case KindIntArray:
		return equalInt64Slice(a.IntArray, b.IntArray)
	case KindDecimalArray:
		return equalStrSlice(a.DecimalArray, b.DecimalArray)
	case KindFile:
		return a.File == b.File
	}
	return false
}

func equalStrSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalInt64Slice(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
