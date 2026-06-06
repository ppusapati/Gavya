-- name: CreateTenant :one
INSERT INTO tenants (id,name,slug,plan,status,contact_email,contact_phone,address,country,timezone,currency,max_users,max_cattle,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING *;

-- name: GetTenant :one
SELECT * FROM tenants WHERE id=$1 AND deleted_at IS NULL;

-- name: GetTenantBySlug :one
SELECT * FROM tenants WHERE slug=$1 AND deleted_at IS NULL;

-- name: ListTenants :many
SELECT * FROM tenants WHERE deleted_at IS NULL ORDER BY name;

-- name: UpdateTenant :one
UPDATE tenants SET name=$2,contact_email=$3,contact_phone=$4,address=$5,country=$6,timezone=$7,currency=$8,max_users=$9,max_cattle=$10,updated_by=$11,updated_at=NOW()
WHERE id=$1 AND deleted_at IS NULL RETURNING *;

-- name: UpdateTenantStatus :one
UPDATE tenants SET status=$2,updated_by=$3,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL RETURNING *;

-- name: UpsertTenantSetting :one
INSERT INTO tenant_settings (id,tenant_id,key,value,data_type,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (tenant_id,key) DO UPDATE SET value=EXCLUDED.value,data_type=EXCLUDED.data_type,updated_by=EXCLUDED.updated_by,updated_at=NOW()
RETURNING *;

-- name: ListTenantSettings :many
SELECT * FROM tenant_settings WHERE tenant_id=$1 ORDER BY key;
