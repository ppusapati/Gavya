-- name: CreateMilkSession :one
INSERT INTO milk_sessions (id, tenant_id, cattle_id, session_date, shift_type, status, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetMilkSession :one
SELECT * FROM milk_sessions WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL;

-- name: ListMilkSessions :many
SELECT * FROM milk_sessions WHERE tenant_id = $1 AND deleted_at IS NULL ORDER BY session_date DESC LIMIT $2 OFFSET $3;

-- name: UpdateMilkSessionStatus :one
UPDATE milk_sessions SET status = $3, updated_by = $4, updated_at = NOW() WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL RETURNING *;

-- name: CreateMilkRecord :one
INSERT INTO milk_records (id, tenant_id, session_id, cattle_id, quantity_liters, recorded_at, recorded_by, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetMilkRecord :one
SELECT * FROM milk_records WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL;

-- name: ListSessionRecords :many
SELECT * FROM milk_records WHERE session_id = $1 AND tenant_id = $2 AND deleted_at IS NULL ORDER BY recorded_at;

-- name: GetDailyYield :one
SELECT COALESCE(SUM(quantity_liters), 0) AS total FROM milk_records WHERE tenant_id = $1 AND cattle_id = $2 AND recorded_at::date = $3 AND deleted_at IS NULL;

-- name: CreateMilkQuality :one
INSERT INTO milk_quality (id, tenant_id, record_id, fat_percent, snf_percent, lactose, tested_at, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetMilkQuality :one
SELECT * FROM milk_quality WHERE record_id = $1 AND tenant_id = $2;
