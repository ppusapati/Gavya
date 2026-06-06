-- name: CreateListing :one
INSERT INTO cattle_listings (id, tenant_id, cattle_id, seller_id, title, description, asking_price, currency, listing_type, status, expires_at, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING *;

-- name: GetListing :one
SELECT * FROM cattle_listings
WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL;

-- name: ListActiveListings :many
SELECT * FROM cattle_listings
WHERE tenant_id = $1 AND status = 'active' AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: UpdateListingStatus :one
UPDATE cattle_listings
SET status = $3, updated_by = $4, updated_at = NOW()
WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL
RETURNING *;

-- name: CreateBid :one
INSERT INTO cattle_bids (id, tenant_id, listing_id, bidder_id, bid_amount, currency, status, message, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: ListListingBids :many
SELECT * FROM cattle_bids
WHERE listing_id = $1 AND deleted_at IS NULL
ORDER BY bid_amount DESC;

-- name: UpdateBidStatus :one
UPDATE cattle_bids
SET status = $2, updated_by = $3, updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL
RETURNING *;

-- name: CreateSale :one
INSERT INTO cattle_sales (id, tenant_id, listing_id, seller_id, buyer_id, cattle_id, sale_price, currency, sale_date, status, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetSale :one
SELECT * FROM cattle_sales
WHERE id = $1 AND tenant_id = $2 AND deleted_at IS NULL;

-- name: CreateOwnership :one
INSERT INTO cattle_ownership (id, tenant_id, cattle_id, owner_id, acquired_at, acquisition_type, sale_id, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListCattleOwnership :many
SELECT * FROM cattle_ownership
WHERE tenant_id = $1 AND cattle_id = $2
ORDER BY acquired_at DESC;
