-- name: CreateAuditLog :one
INSERT INTO audit_logs (id,tenant_id,actor_id,actor_type,action,resource_type,resource_id,old_value,new_value,ip_address,user_agent,service_name,trace_id,created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING *;

-- name: GetAuditLog :one
SELECT * FROM audit_logs WHERE id=$1 AND tenant_id=$2;

-- name: ListAuditLogs :many
SELECT * FROM audit_logs WHERE tenant_id=$1 ORDER BY created_at DESC;

-- name: ListAuditLogsByResource :many
SELECT * FROM audit_logs WHERE tenant_id=$1 AND resource_type=$2 AND resource_id=$3 ORDER BY created_at DESC;

-- name: ListAuditLogsByActor :many
SELECT * FROM audit_logs WHERE tenant_id=$1 AND actor_id=$2 ORDER BY created_at DESC;
