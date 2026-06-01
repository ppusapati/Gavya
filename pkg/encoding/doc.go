// Package encoding is the pluggable codec registry used for content
// negotiation at the Transport boundary.
//
// Registered codecs implement:
//
//	Marshal(v interface{}) ([]byte, error)
//	Unmarshal(data []byte, v interface{}) error
//	Name() string
//
// Subpackages ship standard codecs and register themselves via init():
//
//   - encoding/json  — JSON (application/json)
//   - encoding/proto — Protobuf (application/x-protobuf)
//   - encoding/xml   — XML (application/xml)
//   - encoding/yaml  — YAML (application/x-yaml)
//   - encoding/form  — URL-encoded form data (supports protobuf messages)
//
// Content negotiation:
//
//	codec := encoding.GetCodec("json")     // by subtype
//	bytes, _ := codec.Marshal(response)
//
// RegisterCodec / MustRegisterCodec let callers plug in custom encodings;
// the registry is process-global and panics on duplicate names via Must.
package encoding
