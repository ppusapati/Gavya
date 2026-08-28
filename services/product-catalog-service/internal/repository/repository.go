package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/audit"

	"github.com/ppusapati/gavya/services/product-catalog-service/internal/domain"
)

// ErrNotFound lets a caller tell a missing record from a failed query. Without
// it every outcome reaches the handler as an opaque error and is reported as
// internal, so a client cannot distinguish "no such product" from "the database
// is unreachable".
var ErrNotFound = errors.New("not found")

// ErrUnknownReference is a foreign key the caller supplied that does not resolve
// — a category, brand or product that is not there. Without it the constraint
// violation reaches the handler as an opaque error and is reported as internal,
// which tells a client to retry a call that will never succeed.
var ErrUnknownReference = errors.New("a referenced category, brand or product does not exist")

// The unique violations, named so the handler can report a conflict rather than
// an internal failure.
var (
	ErrDuplicateCategorySlug = errors.New("a category with that slug already exists")
	ErrDuplicateBrandSlug    = errors.New("a brand with that slug already exists")
	ErrDuplicateProductSlug  = errors.New("a product with that slug already exists")
	ErrDuplicateSKUCode      = errors.New("a sku with that code already exists")
)

// Columns are listed explicitly rather than selected with *, because the scans
// below are positional: adding a column to the table would silently misalign
// every field after it.
const categoryCols = `id,tenant_id,name,slug,parent_id,COALESCE(description,''),COALESCE(sort_order,0),` +
	`created_at,updated_at,created_by,updated_by,deleted_at`

const brandCols = `id,tenant_id,name,slug,COALESCE(logo_url,''),` +
	`created_at,updated_at,created_by,updated_by,deleted_at`

const productCols = `id,tenant_id,COALESCE(category_id,''),COALESCE(brand_id,''),name,slug,COALESCE(description,''),product_type,status,` +
	`created_at,updated_at,created_by,updated_by,deleted_at`

const skuCols = `id,tenant_id,product_id,code,name,price,currency,unit,COALESCE(unit_size,0),status,` +
	`created_at,updated_at,created_by,updated_by,deleted_at`

type Repository interface {
	// PinTenantMoney fixes the currency this tenant records money in.
	PinTenantMoney(ctx context.Context, tenantID string, money Money) error
	// TenantMoney reports it.
	TenantMoney(ctx context.Context, tenantID string) (Money, error)

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

// IDs supplies the identifier each audit entry carries.
type IDs interface{ New() string }

type repo struct {
	pool *pgxpool.Pool
	ids  IDs
}

func New(pool *pgxpool.Pool, ids IDs) Repository {
	return &repo{pool: pool, ids: ids}
}

const serviceName = "product-catalog-service"

type scanner interface {
	Scan(dest ...any) error
}

func (r *repo) CreateCategory(ctx context.Context, c *domain.Category) (*domain.Category, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO categories (id,tenant_id,name,slug,parent_id,description,sort_order,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+categoryCols,
		c.ID, c.TenantID, c.Name, c.Slug, c.ParentID, c.Description, c.SortOrder, c.CreatedBy, c.UpdatedBy,
	)
	out, err := scanCategory(row)
	if isUniqueViolation(err) {
		return nil, ErrDuplicateCategorySlug
	}
	return out, err
}

func (r *repo) GetCategory(ctx context.Context, id, tenantID string) (*domain.Category, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+categoryCols+` FROM categories WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanCategory(row)
}

func (r *repo) ListCategories(ctx context.Context, tenantID string) ([]*domain.Category, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+categoryCols+` FROM categories WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY sort_order, name`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Category
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func (r *repo) CreateBrand(ctx context.Context, b *domain.Brand) (*domain.Brand, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO brands (id,tenant_id,name,slug,logo_url,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING `+brandCols,
		b.ID, b.TenantID, b.Name, b.Slug, b.LogoURL, b.CreatedBy, b.UpdatedBy,
	)
	out, err := scanBrand(row)
	if isUniqueViolation(err) {
		return nil, ErrDuplicateBrandSlug
	}
	return out, err
}

