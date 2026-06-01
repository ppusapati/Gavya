package classregistry

import "context"

// ClassEntity is one row in classregistry.class_entities. This is the
// Go shape of an entity stored in the generic Layer 2 entity plane —
// i.e. an instance of a class whose shape is declared in
// `config/class_registry/<domain>.yaml` but which does not have a
// dedicated business-service table of its own.
//
// The Layer 2 model in docs/VERTICAL_GAP_ANALYSIS.md maps ClassEntity
// to SAP's AUSP row, Oracle's DFF-extended record, and Dynamics's
// Dataverse entity row.
type ClassEntity struct {
	// ID is the ULID row identifier.
	ID string

	// TenantID scopes the row. Every entity is tenant-owned.
	TenantID string

	// Domain + Class identifies which class registry this row is an
	// instance of. They must resolve to a real class via
	// Registry.GetClass(Domain, Class).
	Domain string
	Class  string

	// NaturalKey is the domain-meaningful identifier. Scoped by
	// (tenant, domain, class). Examples: a PPA contract's
	// contract_number, a crop_master crop's crop_code.
	NaturalKey string

	// Label is a human-readable display name. Admin UIs show this;
	// machine code uses NaturalKey.
	Label string

	// Attributes is the typed attribute map matching the class's
	// AttributeSpec shape. Validation against the class is enforced
	// at write time by the EntityStore implementation calling the
	// registry's ValidateAttributes before insert.
	Attributes map[string]AttributeValue

	// Status is 'active' (live) or 'archived' (existing references
	// still resolve, new references discouraged).
	Status string
}

// EntityListFilter narrows ListForTenantClass results.
type EntityListFilter struct {
	TenantID string
	Domain   string
	Class    string

	// NaturalKeyPrefix, when non-empty, restricts rows to those
	// whose natural_key starts with the given prefix. Useful for
	// autocomplete / typeahead UIs.
	NaturalKeyPrefix string

	// IncludeArchived defaults to false — only 'active' rows.
	IncludeArchived bool

	Limit  int32
	Offset int32
}

// EntityStore is the narrow persistence port the resolver + onboarding
// provisioner + admin UIs depend on. An SQL-backed impl lives under
// packages/classregistry/pgstore/entities.go; tests may substitute
// an in-memory fake.
//
// All methods are tenant-scoped — the tenant_id on each call is
// mandatory. The store itself does not read ambient tenant context
// (p9context.FromCurrentTenant); the caller must supply it explicitly.
// This keeps the port usable in both request-scoped code and
// onboarding/seeding code that operates outside an http request.
type EntityStore interface {
	// GetByNaturalKey returns the live entity for (tenant, domain,
	// class, natural_key) or an errors.NotFound-typed error. This is
	// the hot path — the lookup resolver hits it.
	GetByNaturalKey(
		ctx context.Context,
		tenantID, domain, class, naturalKey string,
	) (*ClassEntity, error)

	// Exists returns true if a live entity exists for (tenant, domain,
	// class, natural_key). Cheaper than GetByNaturalKey when callers
	// only need existence (e.g. foreign-key validation on an attribute
	// with `reference: lookup: X`).
	Exists(
		ctx context.Context,
		tenantID, domain, class, naturalKey string,
	) (bool, error)

	// List returns a page of entities matching the filter, sorted by
	// (domain, class, natural_key). The second return is the total
	// count matching the filter before Limit/Offset.
	List(ctx context.Context, filter EntityListFilter) ([]*ClassEntity, int32, error)

	// Upsert inserts or updates an entity by (tenant, domain, class,
	// natural_key). The store is responsible for running
	// ValidateAttributes against the registry before writing.
	// Returns the written row's ID.
	Upsert(ctx context.Context, entity *ClassEntity, actorID string) (string, error)

	// Archive flips status to 'archived' — existing references still
	// resolve but new references should not be created. To fully
	// remove the row use Delete.
	Archive(ctx context.Context, tenantID, domain, class, naturalKey, actorID string) error

	// Delete soft-deletes the entity. The row is preserved for audit;
	// queries through the hot-path index skip it.
	Delete(ctx context.Context, tenantID, domain, class, naturalKey, actorID string) error
}
