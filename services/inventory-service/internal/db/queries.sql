-- name: CreateWarehouse :one
INSERT INTO warehouses (id,tenant_id,name,code,address,manager_id,status,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING *;

-- name: GetWarehouse :one
SELECT * FROM warehouses WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL;

-- name: ListWarehouses :many
SELECT * FROM warehouses WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY name;

-- name: GetInventoryItem :one
SELECT * FROM inventory_items WHERE warehouse_id=$1 AND sku_id=$2 AND tenant_id=$3;

-- name: UpsertInventoryItem :one
INSERT INTO inventory_items (id,tenant_id,warehouse_id,sku_id,quantity_on_hand,quantity_reserved,reorder_point,max_stock,last_updated_at,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NOW(),$9,$10)
ON CONFLICT (tenant_id,warehouse_id,sku_id) DO UPDATE SET
  quantity_on_hand = EXCLUDED.quantity_on_hand,
  last_updated_at = NOW(),
  updated_by = EXCLUDED.updated_by,
  updated_at = NOW()
RETURNING *;

-- name: CreateStockMovement :one
INSERT INTO stock_movements (id,tenant_id,warehouse_id,sku_id,movement_type,quantity,reference_id,reference_type,notes,moved_at,moved_by,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING *;

-- name: ListStockMovements :many
SELECT * FROM stock_movements WHERE tenant_id=$1 AND warehouse_id=$2 ORDER BY moved_at DESC LIMIT $3 OFFSET $4;

-- name: CreateBatch :one
INSERT INTO batches (id,tenant_id,warehouse_id,sku_id,batch_number,quantity,manufactured_at,expires_at,status,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING *;

-- name: GetBatch :one
SELECT * FROM batches WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL;

-- name: ListExpiringBatches :many
SELECT * FROM batches WHERE tenant_id=$1 AND expires_at <= NOW() + INTERVAL '7 days' AND status='available' AND deleted_at IS NULL ORDER BY expires_at;
