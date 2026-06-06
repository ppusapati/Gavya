-- name: CreateCategory :one
INSERT INTO categories (id,tenant_id,name,slug,parent_id,description,sort_order,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING *;

-- name: GetCategory :one
SELECT * FROM categories WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL;

-- name: ListCategories :many
SELECT * FROM categories WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY sort_order, name;

-- name: CreateBrand :one
INSERT INTO brands (id,tenant_id,name,slug,logo_url,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING *;

-- name: GetBrand :one
SELECT * FROM brands WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL;

-- name: ListBrands :many
SELECT * FROM brands WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY name;

-- name: CreateProduct :one
INSERT INTO products (id,tenant_id,category_id,brand_id,name,slug,description,product_type,status,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING *;

-- name: GetProduct :one
SELECT * FROM products WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL;

-- name: ListProducts :many
SELECT * FROM products WHERE tenant_id=$1 AND product_type=$2 AND status=$3 AND deleted_at IS NULL ORDER BY name;

-- name: CreateSKU :one
INSERT INTO skus (id,tenant_id,product_id,code,name,price,currency,unit,unit_size,status,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING *;

-- name: GetSKU :one
SELECT * FROM skus WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL;

-- name: ListProductSKUs :many
SELECT * FROM skus WHERE product_id=$1 AND tenant_id=$2 AND deleted_at IS NULL ORDER BY name;

-- name: UpdateSKUPrice :one
UPDATE skus SET price=$3,updated_by=$4,updated_at=NOW()
WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING *;
