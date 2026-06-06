package service

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/ppusapati/gavya/services/tenant-service/internal/domain"
	ulidpkg "p9e.in/samavaya/packages/ULID"
)

var nonAlphanumHyphen = regexp.MustCompile(`[^a-z0-9-]`)

func generateSlug(name string) string {
	slug := strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	slug = nonAlphanumHyphen.ReplaceAllString(slug, "")
	return slug
}

func (s *Service) CreateTenant(ctx context.Context, t *domain.Tenant) (*domain.Tenant, error) {
	if t.ContactEmail == "" {
		return nil, errors.New("contact_email is required")
	}
	if t.Name == "" {
		return nil, errors.New("name is required")
	}
	t.ID = ulidpkg.New().String()
	if t.Slug == "" {
		t.Slug = generateSlug(t.Name)
	}
	if t.Plan == "" {
		t.Plan = "free"
	}
	if t.Status == "" {
		t.Status = "pending"
	}
	if t.Timezone == "" {
		t.Timezone = "UTC"
	}
	if t.Currency == "" {
		t.Currency = "INR"
	}
	if t.MaxUsers == 0 {
		t.MaxUsers = 5
	}
	if t.MaxCattle == 0 {
		t.MaxCattle = 100
	}
	if t.CreatedBy == "" {
		t.CreatedBy = "system"
	}
	t.UpdatedBy = t.CreatedBy
	return s.repo.CreateTenant(ctx, t)
}

func (s *Service) GetTenant(ctx context.Context, id string) (*domain.Tenant, error) {
	if id == "" {
		return nil, errors.New("id is required")
	}
	return s.repo.GetTenant(ctx, id)
}

func (s *Service) ListTenants(ctx context.Context) ([]*domain.Tenant, error) {
	return s.repo.ListTenants(ctx)
}

func (s *Service) UpdateTenant(ctx context.Context, t *domain.Tenant) (*domain.Tenant, error) {
	if t.ID == "" {
		return nil, errors.New("id is required")
	}
	if t.UpdatedBy == "" {
		t.UpdatedBy = "system"
	}
	return s.repo.UpdateTenant(ctx, t)
}

func (s *Service) SuspendTenant(ctx context.Context, id, updatedBy string) (*domain.Tenant, error) {
	if id == "" {
		return nil, errors.New("id is required")
	}
	if updatedBy == "" {
		updatedBy = "system"
	}
	return s.repo.UpdateTenantStatus(ctx, id, "suspended", updatedBy)
}

func (s *Service) ActivateTenant(ctx context.Context, id, updatedBy string) (*domain.Tenant, error) {
	if id == "" {
		return nil, errors.New("id is required")
	}
	if updatedBy == "" {
		updatedBy = "system"
	}
	return s.repo.UpdateTenantStatus(ctx, id, "active", updatedBy)
}

func (s *Service) UpsertTenantSetting(ctx context.Context, setting *domain.TenantSetting) (*domain.TenantSetting, error) {
	if setting.TenantID == "" || setting.Key == "" {
		return nil, errors.New("tenant_id and key are required")
	}
	setting.ID = ulidpkg.New().String()
	if setting.DataType == "" {
		setting.DataType = "string"
	}
	if setting.CreatedBy == "" {
		setting.CreatedBy = "system"
	}
	setting.UpdatedBy = setting.CreatedBy
	return s.repo.UpsertTenantSetting(ctx, setting)
}

func (s *Service) ListTenantSettings(ctx context.Context, tenantID string) ([]*domain.TenantSetting, error) {
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	return s.repo.ListTenantSettings(ctx, tenantID)
}
