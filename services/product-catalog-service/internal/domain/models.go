package domain

import (
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"
)

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
	Price money.Money
	Unit  string
	// UnitSize is a quantity, not money: 2.5 kg, not 2.50 rupees. It is still a
	// float64 and out of scope here; libs/integrity/quantity is where it would
	// go, and NUMERIC(10,3) is nowhere near the range where float64 loses a
	// digit.
	UnitSize  float64
	Status    string // active/inactive
	CreatedAt time.Time
	UpdatedAt time.Time
	CreatedBy string
	UpdatedBy string
	DeletedAt *time.Time
}
