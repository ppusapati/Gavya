package domain

import "time"

type Category struct {
	ID          string
	TenantID    string
	Name        string
	Slug        string
	ParentID    *string
	Description string
	SortOrder   int
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CreatedBy   string
	UpdatedBy   string
	DeletedAt   *time.Time
}

type Brand struct {
	ID        string
	TenantID  string
	Name      string
	Slug      string
	LogoURL   string
	CreatedAt time.Time
	UpdatedAt time.Time
	CreatedBy string
	UpdatedBy string
	DeletedAt *time.Time
}

type Product struct {
	ID          string
	TenantID    string
	CategoryID  string
	BrandID     string
	Name        string
	Slug        string
	Description string
	ProductType string // feed/medicine/supplement/accessory/equipment
	Status      string // active/inactive/discontinued
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CreatedBy   string
	UpdatedBy   string
	DeletedAt   *time.Time
}

type SKU struct {
	ID        string
	TenantID  string
	ProductID string
	Code      string
	Name      string
	Price     float64
	Currency  string
	Unit      string
	UnitSize  float64
	Status    string // active/inactive
	CreatedAt time.Time
	UpdatedAt time.Time
	CreatedBy string
	UpdatedBy string
	DeletedAt *time.Time
}