func (r *repo) GetBrand(ctx context.Context, id, tenantID string) (*domain.Brand, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+brandCols+` FROM brands WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanBrand(row)
}

func (r *repo) ListBrands(ctx context.Context, tenantID string) ([]*domain.Brand, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+brandCols+` FROM brands WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Brand
	for rows.Next() {
		b, err := scanBrand(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

func (r *repo) CreateProduct(ctx context.Context, p *domain.Product) (*domain.Product, error) {
	row := r.pool.QueryRow(ctx,
		// Category and brand are optional, and the columns are nullable. An empty
		// string is not the same as no category: it is a foreign key to a row
		// whose id is "", which does not exist. NULLIF turns "none supplied" into
		// the absence the column was designed for.
		`INSERT INTO products (id,tenant_id,category_id,brand_id,name,slug,description,product_type,status,created_by,updated_by)
		 VALUES ($1,$2,NULLIF($3,''),NULLIF($4,''),$5,$6,$7,$8,$9,$10,$11) RETURNING `+productCols,
		p.ID, p.TenantID, p.CategoryID, p.BrandID, p.Name, p.Slug, p.Description,
		p.ProductType, p.Status, p.CreatedBy, p.UpdatedBy,
	)
	out, err := scanProduct(row)
	if isUniqueViolation(err) {
		return nil, ErrDuplicateProductSlug
	}
	if isForeignKeyViolation(err) {
		// The caller named a category or brand that is not there. Reporting that
		// as an internal failure tells them to try again, when what they need to
		// do is fix the id.
		return nil, ErrUnknownReference
	}
	return out, err
}

func (r *repo) GetProduct(ctx context.Context, id, tenantID string) (*domain.Product, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+productCols+` FROM products WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanProduct(row)
}

func (r *repo) ListProducts(ctx context.Context, tenantID, productType, status string) ([]*domain.Product, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+productCols+` FROM products WHERE tenant_id=$1 AND ($2='' OR product_type=$2) AND status=$3 AND deleted_at IS NULL ORDER BY name`,
		tenantID, productType, status,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Product
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (r *repo) CreateSKU(ctx context.Context, s *domain.SKU) (*domain.SKU, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO skus (id,tenant_id,product_id,code,name,price,currency,unit,unit_size,status,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING `+skuCols,
		s.ID, s.TenantID, s.ProductID, s.Code, s.Name, s.Price, s.Currency,
		s.Unit, s.UnitSize, s.Status, s.CreatedBy, s.UpdatedBy,
	)
	out, err := scanSKU(row)
	if isUniqueViolation(err) {
		return nil, ErrDuplicateSKUCode
	}
	if isForeignKeyViolation(err) {
		return nil, ErrUnknownReference
	}
	return out, err
}

func (r *repo) GetSKU(ctx context.Context, id, tenantID string) (*domain.SKU, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+skuCols+` FROM skus WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanSKU(row)
}

func (r *repo) ListProductSKUs(ctx context.Context, productID, tenantID string) ([]*domain.SKU, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+skuCols+` FROM skus WHERE product_id=$1 AND tenant_id=$2 AND deleted_at IS NULL ORDER BY name`,
		productID, tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.SKU
	for rows.Next() {
		s, err := scanSKU(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

// UpdateSKUPrice changes a price and records what it was.
//
// This is the one place in these older services where a money figure is
// overwritten and nothing else holds it. An order total is derived from lines
// that are inserted, so losing the total loses nothing; a SKU's price is the
// primary fact, and an invoice raised before it moved cannot be reconciled
// against a price that no longer exists anywhere.
//
// The audit entry goes in the same transaction as the change, so the two land
// together or not at all. A trail written afterwards is one that is missing
// exactly the changes that crashed halfway.
func (r *repo) UpdateSKUPrice(ctx context.Context, id, tenantID string, price float64, updatedBy string) (*domain.SKU, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	// The old price, read under the lock the update will hold, so a concurrent
	// change cannot land between the read and the write and be recorded as
	// though it had not happened.
	var before float64
	var currencyCode string
	if err := tx.QueryRow(ctx,
		`SELECT price, COALESCE(currency,'') FROM skus
		  WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL FOR UPDATE`,
		id, tenantID).Scan(&before, &currencyCode); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	row := tx.QueryRow(ctx,
		`UPDATE skus SET price=$3,updated_by=$4,updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING `+skuCols,
		id, tenantID, price, updatedBy,
	)
	sku, err := scanSKU(row)
	if err != nil {
		return nil, err
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "update_sku_price", ResourceType: "sku", ResourceID: id,
		Before:      map[string]any{"price": before, "currency": currencyCode},
		After:       map[string]any{"price": price, "currency": currencyCode},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	return sku, tx.Commit(ctx)
}

func scanCategory(s scanner) (*domain.Category, error) {
	c := &domain.Category{}
	err := s.Scan(&c.ID, &c.TenantID, &c.Name, &c.Slug, &c.ParentID, &c.Description, &c.SortOrder,
		&c.CreatedAt, &c.UpdatedAt, &c.CreatedBy, &c.UpdatedBy, &c.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return c, nil
}

func scanBrand(s scanner) (*domain.Brand, error) {
	b := &domain.Brand{}
	err := s.Scan(&b.ID, &b.TenantID, &b.Name, &b.Slug, &b.LogoURL,
		&b.CreatedAt, &b.UpdatedAt, &b.CreatedBy, &b.UpdatedBy, &b.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return b, nil
}

func scanProduct(s scanner) (*domain.Product, error) {
	p := &domain.Product{}
	err := s.Scan(&p.ID, &p.TenantID, &p.CategoryID, &p.BrandID, &p.Name, &p.Slug, &p.Description,
		&p.ProductType, &p.Status, &p.CreatedAt, &p.UpdatedAt, &p.CreatedBy, &p.UpdatedBy, &p.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return p, nil
}

func scanSKU(s scanner) (*domain.SKU, error) {
	s2 := &domain.SKU{}
	err := s.Scan(&s2.ID, &s2.TenantID, &s2.ProductID, &s2.Code, &s2.Name, &s2.Price, &s2.Currency,
		&s2.Unit, &s2.UnitSize, &s2.Status, &s2.CreatedAt, &s2.UpdatedAt, &s2.CreatedBy, &s2.UpdatedBy, &s2.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s2, nil
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
