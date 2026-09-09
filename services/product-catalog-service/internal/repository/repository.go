package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/audit"
	"github.com/ppusapati/gavya/libs/integrity/currency"
	"github.com/ppusapati/gavya/libs/integrity/money"

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

// ErrCurrencyChange is a price update that would put a SKU into a different
// currency than the one it was created in. Refused rather than applied: every
// invoice raised against that SKU was denominated in the old one, and there is
// nothing in the record that would say when the meaning of the number changed.
var ErrCurrencyChange = errors.New("a SKU's currency cannot be changed by repricing it")

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
	UpdateSKUPrice(ctx context.Context, id, tenantID string, price money.Money, updatedBy string) (*domain.SKU, error)
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
		// The price goes down as the decimal literal the caller asked for, cast
		// to the column's type by PostgreSQL. Nothing between the request and
		// the column is a float.
		`INSERT INTO skus (id,tenant_id,product_id,code,name,price,currency,unit,unit_size,status,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6::numeric,$7,$8,$9,$10,$11,$12) RETURNING `+skuCols,
		s.ID, s.TenantID, s.ProductID, s.Code, s.Name, s.Price.String(), s.Price.Currency,
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
// The price arrives as money, and the currency it carries has to be the one the
// row is already in. A price cannot change currency: that would restate what a
// past invoice was denominated in, silently, as a side effect of a price edit.
func (r *repo) UpdateSKUPrice(ctx context.Context, id, tenantID string, price money.Money, updatedBy string) (*domain.SKU, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	// The old price, read under the lock the update will hold, so a concurrent
	// change cannot land between the read and the write and be recorded as
	// though it had not happened.
	var beforeLiteral, currencyCode string
	if err := tx.QueryRow(ctx,
		`SELECT price, COALESCE(currency,'') FROM skus
		  WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL FOR UPDATE`,
		id, tenantID).Scan(&beforeLiteral, &currencyCode); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	before, err := parsePrice(beforeLiteral, currencyCode)
	if err != nil {
		return nil, fmt.Errorf("sku %s: %w", id, err)
	}
	if before.Currency != price.Currency {
		return nil, fmt.Errorf("%w: this SKU is priced in %s, not %s",
			ErrCurrencyChange, before.Currency, price.Currency)
	}

	row := tx.QueryRow(ctx,
		`UPDATE skus SET price=$3::numeric,updated_by=$4,updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING `+skuCols,
		id, tenantID, price.String(), updatedBy,
	)
	sku, err := scanSKU(row)
	if err != nil {
		return nil, err
	}

	// The trail records the decimal literals, not the floats they used to be
	// read as. An audit entry is the record of what happened; a figure rounded
	// on its way into one is a record of something else.
	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "update_sku_price", ResourceType: "sku", ResourceID: id,
		Before:      map[string]any{"price": before.String(), "currency": before.Currency},
		After:       map[string]any{"price": price.String(), "currency": price.Currency},
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

// scanSKU reads one row, taking the price out of NUMERIC as a decimal literal
// rather than a float64.
//
// The scale is the currency's, not the column's. The column is NUMERIC(18,4) so
// that one schema serves a yen deployment and a dinar one; how many of those
// four decimals are real is a fact about the currency. PostgreSQL hands back
// "0" for a zero NUMERIC(18,4) and "1234.50" for a two-decimal one, so the text
// carries no reliable scale of its own — money.Parse pads a short literal to the
// currency's scale and refuses a long one, which is the behaviour wanted here:
// a stored figure with more precision than its currency has is not something to
// round away quietly.
func scanSKU(s scanner) (*domain.SKU, error) {
	s2 := &domain.SKU{}
	var price, code string
	err := s.Scan(&s2.ID, &s2.TenantID, &s2.ProductID, &s2.Code, &s2.Name, &price, &code,
		&s2.Unit, &s2.UnitSize, &s2.Status, &s2.CreatedAt, &s2.UpdatedAt, &s2.CreatedBy, &s2.UpdatedBy, &s2.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	s2.Price, err = parsePrice(price, code)
	if err != nil {
		return nil, fmt.Errorf("sku %s: %w", s2.ID, err)
	}
	return s2, nil
}

// moneyColumnScale is how many decimals the money columns hold.
//
// They are NUMERIC(18,4) so that one schema serves a yen deployment and a dinar
// one: four is the most any ISO 4217 currency has. It is not how many decimals
// any particular amount has — a rupee price stored there reads back as
// "42.5000", and those last two zeros are the column's padding, not precision.
const moneyColumnScale int32 = 4

// parsePrice turns a stored decimal literal into money at its currency's scale.
//
// Two steps, deliberately. The literal is read at the column's scale, because
// that is what PostgreSQL renders; then it is brought down to the currency's,
// because that is how many decimals the amount actually has. The second step
// must throw nothing away — a rupee price with a non-zero third decimal is a
// figure the currency cannot express, and something has gone wrong upstream if
// one is there. Reporting it beats rounding it away, which would make the
// service quietly disagree with its own database.
//
// The rounding mode is named but never used: the call is refused unless it
// discarded nothing, so no rounding decision is being made here. It is a
// decomposition of the column's padding, not a choice about money.
//
// A row whose currency is unreadable is an error rather than a zero: a price
// with no currency is not a price, and returning one as though it were is how a
// rupee figure ends up being read as dollars.
func parsePrice(literal, code string) (money.Money, error) {
	normalised, err := currency.Normalise(code)
	if err != nil {
		return money.Money{}, fmt.Errorf("currency %q: %w", code, err)
	}
	scale, err := currency.Scale(normalised)
	if err != nil {
		return money.Money{}, fmt.Errorf("currency %q: %w", code, err)
	}
	stored, err := money.Parse(literal, moneyColumnScale, normalised)
	if err != nil {
		return money.Money{}, fmt.Errorf("price %q: %w", literal, err)
	}
	m, step, err := money.Rescale(stored, scale, money.RoundTowardZero)
	if err != nil {
		return money.Money{}, fmt.Errorf("price %q: %w", literal, err)
	}
	if step.Discarded != 0 {
		return money.Money{}, fmt.Errorf(
			"price %q is stored with more precision than %s has: %d beyond %d decimals would "+
				"have to be dropped to read it, and dropping it silently would make this "+
				"service disagree with its own database",
			literal, normalised, step.Discarded, scale)
	}
	return m, nil
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
