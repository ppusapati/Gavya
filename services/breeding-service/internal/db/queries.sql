-- name: CreateBreedingCycle :one
INSERT INTO breeding_cycles (id,tenant_id,cattle_id,heat_date,status,notes,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING *;

-- name: GetBreedingCycle :one
SELECT * FROM breeding_cycles WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL;

-- name: ListCattleBreedingCycles :many
SELECT * FROM breeding_cycles WHERE tenant_id=$1 AND cattle_id=$2 AND deleted_at IS NULL ORDER BY heat_date DESC;

-- name: UpdateCycleStatus :one
UPDATE breeding_cycles SET status=$3,updated_by=$4,updated_at=NOW()
WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING *;

-- name: CreateInsemination :one
INSERT INTO inseminations (id,tenant_id,cycle_id,cattle_id,bull_id,semen_batch_id,inseminated_at,method,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING *;

-- name: GetInsemination :one
SELECT * FROM inseminations WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL;

-- name: CreatePregnancy :one
INSERT INTO pregnancies (id,tenant_id,cattle_id,insemination_id,confirmed_at,expected_calving_date,status,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING *;

-- name: GetPregnancy :one
SELECT * FROM pregnancies WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL;

-- name: UpdatePregnancyStatus :one
UPDATE pregnancies SET status=$3,updated_by=$4,updated_at=NOW()
WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING *;

-- name: ListActivePregnancies :many
SELECT * FROM pregnancies WHERE tenant_id=$1 AND status='active' AND deleted_at IS NULL ORDER BY expected_calving_date;

-- name: CreateCalvingRecord :one
INSERT INTO calving_records (id,tenant_id,pregnancy_id,cattle_id,calf_id,calving_date,calf_gender,calf_weight,complications,status,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING *;
