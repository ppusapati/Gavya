package errors

import "google.golang.org/protobuf/runtime/protoimpl"

// Status is a status error structure used by the errors package.
// This replaces the proto-generated type for compilation.
type Status struct {
	Code     int32             `json:"code"`
	Reason   string            `json:"reason"`
	Message  string            `json:"message"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Extension descriptors for the protoc-gen-go-errors plugin.
//
// Historical context (2026-04-19 sweep): the original options proto —
// options/errors.proto declaring `errors.default_code` (enum) and
// `errors.code` (enum value) — was never regenerated after this package
// was stripped down to a hand-written Status struct. The plugin in
// packages/cmd/protoc-gen-go-errors still imports `errors.E_DefaultCode`
// and `errors.E_Code` to call `proto.GetExtension` when reading
// per-enum HTTP code hints. Without these identifiers the plugin won't
// compile.
//
// These are deliberate stubs: uninitialised `*protoimpl.ExtensionInfo`
// values. `proto.GetExtension` with an empty ExtensionInfo returns the
// type's zero value (int32(0)), which the plugin already treats as
// "no override — skip this enum value" (see genErrorsReason — the
// `if enumCode == 0 { continue }` branch). So the plugin still builds and
// runs; it just emits no error definitions until the proto-generated
// descriptors land.
//
// Regenerating this package from a proper errors.proto is tracked under
// roadmap Phase B follow-ups.
var (
	E_DefaultCode = &protoimpl.ExtensionInfo{}
	E_Code        = &protoimpl.ExtensionInfo{}
)
