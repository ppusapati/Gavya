-- name: CreateOrder :one
INSERT INTO orders (id,tenant_id,customer_id,order_number,status,sub_total,tax_amount,total_amount,currency,shipping_address,notes,ordered_at,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING *;

-- name: GetOrder :one
SELECT * FROM orders WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL;

-- name: ListOrders :many
SELECT * FROM orders WHERE tenant_id=$1 AND status=$2 AND deleted_at IS NULL ORDER BY ordered_at DESC;

-- name: UpdateOrderStatus :one
UPDATE orders SET status=$3,updated_by=$4,updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING *;

-- name: UpdateOrderTotals :one
UPDATE orders SET sub_total=$3,tax_amount=$4,total_amount=$5,updated_by=$6,updated_at=NOW()
WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING *;

-- name: CreateOrderItem :one
INSERT INTO order_items (id,tenant_id,order_id,sku_id,product_id,quantity,unit_price,total_price,status,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING *;

-- name: ListOrderItems :many
SELECT * FROM order_items WHERE order_id=$1 AND tenant_id=$2 ORDER BY created_at;

-- name: SumOrderItems :one
SELECT COALESCE(SUM(total_price),0) AS total FROM order_items WHERE order_id=$1 AND tenant_id=$2;

-- name: CreateInvoice :one
INSERT INTO invoices (id,tenant_id,order_id,invoice_number,status,sub_total,tax_amount,total_amount,currency,issued_at,due_at,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING *;

-- name: GetInvoice :one
SELECT * FROM invoices WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL;
