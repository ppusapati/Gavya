// Package utils is the grab-bag of small, cross-cutting helpers that
// don't fit anywhere else.
//
// The main surface:
//
//   - ParseUpdates(string) map[string]interface{}
//     Parses a request body's update fields into a patch map — used by
//     generic CRUD handlers when the caller sends a sparse update.
//
//   - ExtractFieldsAndValues([]byte) ([]string, []interface{}, error)
//     Flattens a JSON byte slice into two parallel arrays for dynamic
//     SQL builders.
//
// Additions are allowed only when the helper is genuinely cross-cutting.
// If a function is specific to one domain, keep it in that domain's
// package — the platform policy is deliberately strict about what lands
// here to prevent another util-bag from metastasising.
package utils
