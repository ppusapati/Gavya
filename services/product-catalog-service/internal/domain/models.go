package domain

import (
	"time"

	"github.com/ppusapati/gavya/libs/integrity/exact"
	"github.com/ppusapati/gavya/libs/integrity/money"
)

// The unit_size column: NUMERIC(10,3). A size arriving from the wire is
// checked against it and held at its scale.
const (
	UnitSizeScale     int32 = 3
	UnitSizePrecision int32 = 10
)

type Category struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug"`
	ParentID    *string    `json:"parent_id"`
	Description string     `json:"description"`
	SortOrder   int        `json:"sort_order"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CreatedBy   string     `json:"created_by"`
	UpdatedBy   string     `json:"updated_by"`
	DeletedAt   *time.Time `json:"deleted_at"`
}

type Brand struct {
	ID        string     `json:"id"`
	TenantID  string     `json:"tenant_id"`
	Name      string     `json:"name"`
	Slug      string     `json:"slug"`
	LogoURL   string     `json:"logo_url"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	CreatedBy string     `json:"created_by"`
	UpdatedBy string     `json:"updated_by"`
	DeletedAt *time.Time `json:"deleted_at"`
}

type Product struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	CategoryID  string     `json:"category_id"`
	BrandID     string     `json:"brand_id"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug"`
	Description string     `json:"description"`
	ProductType string     `json:"product_type"` // feed/medicine/supplement/accessory/equipment
	Status      string     `json:"status"`       // active/inactive/discontinued
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CreatedBy   string     `json:"created_by"`
	UpdatedBy   string     `json:"updated_by"`
	DeletedAt   *time.Time `json:"deleted_at"`
}

type SKU struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	ProductID string `json:"product_id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	// Price is exact, and carries the currency it is in.
	//
	// It was a float64 read out of a NUMERIC(18,4) column. Four decimals survive
	// float64 only below about 10^11 — measured, not assumed — so the schema
	// grew a CHECK refusing anything larger, because a figure the code would
	// mangle is worse stored than refused. Reading it as a decimal removes the
	// reason for that ceiling.
	//
	// The currency lives here rather than in a field beside it. Two places that
	// each claim to say what currency an amount is in are two places that can
	// disagree, and an amount whose currency is a separate field is one
	// refactor away from being added to an amount in another.
	Price money.Money `json:"price"`
	Unit  string      `json:"unit"`
	// UnitSize is a quantity, not money: 2.5 kg, not 2.50 rupees. It is exact
	// at its column's three decimals, the way the price is at its currency's.
	UnitSize  exact.Fixed `json:"unit_size"`
	Status    string      `json:"status"` // active/inactive
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
	CreatedBy string      `json:"created_by"`
	UpdatedBy string      `json:"updated_by"`
	DeletedAt *time.Time  `json:"deleted_at"`
}
