package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/services/feed-service/internal/domain"
)

// ErrNotFound lets a caller tell a missing record from a failed query. Without
// it every outcome reaches the handler as an opaque error and is reported as
// internal, so a client cannot distinguish "no such plan" from "the database is
// unreachable".
var ErrNotFound = errors.New("not found")

// Columns are listed explicitly rather than selected with *, because the scans
// below are positional: adding a column to the table would silently misalign
// every field after it.
const feedTypeCols = `id,tenant_id,name,category,unit,nutritional_info,` +
	`created_at,updated_at,created_by,updated_by,deleted_at`

const nutritionPlanCols = `id,tenant_id,cattle_id,feed_type_id,daily_quantity_kg,start_date,end_date,notes,` +
	`created_at,updated_at,created_by,updated_by,deleted_at`

const feedConsumptionCols = `id,tenant_id,cattle_id,feed_type_id,quantity_kg,fed_at,fed_by,` +
	`created_at,updated_at,created_by,updated_by`

type Repository interface {
	CreateFeedType(ctx context.Context, f *domain.FeedType) (*domain.FeedType, error)
	GetFeedType(ctx context.Context, id, tenantID string) (*domain.FeedType, error)
	ListFeedTypes(ctx context.Context, tenantID string) ([]*domain.FeedType, error)
	CreateNutritionPlan(ctx context.Context, p *domain.NutritionPlan) (*domain.NutritionPlan, error)
	GetNutritionPlan(ctx context.Context, id, tenantID string) (*domain.NutritionPlan, error)
	ListNutritionPlans(ctx context.Context, tenantID, cattleID string) ([]*domain.NutritionPlan, error)
	CreateFeedConsumption(ctx context.Context, c *domain.FeedConsumption) (*domain.FeedConsumption, error)
	GetFeedConsumptionReport(ctx context.Context, tenantID, cattleID string, from, to time.Time) ([]*domain.FeedConsumptionReport, error)
}

type repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) Repository {
	return &repo{pool: pool}
}

type scanner interface {
	Scan(dest ...any) error
}

func (r *repo) CreateFeedType(ctx context.Context, f *domain.FeedType) (*domain.FeedType, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO feed_types (id,tenant_id,name,category,unit,nutritional_info,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING `+feedTypeCols,
		f.ID, f.TenantID, f.Name, f.Category, f.Unit, f.NutritionalInfo, f.CreatedBy, f.UpdatedBy,
	)
	return scanFeedType(row)
}

func (r *repo) GetFeedType(ctx context.Context, id, tenantID string) (*domain.FeedType, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+feedTypeCols+` FROM feed_types WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanFeedType(row)
}

func (r *repo) ListFeedTypes(ctx context.Context, tenantID string) ([]*domain.FeedType, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+feedTypeCols+` FROM feed_types WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.FeedType
	for rows.Next() {
		f, err := scanFeedType(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, f)
	}
	return result, rows.Err()
}

func (r *repo) CreateNutritionPlan(ctx context.Context, p *domain.NutritionPlan) (*domain.NutritionPlan, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO nutrition_plans (id,tenant_id,cattle_id,feed_type_id,daily_quantity_kg,start_date,end_date,notes,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING `+nutritionPlanCols,
		p.ID, p.TenantID, p.CattleID, p.FeedTypeID, p.DailyQuantityKg, p.StartDate, p.EndDate,
		p.Notes, p.CreatedBy, p.UpdatedBy,
	)
	return scanNutritionPlan(row)
}

func (r *repo) GetNutritionPlan(ctx context.Context, id, tenantID string) (*domain.NutritionPlan, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+nutritionPlanCols+` FROM nutrition_plans WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanNutritionPlan(row)
}

func (r *repo) ListNutritionPlans(ctx context.Context, tenantID, cattleID string) ([]*domain.NutritionPlan, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+nutritionPlanCols+` FROM nutrition_plans WHERE tenant_id=$1 AND cattle_id=$2 AND deleted_at IS NULL ORDER BY start_date DESC`,
		tenantID, cattleID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.NutritionPlan
	for rows.Next() {
		p, err := scanNutritionPlan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (r *repo) CreateFeedConsumption(ctx context.Context, c *domain.FeedConsumption) (*domain.FeedConsumption, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO feed_consumption (id,tenant_id,cattle_id,feed_type_id,quantity_kg,fed_at,fed_by,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+feedConsumptionCols,
		c.ID, c.TenantID, c.CattleID, c.FeedTypeID, c.QuantityKg, c.FedAt, c.FedBy, c.CreatedBy, c.UpdatedBy,
	)
	return scanFeedConsumption(row)
}

func (r *repo) GetFeedConsumptionReport(ctx context.Context, tenantID, cattleID string, from, to time.Time) ([]*domain.FeedConsumptionReport, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT cattle_id, feed_type_id, SUM(quantity_kg) AS total_kg
		 FROM feed_consumption
		 WHERE tenant_id=$1 AND cattle_id=$2 AND fed_at BETWEEN $3 AND $4
		 GROUP BY cattle_id, feed_type_id`,
		tenantID, cattleID, from, to,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.FeedConsumptionReport
	for rows.Next() {
		rep := &domain.FeedConsumptionReport{}
		if err := rows.Scan(&rep.CattleID, &rep.FeedTypeID, &rep.TotalKg); err != nil {
			return nil, err
		}
		result = append(result, rep)
	}
	return result, rows.Err()
}

func scanFeedType(s scanner) (*domain.FeedType, error) {
	f := &domain.FeedType{}
	err := s.Scan(&f.ID, &f.TenantID, &f.Name, &f.Category, &f.Unit, &f.NutritionalInfo,
		&f.CreatedAt, &f.UpdatedAt, &f.CreatedBy, &f.UpdatedBy, &f.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return f, nil
}

func scanNutritionPlan(s scanner) (*domain.NutritionPlan, error) {
	p := &domain.NutritionPlan{}
	err := s.Scan(&p.ID, &p.TenantID, &p.CattleID, &p.FeedTypeID, &p.DailyQuantityKg,
		&p.StartDate, &p.EndDate, &p.Notes, &p.CreatedAt, &p.UpdatedAt, &p.CreatedBy, &p.UpdatedBy, &p.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return p, nil
}

func scanFeedConsumption(s scanner) (*domain.FeedConsumption, error) {
	c := &domain.FeedConsumption{}
	err := s.Scan(&c.ID, &c.TenantID, &c.CattleID, &c.FeedTypeID, &c.QuantityKg,
		&c.FedAt, &c.FedBy, &c.CreatedAt, &c.UpdatedAt, &c.CreatedBy, &c.UpdatedBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return c, nil
}
