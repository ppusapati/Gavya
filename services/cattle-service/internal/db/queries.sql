-- name: CreateBreed :one
INSERT INTO breeds (id, tenant_id, name, origin, description, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetBreed :one
SELECT * FROM breeds
WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL;

-- name: ListBreeds :many
SELECT * FROM breeds
WHERE tenant_id = $1 AND deleted_at IS NULL
ORDER BY name ASC;

-- name: UpdateBreed :one
UPDATE breeds
SET name = $3, origin = $4, description = $5, updated_by = $6, updated_at = NOW()
WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteBreed :exec
UPDATE breeds
SET deleted_at = NOW(), updated_at = NOW()
WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL;

-- name: CreateCattle :one
INSERT INTO cattle (id, tenant_id, tag_number, name, breed_id, date_of_birth, gender, status, weight, color, owner_id, farm_id, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING *;

-- name: GetCattle :one
SELECT * FROM cattle
WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL;

-- name: ListCattle :many
SELECT * FROM cattle
WHERE tenant_id = $1
  AND deleted_at IS NULL
  AND ($2 = '' OR status = $2)
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: UpdateCattle :one
UPDATE cattle
SET status = $3, weight = $4, updated_by = $5, updated_at = NOW()
WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteCattle :exec
UPDATE cattle
SET deleted_at = NOW(), updated_at = NOW()
WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL;

-- name: CreateCattleLineage :one
INSERT INTO cattle_lineage (id, tenant_id, cattle_id, sire_id, dam_id)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetCattleLineage :one
SELECT * FROM cattle_lineage
WHERE cattle_id = $1 AND tenant_id = $2;
