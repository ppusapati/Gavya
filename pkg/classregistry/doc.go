// Package classregistry is the in-memory index of YAML-defined class
// definitions that back every consolidated business domain in Samavāya.
//
// It is the implementation of Phase F from the BI consolidation roadmap
// and the design at docs/GENERIC_INDUSTRY_IMPLEMENTATION.md.
//
// # What this package does
//
// Business domains (workcenter, asset, bom, journal, …) no longer ship
// five vertical subdirectories each. Instead each domain has exactly
// one generic service whose entities carry a `class` field and an
// `attributes` map. This package loads the YAML definitions for those
// classes, validates attribute shapes against them, resolves
// inheritance, computes derived attributes, and exposes class metadata
// to downstream services.
//
// Three collaborators expect its output:
//
//  1. Generic domain services — call Validate on every write to reject
//     attributes that don't match the class's declared shape.
//  2. Layer 3 calculation services — call GetProcesses to check whether
//     a class opted into the calculation, branch on class internally.
//  3. Frontend — calls the GetClassSchema / ListClasses RPCs (exposed
//     separately by classregistry/api/v1) to render class-aware forms.
//
// # Vocabulary
//
// The words "industry", "class", and "profile" replace the older
// "vertical". A class definition lives here; an industry lives on the
// tenant; a profile (onboarding bundle) is configured separately. See
// docs/GENERIC_INDUSTRY_IMPLEMENTATION.md §3.
//
// # File layout
//
// At runtime the package reads every file under:
//
//	config/class_registry/<domain>.yaml
//
// One file per domain. Each file declares optional base classes that
// supply inherited attributes, then the class entries themselves. The
// Loader merges base classes into each child via `extends`.
//
// # What this package does NOT do
//
//   - It does not run Layer 3 calculations. Those are separate typed
//     services under business/<domain>/<calculation>/.
//   - It does not evaluate branching or recursive logic. Derived
//     attributes use a bounded non-Turing-complete grammar; see
//     expression.go for the parser and permitted functions.
//   - It does not mutate YAML. Class definitions are deployment-time
//     artifacts. Per-tenant overrides live in a separate table and
//     merge in at request time via the Registry's With layer (F.6.2).
package classregistry
