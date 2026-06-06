-- name: CreateNotification :one
INSERT INTO notifications (id,tenant_id,recipient_id,recipient_type,channel,title,body,status,priority,reference_id,reference_type,sent_at,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING *;

-- name: GetNotification :one
SELECT * FROM notifications WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL;

-- name: UpdateNotificationStatus :one
UPDATE notifications SET status=$3,updated_by=$4,updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING *;

-- name: MarkNotificationRead :one
UPDATE notifications SET status='read',read_at=NOW(),updated_by=$3,updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING *;

-- name: MarkAllNotificationsRead :exec
UPDATE notifications SET status='read',read_at=NOW(),updated_by=$3,updated_at=NOW()
WHERE recipient_id=$1 AND tenant_id=$2 AND status != 'read' AND deleted_at IS NULL;

-- name: ListNotifications :many
SELECT * FROM notifications WHERE tenant_id=$1 AND channel=$2 AND status=$3 AND deleted_at IS NULL ORDER BY created_at DESC;

-- name: GetUnreadCount :one
SELECT COUNT(*) FROM notifications WHERE tenant_id=$1 AND recipient_id=$2 AND status != 'read' AND deleted_at IS NULL;

-- name: CreateNotificationTemplate :one
INSERT INTO notification_templates (id,tenant_id,event_type,channel,title,body_template,is_active,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING *;

-- name: ListNotificationTemplates :many
SELECT * FROM notification_templates WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY event_type;
