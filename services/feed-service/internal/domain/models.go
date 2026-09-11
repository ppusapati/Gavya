package domain

import "time"

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
	ID              string     `json:"id"`
	TenantID        string     `json:"tenant_id"`
	CattleID        string     `json:"cattle_id"`
	FeedTypeID      string     `json:"feed_type_id"`
	DailyQuantityKg float64    `json:"daily_quantity_kg"`
	StartDate       time.Time  `json:"start_date"`
	EndDate         *time.Time `json:"end_date"`
	Notes           string     `json:"notes"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	CreatedBy       string     `json:"created_by"`
	UpdatedBy       string     `json:"updated_by"`
	DeletedAt       *time.Time `json:"deleted_at"`
}

type FeedConsumption struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id"`
	CattleID   string    `json:"cattle_id"`
	FeedTypeID string    `json:"feed_type_id"`
	QuantityKg float64   `json:"quantity_kg"`
	FedAt      time.Time `json:"fed_at"`
	FedBy      string    `json:"fed_by"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	CreatedBy  string    `json:"created_by"`
	UpdatedBy  string    `json:"updated_by"`
}

type FeedConsumptionReport struct {
	CattleID   string  `json:"cattle_id"`
	FeedTypeID string  `json:"feed_type_id"`
	TotalKg    float64 `json:"total_kg"`
}
