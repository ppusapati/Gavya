package service

import (
	"context"
	"errors"
	"strings"

	"github.com/ppusapati/gavya/services/product-catalog-service/internal/domain"
	ulidpkg "p9e.in/samavaya/packages/ULID"
)

func slugify(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, " ", "-"))
}

func (s *Service) CreateCategory(ctx context.Context, c *domain.Category) (*domain.Category, error) {
	if c.TenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if c.Name == "" {
		return nil, errors.New("name is required")
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
		return nil, errors.New("tenant_id is required")
	}
	return s.repo.ListCategories(ctx, tenantID)
}

func (s *Service) CreateBrand(ctx context.Context, b *domain.Brand) (*domain.Brand, error) {
	if b.TenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if b.Name == "" {
		return nil, errors.New("name is required")
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
		return nil, errors.New("tenant_id is required")
	}
	return s.repo.ListBrands(ctx, tenantID)
}

func (s *Service) CreateProduct(ctx context.Context, p *domain.Product) (*domain.Product, error) {
	if p.TenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if p.Name == "" {
		return nil, errors.New("name is required")
	}
	if p.ProductType == "" {
		return nil, errors.New("product_type is required")
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
		return nil, errors.New("id and tenant_id are required")
	}
	return s.repo.GetProduct(ctx, id, tenantID)
}

func (s *Service) ListProducts(ctx context.Context, tenantID, productType, status string) ([]*domain.Product, error) {
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if productType == "" {
		productType = "%"
	}
	if status == "" {
		status = "active"
	}
	return s.repo.ListProducts(ctx, tenantID, productType, status)
}

func (s *Service) CreateSKU(ctx context.Context, sku *domain.SKU) (*domain.SKU, error) {
	if sku.TenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if sku.ProductID == "" {
		return nil, errors.New("product_id is required")
	}
	if sku.Code == "" {
		return nil, errors.New("code is required")
	}
	sku.ID = ulidpkg.New().String()
	if sku.Currency == "" {
		sku.Currency = "INR"
	}
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
		return nil, errors.New("id and tenant_id are required")
	}
	return s.repo.GetSKU(ctx, id, tenantID)
}

func (s *Service) ListProductSKUs(ctx context.Context, productID, tenantID string) ([]*domain.SKU, error) {
	if productID == "" || tenantID == "" {
		return nil, errors.New("product_id and tenant_id are required")
	}
	return s.repo.ListProductSKUs(ctx, productID, tenantID)
}

func (s *Service) UpdateSKUPrice(ctx context.Context, id, tenantID string, price float64, updatedBy string) (*domain.SKU, error) {
	if id == "" || tenantID == "" {
		return nil, errors.New("id and tenant_id are required")
	}
	if price < 0 {
		return nil, errors.New("price must be non-negative")
	}
	if updatedBy == "" {
		updatedBy = "system"
	}
	return s.repo.UpdateSKUPrice(ctx, id, tenantID, price, updatedBy)
}
