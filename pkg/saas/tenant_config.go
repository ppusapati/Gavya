package saas

import "p9e.in/samavaya/packages/saas/data"

// TenantType represents the subscription tier of a tenant
type TenantType string

const (
	// TenantTypeFree uses shared database with tenant_id column isolation
	TenantTypeFree TenantType = "free"
	// TenantTypePaid uses dedicated database per tenant
	TenantTypePaid TenantType = "paid"
)

// TenantConfig holds configuration for a specific tenant
type TenantConfig struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Region   string           `json:"region"`
	Type     TenantType       `json:"type"`
	IsActive bool             `json:"is_active"`
	Conn     data.ConnStrings `json:"conn"`
}

// NewTenantConfig creates a new tenant config (defaults to free tier)
func NewTenantConfig(id string, name string, region string) *TenantConfig {
	return &TenantConfig{
		ID:       id,
		Name:     name,
		Region:   region,
		Type:     TenantTypeFree,
		IsActive: true,
		Conn:     make(data.ConnStrings),
	}
}

// NewPaidTenantConfig creates a new paid tenant config with dedicated database
func NewPaidTenantConfig(id string, name string, region string, dbConnStr string) *TenantConfig {
	tc := &TenantConfig{
		ID:       id,
		Name:     name,
		Region:   region,
		Type:     TenantTypePaid,
		IsActive: true,
		Conn:     make(data.ConnStrings),
	}
	tc.Conn.SetDefault(dbConnStr)
	return tc
}

// IsFree returns true if tenant is on free tier (shared database)
func (tc *TenantConfig) IsFree() bool {
	return tc.Type == TenantTypeFree || tc.Type == ""
}

// IsPaid returns true if tenant is on paid tier (dedicated database)
func (tc *TenantConfig) IsPaid() bool {
	return tc.Type == TenantTypePaid
}

// RequiresTenantFilter returns true if queries need tenant_id WHERE clause
// Note: We always return true for defense in depth (even paid tier has tenant_id)
func (tc *TenantConfig) RequiresTenantFilter() bool {
	return true
}
