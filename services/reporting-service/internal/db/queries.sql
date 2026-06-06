-- name: CreateReport :one
INSERT INTO reports (id,tenant_id,name,report_type,parameters,status,file_format,requested_by,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING *;

-- name: GetReport :one
SELECT * FROM reports WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL;

-- name: ListReports :many
SELECT * FROM reports WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY created_at DESC;

-- name: UpdateReportStatus :one
UPDATE reports SET status=$3,updated_by=$4,updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING *;

-- name: CreateReportSchedule :one
INSERT INTO report_schedules (id,tenant_id,report_type,schedule,parameters,is_active,next_run_at,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING *;

-- name: GetReportSchedule :one
SELECT * FROM report_schedules WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL;

-- name: ListReportSchedules :many
SELECT * FROM report_schedules WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY created_at;

-- name: UpdateScheduleActive :one
UPDATE report_schedules SET is_active=$3,updated_by=$4,updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING *;

-- name: SoftDeleteSchedule :exec
UPDATE report_schedules SET deleted_at=NOW(),updated_by=$3,updated_at=NOW() WHERE id=$1 AND tenant_id=$2;
