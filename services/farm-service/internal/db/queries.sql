-- name: CreateFarm :one
INSERT INTO farms (id,tenant_id,name,code,address,city,state,country,capacity,manager_id,status,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING *;

-- name: GetFarm :one
SELECT * FROM farms WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL;

-- name: ListFarms :many
SELECT * FROM farms WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY name;

-- name: UpdateFarm :one
UPDATE farms SET name=$3,address=$4,city=$5,state=$6,country=$7,manager_id=$8,status=$9,updated_by=$10,updated_at=NOW()
WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING *;

-- name: UpdateFarmCapacity :one
UPDATE farms SET capacity=$3,updated_by=$4,updated_at=NOW()
WHERE id=$1 AND tenant_id=$2 RETURNING *;

-- name: CreateFarmSection :one
INSERT INTO farm_sections (id,tenant_id,farm_id,name,section_type,capacity,current_occupancy,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING *;

-- name: GetFarmSection :one
SELECT * FROM farm_sections WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL;

-- name: ListFarmSections :many
SELECT * FROM farm_sections WHERE tenant_id=$1 AND farm_id=$2 AND deleted_at IS NULL ORDER BY name;

-- name: UpdateFarmSection :one
UPDATE farm_sections SET name=$3,section_type=$4,capacity=$5,current_occupancy=$6,updated_by=$7,updated_at=NOW()
WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING *;
