-- name: CreateInvoice :one
INSERT INTO invoices (id,tenant_id,customer_id,invoice_number,reference_id,reference_type,status,sub_total,tax_amount,total_amount,currency,issued_at,due_at,notes,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING *;

-- name: GetInvoice :one
SELECT * FROM invoices WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL;

-- name: UpdateInvoiceStatus :one
UPDATE invoices SET status=$3,updated_by=$4,updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING *;

-- name: UpdateInvoiceTotals :one
UPDATE invoices SET sub_total=$3,tax_amount=$4,total_amount=$5,updated_by=$6,updated_at=NOW()
WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING *;

-- name: MarkInvoicePaid :one
UPDATE invoices SET status='paid',paid_at=$3,updated_by=$4,updated_at=NOW()
WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING *;

-- name: ListOutstandingInvoices :many
SELECT * FROM invoices WHERE tenant_id=$1 AND status IN ('sent','overdue') AND deleted_at IS NULL ORDER BY due_at;

-- name: CreateInvoiceItem :one
INSERT INTO invoice_items (id,tenant_id,invoice_id,description,quantity,unit_price,total_price,tax_rate,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING *;

-- name: ListInvoiceItems :many
SELECT * FROM invoice_items WHERE invoice_id=$1 AND tenant_id=$2 ORDER BY created_at;

-- name: SumInvoiceItems :one
SELECT COALESCE(SUM(total_price),0) AS total FROM invoice_items WHERE invoice_id=$1 AND tenant_id=$2;

-- name: SumPayments :one
SELECT COALESCE(SUM(amount),0) AS total FROM payments WHERE invoice_id=$1 AND tenant_id=$2;

-- name: CreatePayment :one
INSERT INTO payments (id,tenant_id,invoice_id,amount,currency,payment_method,reference_no,paid_at,notes,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING *;
