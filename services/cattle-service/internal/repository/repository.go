package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/audit"

	"github.com/ppusapati/gavya/services/cattle-service/internal/domain"
)

// Repository defines the data access interface for cattle-service.
type Repository interface {
	CreateCattle(ctx context.Context, c *domain.Cattle) (*domain.Cattle, error)
	GetCattle(ctx context.Context, id, tenantID string) (*domain.Cattle, error)
	ListCattle(ctx context.Context, tenantID, status string, limit, offset int) ([]*domain.Cattle, error)
	UpdateCattle(ctx context.Context, c *domain.Cattle) (*domain.Cattle, error)
	SoftDeleteCattle(ctx context.Context, id, tenantID, deletedBy string) error

	CreateBreed(ctx context.Context, b *domain.Breed) (*domain.Breed, error)
	GetBreed(ctx context.Context, id, tenantID string) (*domain.Breed, error)
	ListBreeds(ctx context.Context, tenantID string) ([]*domain.Breed, error)

	CreateCattleLineage(ctx context.Context, l *domain.CattleLineage) (*domain.CattleLineage, error)
	GetCattleLineage(ctx context.Context, cattleID, tenantID string) (*domain.CattleLineage, error)
}

// IDs supplies the identifier each audit entry carries.
type IDs interface{ New() string }

type repo struct {
	db  *pgxpool.Pool
	ids IDs
}

const serviceName = "cattle-service"

// New creates a new Repository backed by the given connection pool.
func New(db *pgxpool.Pool, ids IDs) Repository {
	return &repo{db: db, ids: ids}
}

// ─── Cattle ──────────────────────────────────────────────────────────────────

func (r *repo) CreateCattle(ctx context.Context, c *domain.Cattle) (*domain.Cattle, error) {
	const q = `
INSERT INTO cattle
  (id, tenant_id, tag_number, name, breed_id, date_of_birth, gender, status,
   weight, color, owner_id, farm_id, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
RETURNING id, tenant_id, tag_number, COALESCE(name,''), COALESCE(breed_id,''),
          date_of_birth, gender, status, weight, COALESCE(color,''),
          COALESCE(owner_id,''), COALESCE(farm_id,''),
          created_at, updated_at, created_by, updated_by, deleted_at`

	row := r.db.QueryRow(ctx, q,
		c.ID, c.TenantID, c.TagNumber, c.Name, nilIfEmpty(c.BreedID),
		c.DateOfBirth, c.Gender, c.Status, c.Weight, c.Color,
		nilIfEmpty(c.OwnerID), nilIfEmpty(c.FarmID), c.CreatedBy, c.UpdatedBy,
	)
	return scanCattle(row)
}

func (r *repo) GetCattle(ctx context.Context, id, tenantID string) (*domain.Cattle, error) {
	const q = `
SELECT id, tenant_id, tag_number, COALESCE(name,''), COALESCE(breed_id,''),
       date_of_birth, gender, status, weight, COALESCE(color,''),
       COALESCE(owner_id,''), COALESCE(farm_id,''),
       created_at, updated_at, created_by, updated_by, deleted_at
FROM cattle
WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL`

	row := r.db.QueryRow(ctx, q, id, tenantID)
	return scanCattle(row)
}

