-- name: CreateVaccination :one
INSERT INTO vaccinations (id,tenant_id,cattle_id,vaccine_name,batch_number,administered_at,next_due_date,veterinarian_id,dosage,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING *;

-- name: GetVaccination :one
SELECT * FROM vaccinations WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL;

-- name: ListVaccinationHistory :many
SELECT * FROM vaccinations WHERE tenant_id=$1 AND cattle_id=$2 AND deleted_at IS NULL ORDER BY administered_at DESC;

-- name: ListUpcomingVaccinations :many
SELECT * FROM vaccinations
WHERE tenant_id=$1 AND next_due_date <= NOW() + INTERVAL '7 days'
  AND next_due_date >= NOW() AND deleted_at IS NULL
ORDER BY next_due_date;

-- name: CreateTreatment :one
INSERT INTO treatments (id,tenant_id,cattle_id,diagnosis_code,diagnosis,medicine_name,dosage,treated_at,treated_by,follow_up_date,status,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING *;

-- name: GetTreatment :one
SELECT * FROM treatments WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL;

-- name: ListTreatmentHistory :many
SELECT * FROM treatments WHERE tenant_id=$1 AND cattle_id=$2 AND deleted_at IS NULL ORDER BY treated_at DESC;

-- name: UpdateTreatmentStatus :one
UPDATE treatments SET status=$3,updated_by=$4,updated_at=NOW()
WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING *;

-- name: CreateVetVisit :one
INSERT INTO vet_visits (id,tenant_id,cattle_id,veterinarian_id,visit_date,purpose,notes,cost,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING *;

-- name: GetVetVisit :one
SELECT * FROM vet_visits WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL;

-- name: ListVetVisits :many
SELECT * FROM vet_visits WHERE tenant_id=$1 AND cattle_id=$2 AND deleted_at IS NULL ORDER BY visit_date DESC;
