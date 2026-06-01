// Package convert provides type conversion utilities for the accounts backend.
//
// It handles conversions between:
//   - Protobuf types (wrappers, timestamps, structs) and Go types
//   - SQL nullable types and Go types
//   - PostgreSQL-specific types (pgtype) and Go types
//
// Subpackages:
//   - proto: Protobuf wrapper, timestamp, and struct conversions
//   - sql: SQL nullable type and JSON conversions
//   - ptr: Generic pointer utilities
//   - slice: Slice conversion utilities
//   - maps: Map type conversions (map[string]string ↔ map[string]any)
//   - strconv: String parsing utilities (string ↔ float64, int64, etc.)
//
// Naming Convention:
//   - Functions follow the pattern: {SourceType}To{DestType} or {DestType}From{SourceType}
//   - Example: TimestampToTime, TimeToTimestamp, Float64FromString
//   - Functions returning pointers return nil for invalid/empty input
package convert
