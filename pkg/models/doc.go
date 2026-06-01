// Package models holds cross-cutting domain model structs that don't
// belong to any single service but are referenced from several.
//
// Current contents (construction vertical era):
//
//   - BaseModel — the audit-stamped ID / tenant / created/updated fields
//     embedded in most domain rows
//   - ConstructionProject / ProjectPhase / ConstructionBoQ — shared
//     construction-vertical types used by multiple handlers
//
// HISTORICAL NOTE: new domain models should live in their owning service's
// internal/models package (see core/bi/dataset/internal/models as the
// canonical layout). This package is a residue from the pre-vertical
// layout and is shrinking over time. Adding here is allowed only when a
// type is genuinely shared across 3+ services.
package models
