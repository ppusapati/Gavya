// Package merger recursively merges struct, slice, and map values.
//
// Used primarily to reconcile layered configuration (defaults → file →
// env → CLI flags) where each source contributes a partial value for the
// same target struct. Unlike github.com/imdario/mergo this implementation
// is pointer-aware and handles protobuf-generated types without panicking
// on oneof fields.
//
// Entry points:
//
//	merger.Map(dst, src, opts...)              // merge non-zero fields
//	merger.MapWithOverwrite(dst, src, opts...) // src always wins
//
// Options (Config pattern):
//
//   - Type-checking: require dst and src to share the same concrete type
//   - Custom transformers: per-type merge logic (e.g. time.Duration add)
//
// The Transformers interface lets callers provide domain-specific merge
// strategies — see Config.Transformers for the extension point.
package merger
