package service

import (
	"context"
	"errors"
	"strings"

	"github.com/ppusapati/gavya/libs/integrity/currency"
	"github.com/ppusapati/gavya/libs/integrity/exact"
	"github.com/ppusapati/gavya/services/product-catalog-service/internal/domain"
	"github.com/ppusapati/gavya/services/product-catalog-service/internal/repository"
	ulidpkg "p9e.in/samavaya/packages/ulid"
)

// ErrInvalidArgument marks a caller mistake. Without it the handler cannot tell
// "you did not supply an id" from "the query failed", and would have to report
// both the same way.
var ErrInvalidArgument = errors.New("invalid argument")

// invalidArgument carries the reason alone. The marker is matched through Is,
// so errors.Is finds it while the message stays free of a prefix the error code
// already conveys.
type invalidArgument struct{ reason string }

func (e *invalidArgument) Error() string { return e.reason }

func (e *invalidArgument) Is(target error) bool { return target == ErrInvalidArgument }

func invalid(msg string) error { return &invalidArgument{reason: msg} }

func slugify(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, " ", "-"))
}

func (s *Service) CreateCategory(ctx context.Context, c *domain.Category) (*domain.Category, error) {
	if c.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if c.Name == "" {
		return nil, invalid("name is required")
	}
	c.ID = ulidpkg.New().String()
	if c.Slug == "" {
		c.Slug = slugify(c.Name)
	}
	if c.CreatedBy == "" {
		c.CreatedBy = "system"
	}
	c.UpdatedBy = c.CreatedBy
	return s.repo.CreateCategory(ctx, c)
}

func (s *Service) ListCategories(ctx context.Context, tenantID string) ([]*domain.Category, error) {
	if tenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	return s.repo.ListCategories(ctx, tenantID)
}

func (s *Service) CreateBrand(ctx context.Context, b *domain.Brand) (*domain.Brand, error) {
	if b.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if b.Name == "" {
		return nil, invalid("name is required")
	}
	b.ID = ulidpkg.New().String()
	if b.Slug == "" {
		b.Slug = slugify(b.Name)
	}
	if b.CreatedBy == "" {
		b.CreatedBy = "system"
	}
	b.UpdatedBy = b.CreatedBy
	return s.repo.CreateBrand(ctx, b)
}

func (s *Service) ListBrands(ctx context.Context, tenantID string) ([]*domain.Brand, error) {
	if tenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	return s.repo.ListBrands(ctx, tenantID)
}

func (s *Service) CreateProduct(ctx context.Context, p *domain.Product) (*domain.Product, error) {
	if p.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if p.Name == "" {
		return nil, invalid("name is required")
	}
	if p.ProductType == "" {
		return nil, invalid("product_type is required")
	}
	p.ID = ulidpkg.New().String()
	if p.Slug == "" {
		p.Slug = slugify(p.Name)
	}
	if p.Status == "" {
		p.Status = "active"
	}
	if p.CreatedBy == "" {
		p.CreatedBy = "system"
	}
	p.UpdatedBy = p.CreatedBy
	return s.repo.CreateProduct(ctx, p)
}

func (s *Service) GetProduct(ctx context.Context, id, tenantID string) (*domain.Product, error) {
	if id == "" || tenantID == "" {
		return nil, invalid("id and tenant_id are required")
	}
	return s.repo.GetProduct(ctx, id, tenantID)
}

func (s *Service) ListProducts(ctx context.Context, tenantID, productType, status string) ([]*domain.Product, error) {
	if tenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	// An empty product type means "any", and the repository expresses that as an
	// unfiltered column. It used to substitute a % wildcard, which the query
	// compared with = rather than LIKE, so listing every product matched the
	// literal string "%" and always came back empty.
	if status == "" {
		status = "active"
	}
	return s.repo.ListProducts(ctx, tenantID, productType, status)
}

func (s *Service) CreateSKU(ctx context.Context, sku *domain.SKU) (*domain.SKU, error) {
	// unit_size is stored as NUMERIC(10,3). A finer value would be rounded
	// into the column without anyone being told, so it is refused instead.
	if _, err := exact.NonNegativeDecimal(sku.UnitSize, 3, 10); err != nil {
		return nil, invalid(exact.Field("unit_size", err).Error())
	}
	if sku.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if sku.ProductID == "" {
		return nil, invalid("product_id is required")
	}
	if sku.Code == "" {
		return nil, invalid("code is required")
	}
	// The currency is stated, not assumed. There is no default: a price silently
	// recorded in rupees outside India is a price nobody can act on. The first
	// amount a tenant records fixes the currency it records in.
	code, err := currency.Normalise(sku.Currency)
	if err != nil {
		return nil, invalid("currency: " + err.Error())
	}
	scale, err := currency.Scale(code)
	if err != nil {
		return nil, invalid("currency: " + err.Error())
	}
	if err := s.repo.PinTenantMoney(ctx, sku.TenantID, repository.Money{Code: code, Scale: scale}); err != nil {
		return nil, err
	}
	sku.Currency = code
	// Amounts are held to that currency's own precision: a yen price has no
	// decimals, a dinar price has three.
	if _, err := exact.NonNegativeDecimal(sku.Price, scale, exact.MoneyPrecision); err != nil {
		return nil, invalid(exact.Field("price", err).Error())
	}

	sku.ID = ulidpkg.New().String()
	if sku.Status == "" {
		sku.Status = "active"
	}
	if sku.CreatedBy == "" {
		sku.CreatedBy = "system"
	}
	sku.UpdatedBy = sku.CreatedBy
	return s.repo.CreateSKU(ctx, sku)
}

func (s *Service) GetSKU(ctx context.Context, id, tenantID string) (*domain.SKU, error) {
	if id == "" || tenantID == "" {
		return nil, invalid("id and tenant_id are required")
	}
	return s.repo.GetSKU(ctx, id, tenantID)
}

func (s *Service) ListProductSKUs(ctx context.Context, productID, tenantID string) ([]*domain.SKU, error) {
	if productID == "" || tenantID == "" {
		return nil, invalid("product_id and tenant_id are required")
	}
	return s.repo.ListProductSKUs(ctx, productID, tenantID)
}

func (s *Service) UpdateSKUPrice(ctx context.Context, id, tenantID string, price float64, updatedBy string) (*domain.SKU, error) {
	// The same column, reached by a different path. Validating only on create
	// would leave a price that cannot be stored exactly one update away.
	if _, err := exact.NonNegativeDecimal(price, 2, 12); err != nil {
		return nil, invalid(exact.Field("price", err).Error())
	}
	if id == "" || tenantID == "" {
		return nil, invalid("id and tenant_id are required")
	}
	if price < 0 {
		return nil, invalid("price must be non-negative")
	}
	if updatedBy == "" {
		updatedBy = "system"
	}
	return s.repo.UpdateSKUPrice(ctx, id, tenantID, price, updatedBy)
}