func (r *repo) ListCattle(ctx context.Context, tenantID, status string, limit, offset int) ([]*domain.Cattle, error) {
	const q = `
SELECT id, tenant_id, tag_number, COALESCE(name,''), COALESCE(breed_id,''),
       date_of_birth, gender, status, weight, COALESCE(color,''),
       COALESCE(owner_id,''), COALESCE(farm_id,''),
       created_at, updated_at, created_by, updated_by, deleted_at
FROM cattle
WHERE tenant_id = $1 AND deleted_at IS NULL AND ($2 = '' OR status = $2)
ORDER BY created_at DESC
LIMIT $3 OFFSET $4`

	rows, err := r.db.Query(ctx, q, tenantID, status, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list cattle: %w", err)
	}
	defer rows.Close()

	var result []*domain.Cattle
	for rows.Next() {
		c, err := scanCattleFromRows(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// UpdateCattle changes an animal's status and weight and records what they were.
//
// The core entity of the herd, edited in place with no before-image: an animal
// marked sold or dead, a weight restated, and the row afterwards said only who
// had last touched it. A dispute over which animal was sold, or what it weighed
// when it was, had nothing to check against.
func (r *repo) UpdateCattle(ctx context.Context, c *domain.Cattle) (*domain.Cattle, error) {
	const cols = `id, tenant_id, tag_number, COALESCE(name,''), COALESCE(breed_id,''),
          date_of_birth, gender, status, weight, COALESCE(color,''),
          COALESCE(owner_id,''), COALESCE(farm_id,''),
          created_at, updated_at, created_by, updated_by, deleted_at`

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	before, err := scanCattle(tx.QueryRow(ctx,
		`SELECT `+cols+` FROM cattle WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL FOR UPDATE`,
		c.ID, c.TenantID))
	if err != nil {
		return nil, err
	}

	after, err := scanCattle(tx.QueryRow(ctx, `
UPDATE cattle
SET status = $3, weight = $4, updated_by = $5, updated_at = NOW()
WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
RETURNING `+cols, c.ID, c.TenantID, c.Status, c.Weight, c.UpdatedBy))
	if err != nil {
		return nil, err
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "update_cattle", ResourceType: "cattle", ResourceID: c.ID,
		Before: map[string]any{
			"tag_number": before.TagNumber, "status": before.Status, "weight": before.Weight,
		},
		After: map[string]any{
			"tag_number": after.TagNumber, "status": after.Status, "weight": after.Weight,
		},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return after, nil
}

// SoftDeleteCattle marks an animal deleted and records who did it.
//
// It took no actor at all, and that was worse than recording nothing: the update
// stamped `updated_at` to the moment of deletion and left `updated_by` holding
// whoever had last edited the row. A reader pairing those two fields — which is
// what they are for — concluded that person had deleted the animal. The record
// did not merely omit who; it named the wrong person.
//
// The wire request carried a `deleted_by` all along and the handler dropped it,
// so a caller supplying it had every reason to believe it had been kept.
func (r *repo) SoftDeleteCattle(ctx context.Context, id, tenantID, deletedBy string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	// One statement, so the update and the read of what it deleted cannot
	// disagree. RETURNING also answers whether there was anything to delete:
	// `deleted_at IS NULL` means a second delete of the same animal returns no
	// row rather than silently succeeding and writing a second audit entry.
	//
	// The tag number goes into the trail because that is what somebody searches
	// for when an animal has gone missing from a list. The row itself survives —
	// this is a soft delete — so the entry does not have to carry the rest of it.
	var tag string
	if err := tx.QueryRow(ctx,
		`UPDATE cattle SET deleted_at = NOW(), updated_at = NOW(), updated_by = $3
		  WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
		  RETURNING tag_number`,
		id, tenantID, deletedBy).Scan(&tag); err != nil {
		return fmt.Errorf("delete cattle %s: %w", id, err)
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "delete_cattle", ResourceType: "cattle", ResourceID: id,
		Before:      map[string]any{"tag_number": tag, "deleted": false},
		After:       map[string]any{"tag_number": tag, "deleted": true, "deleted_by": deletedBy},
		ServiceName: serviceName,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ─── Breed ────────────────────────────────────────────────────────────────────

func (r *repo) CreateBreed(ctx context.Context, b *domain.Breed) (*domain.Breed, error) {
	const q = `
INSERT INTO breeds (id, tenant_id, name, origin, description, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7)
RETURNING id, tenant_id, name, origin, description, created_at, updated_at, created_by, updated_by, deleted_at`

	row := r.db.QueryRow(ctx, q, b.ID, b.TenantID, b.Name, b.Origin, b.Description, b.CreatedBy, b.UpdatedBy)
	return scanBreed(row)
}

func (r *repo) GetBreed(ctx context.Context, id, tenantID string) (*domain.Breed, error) {
	const q = `
SELECT id, tenant_id, name, origin, description, created_at, updated_at, created_by, updated_by, deleted_at
FROM breeds
WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL`

	row := r.db.QueryRow(ctx, q, id, tenantID)
	return scanBreed(row)
}

func (r *repo) ListBreeds(ctx context.Context, tenantID string) ([]*domain.Breed, error) {
	const q = `
SELECT id, tenant_id, name, origin, description, created_at, updated_at, created_by, updated_by, deleted_at
FROM breeds
WHERE tenant_id = $1 AND deleted_at IS NULL
ORDER BY name ASC`

	rows, err := r.db.Query(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list breeds: %w", err)
	}
	defer rows.Close()

	var result []*domain.Breed
	for rows.Next() {
		b := &domain.Breed{}
		if err := rows.Scan(&b.ID, &b.TenantID, &b.Name, &b.Origin, &b.Description,
			&b.CreatedAt, &b.UpdatedAt, &b.CreatedBy, &b.UpdatedBy, &b.DeletedAt); err != nil {
			return nil, fmt.Errorf("scan breed: %w", err)
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

// ─── CattleLineage ────────────────────────────────────────────────────────────

func (r *repo) CreateCattleLineage(ctx context.Context, l *domain.CattleLineage) (*domain.CattleLineage, error) {
	const q = `
INSERT INTO cattle_lineage (id, tenant_id, cattle_id, sire_id, dam_id)
VALUES ($1,$2,$3,$4,$5)
RETURNING id, tenant_id, cattle_id, sire_id, dam_id`

	row := r.db.QueryRow(ctx, q, l.ID, l.TenantID, l.CattleID, l.SireID, l.DamID)
	out := &domain.CattleLineage{}
	if err := row.Scan(&out.ID, &out.TenantID, &out.CattleID, &out.SireID, &out.DamID); err != nil {
		return nil, fmt.Errorf("create lineage: %w", err)
	}
	return out, nil
}

func (r *repo) GetCattleLineage(ctx context.Context, cattleID, tenantID string) (*domain.CattleLineage, error) {
	const q = `
SELECT id, tenant_id, cattle_id, sire_id, dam_id
FROM cattle_lineage
WHERE cattle_id = $1 AND tenant_id = $2`

	row := r.db.QueryRow(ctx, q, cattleID, tenantID)
	out := &domain.CattleLineage{}
	if err := row.Scan(&out.ID, &out.TenantID, &out.CattleID, &out.SireID, &out.DamID); err != nil {
		return nil, fmt.Errorf("get lineage: %w", err)
	}
	return out, nil
}

// ─── Scan helpers ─────────────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

func scanCattle(s scanner) (*domain.Cattle, error) {
	c := &domain.Cattle{}
	err := s.Scan(
		&c.ID, &c.TenantID, &c.TagNumber, &c.Name, &c.BreedID, &c.DateOfBirth,
		&c.Gender, &c.Status, &c.Weight, &c.Color, &c.OwnerID, &c.FarmID,
		&c.CreatedAt, &c.UpdatedAt, &c.CreatedBy, &c.UpdatedBy, &c.DeletedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan cattle: %w", err)
	}
	return c, nil
}

type rowsScanner interface {
	Scan(dest ...any) error
}

func scanCattleFromRows(s rowsScanner) (*domain.Cattle, error) {
	return scanCattle(s)
}

func scanBreed(s scanner) (*domain.Breed, error) {
	b := &domain.Breed{}
	err := s.Scan(
		&b.ID, &b.TenantID, &b.Name, &b.Origin, &b.Description,
		&b.CreatedAt, &b.UpdatedAt, &b.CreatedBy, &b.UpdatedBy, &b.DeletedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan breed: %w", err)
	}
	return b, nil
}

// nilIfEmpty writes an absent optional reference as NULL rather than as the
// empty string, so the foreign key means what it says.
//
// The reverse of it is COALESCE in every SELECT above, and the pair has to stay
// a pair. It did not: breed_id, owner_id and farm_id were written as NULL and
// read into a plain string, which pgx cannot do. An animal recorded without a
// breed — the ordinary case for a crossbred cow nobody has classified — could be
// created and then never read back, and because ListCattle scans the same
// columns, one such animal made the whole tenant's list fail.
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ensure time import is used
var _ = time.Now
