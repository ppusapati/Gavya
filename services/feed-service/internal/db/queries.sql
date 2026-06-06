-- name: CreateFeedType :one
INSERT INTO feed_types (id,tenant_id,name,category,unit,nutritional_info,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING *;

-- name: GetFeedType :one
SELECT * FROM feed_types WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL;

-- name: ListFeedTypes :many
SELECT * FROM feed_types WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY name;

-- name: CreateNutritionPlan :one
INSERT INTO nutrition_plans (id,tenant_id,cattle_id,feed_type_id,daily_quantity_kg,start_date,end_date,notes,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING *;

-- name: GetNutritionPlan :one
SELECT * FROM nutrition_plans WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL;

-- name: ListNutritionPlans :many
SELECT * FROM nutrition_plans WHERE tenant_id=$1 AND cattle_id=$2 AND deleted_at IS NULL ORDER BY start_date DESC;

-- name: CreateFeedConsumption :one
INSERT INTO feed_consumption (id,tenant_id,cattle_id,feed_type_id,quantity_kg,fed_at,fed_by,created_by,updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING *;

-- name: GetFeedConsumptionReport :many
SELECT cattle_id, feed_type_id, SUM(quantity_kg) AS total_kg
FROM feed_consumption
WHERE tenant_id=$1 AND cattle_id=$2 AND fed_at BETWEEN $3 AND $4
GROUP BY cattle_id, feed_type_id;
