-- name: CreateFileRecord :one
INSERT INTO file_records (id,tenant_id,original_name,stored_name,content_type,size_bytes,storage_path,storage_provider,entity_type,entity_id,uploaded_by,is_public,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING *;

-- name: GetFileRecord :one
SELECT * FROM file_records WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL;

-- name: ListEntityFiles :many
SELECT * FROM file_records WHERE tenant_id=$1 AND entity_type=$2 AND entity_id=$3 AND deleted_at IS NULL ORDER BY created_at DESC;

-- name: SoftDeleteFile :exec
UPDATE file_records SET deleted_at=NOW(),updated_by=$3,updated_at=NOW() WHERE id=$1 AND tenant_id=$2;
