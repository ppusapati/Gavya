package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ppusapati/gavya/services/product-catalog-service/internal/domain"
)

type Repository interface {
	CreateCategory(ctx context.Context, c *domain.Category) (*domain.Category, error)
	GetCategory(ctx context.Context, id, tenantID string) (*domain.Category, error)
	ListCategories(ctx context.Context, tenantID string) ([]*domain.Category, error)
	CreateBrand(ctx context.Context, b *domain.Brand) (*domain.Brand, error)
	GetBrand(ctx context.Context, id, tenantID string) (*domain.Brand, error)
	ListBrands(ctx context.Context, tenantID string) ([]*domain.Brand, error)
	CreateProduct(ctx context.Context, p *domain.Product) (*domain.Product, error)
	GetProduct(ctx context.Context, id, tenantID string) (*domain.Product, error)
	ListProducts(ctx context.Context, tenantID, productType, status string) ([]*domain.Product, error)
	CreateSKU(ctx context.Context, s *domain.SKU) (*domain.SKU, error)
	GetSKU(ctx context.Context, id, tenantID string) (*domain.SKU, error)
	ListProductSKUs(ctx context.Context, productID, tenantID string) ([]*domain.SKU, error)
	UpdateSKUPrice(ctx context.Context, id, tenantID string, price float64, updatedBy string) (*domain.SKU, error)
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

func (r *repo) CreateCategory(ctx context.Context, c *domain.Category) (*domain.Category, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO categories (id,tenant_id,name,slug,parent_id,description,sort_order,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING *`,
		c.ID, c.TenantID, c.Name, c.Slug, c.ParentID, c.Description, c.SortOrder, c.CreatedBy, c.UpdatedBy,
	)
	return scanCategory(row)
}

func (r *repo) GetCategory(ctx context.Context, id, tenantID string) (*domain.Category, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT * FROM categories WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanCategory(row)
}

func (r *repo) ListCategories(ctx context.Context, tenantID string) ([]*domain.Category, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT * FROM categories WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY sort_order, name`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Category
	for rows.Next() {
		c := &domain.Category{}
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Name, &c.Slug, &c.ParentID, &c.Description, &c.SortOrder,
			&c.CreatedAt, &c.UpdatedAt, &c.CreatedBy, &c.UpdatedBy, &c.DeletedAt); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func (r *repo) CreateBrand(ctx context.Context, b *domain.Brand) (*domain.Brand, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO brands (id,tenant_id,name,slug,logo_url,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING *`,
		b.ID, b.TenantID, b.Name, b.Slug, b.LogoURL, b.CreatedBy, b.UpdatedBy,
	)
	return scanBrand(row)
}

func (r *repo) GetBrand(ctx context.Context, id, tenantID string) (*domain.Brand, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT * FROM brands WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanBrand(row)
}

func (r *repo) ListBrands(ctx context.Context, tenantID string) ([]*domain.Brand, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT * FROM brands WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Brand
	for rows.Next() {
		b := &domain.Brand{}
		if err := rows.Scan(&b.ID, &b.TenantID, &b.Name, &b.Slug, &b.LogoURL,
			&b.CreatedAt, &b.UpdatedAt, &b.CreatedBy, &b.UpdatedBy, &b.DeletedAt); err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

func (r *repo) CreateProduct(ctx context.Context, p *domain.Product) (*domain.Product, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO products (id,tenant_id,category_id,brand_id,name,slug,description,product_type,status,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING *`,
		p.ID, p.TenantID, p.CategoryID, p.BrandID, p.Name, p.Slug, p.Description,
		p.ProductType, p.Status, p.CreatedBy, p.UpdatedBy,
	)
	return scanProduct(row)
}

func (r *repo) GetProduct(ctx context.Context, id, tenantID string) (*domain.Product, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT * FROM products WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanProduct(row)
}

func (r *repo) ListProducts(ctx context.Context, tenantID, productType, status string) ([]*domain.Product, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT * FROM products WHERE tenant_id=$1 AND product_type=$2 AND status=$3 AND deleted_at IS NULL ORDER BY name`,
		tenantID, productType, status,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Product
	for rows.Next() {
		p := &domain.Product{}
		if err := rows.Scan(&p.ID, &p.TenantID, &p.CategoryID, &p.BrandID, &p.Name, &p.Slug, &p.Description,
			&p.ProductType, &p.Status, &p.CreatedAt, &p.UpdatedAt, &p.CreatedBy, &p.UpdatedBy, &p.DeletedAt); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (r *repo) CreateSKU(ctx context.Context, s *domain.SKU) (*domain.SKU, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO skus (id,tenant_id,product_id,code,name,price,currency,unit,unit_size,status,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING *`,
		s.ID, s.TenantID, s.ProductID, s.Code, s.Name, s.Price, s.Currency,
		s.Unit, s.UnitSize, s.Status, s.CreatedBy, s.UpdatedBy,
	)
	return scanSKU(row)
}

func (r *repo) GetSKU(ctx context.Context, id, tenantID string) (*domain.SKU, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT * FROM skus WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanSKU(row)
}

func (r *repo) ListProductSKUs(ctx context.Context, productID, tenantID string) ([]*domain.SKU, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT * FROM skus WHERE product_id=$1 AND tenant_id=$2 AND deleted_at IS NULL ORDER BY name`,
		productID, tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.SKU
	for rows.Next() {
		s := &domain.SKU{}
		if err := rows.Scan(&s.ID, &s.TenantID, &s.ProductID, &s.Code, &s.Name, &s.Price, &s.Currency,
			&s.Unit, &s.UnitSize, &s.Status, &s.CreatedAt, &s.UpdatedAt, &s.CreatedBy, &s.UpdatedBy, &s.DeletedAt); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func (r *repo) UpdateSKUPrice(ctx context.Context, id, tenantID string, price float64, updatedBy string) (*domain.SKU, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE skus SET price=$3,updated_by=$4,updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING *`,
		id, tenantID, price, updatedBy,
	)
	return scanSKU(row)
}

func scanCategory(s scanner) (*domain.Category, error) {
	c := &domain.Category{}
	err := s.Scan(&c.ID, &c.TenantID, &c.Name, &c.Slug, &c.ParentID, &c.Description, &c.SortOrder,
		&c.CreatedAt, &c.UpdatedAt, &c.CreatedBy, &c.UpdatedBy, &c.DeletedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func scanBrand(s scanner) (*domain.Brand, error) {
	b := &domain.Brand{}
	err := s.Scan(&b.ID, &b.TenantID, &b.Name, &b.Slug, &b.LogoURL,
		&b.CreatedAt, &b.UpdatedAt, &b.CreatedBy, &b.UpdatedBy, &b.DeletedAt)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func scanProduct(s scanner) (*domain.Product, error) {
	p := &domain.Product{}
	err := s.Scan(&p.ID, &p.TenantID, &p.CategoryID, &p.BrandID, &p.Name, &p.Slug, &p.Description,
		&p.ProductType, &p.Status, &p.CreatedAt, &p.UpdatedAt, &p.CreatedBy, &p.UpdatedBy, &p.DeletedAt)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func scanSKU(s scanner) (*domain.SKU, error) {
	s2 := &domain.SKU{}
	err := s.Scan(&s2.ID, &s2.TenantID, &s2.ProductID, &s2.Code, &s2.Name, &s2.Price, &s2.Currency,
		&s2.Unit, &s2.UnitSize, &s2.Status, &s2.CreatedAt, &s2.UpdatedAt, &s2.CreatedBy, &s2.UpdatedBy, &s2.DeletedAt)
	if err != nil {
		return nil, err
	}
	return s2, nil
}
