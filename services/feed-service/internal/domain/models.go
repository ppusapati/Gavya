package domain

import (
	"time"

	"github.com/ppusapati/gavya/libs/integrity/exact"
)

// The kilogram columns: NUMERIC(8,3). A quantity arriving from the wire is
// checked against these and held at their scale.
const (
	KgScale     int32 = 3
	KgPrecision int32 = 8
)

type FeedType struct {
	ID              string     `json:"id"`
	TenantID        string     `json:"tenant_id"`
	Name            string     `json:"name"`
	Category        string     `json:"category"`
	Unit            string     `json:"unit"`
	NutritionalInfo string     `json:"nutritional_info"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	CreatedBy       string     `json:"created_by"`
	UpdatedBy       string     `json:"updated_by"`
	DeletedAt       *time.Time `json:"deleted_at"`
}

type NutritionPlan struct {
	ID              string      `json:"id"`
	TenantID        string      `json:"tenant_id"`
	CattleID        string      `json:"cattle_id"`
	FeedTypeID      string      `json:"feed_type_id"`
	DailyQuantityKg exact.Fixed `json:"daily_quantity_kg"`
	StartDate       time.Time   `json:"start_date"`
	EndDate         *time.Time  `json:"end_date"`
	Notes           string      `json:"notes"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
	CreatedBy       string      `json:"created_by"`
	UpdatedBy       string      `json:"updated_by"`
	DeletedAt       *time.Time  `json:"deleted_at"`
}

type FeedConsumption struct {
	ID         string      `json:"id"`
	TenantID   string      `json:"tenant_id"`
	CattleID   string      `json:"cattle_id"`
	FeedTypeID string      `json:"feed_type_id"`
	QuantityKg exact.Fixed `json:"quantity_kg"`
	FedAt      time.Time   `json:"fed_at"`
	FedBy      string      `json:"fed_by"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
	CreatedBy  string      `json:"created_by"`
	UpdatedBy  string      `json:"updated_by"`
}

type FeedConsumptionReport struct {
	CattleID   string `json:"cattle_id"`
	FeedTypeID string `json:"feed_type_id"`
	// TotalKg is summed by the database in the column's own type and read back
	// at its scale.
	TotalKg exact.Fixed `json:"total_kg"`
}
