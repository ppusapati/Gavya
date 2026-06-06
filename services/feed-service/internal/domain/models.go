package domain

import "time"

type FeedType struct {
	ID, TenantID, Name, Category, Unit, NutritionalInfo string
	CreatedAt, UpdatedAt                                 time.Time
	CreatedBy, UpdatedBy                                 string
	DeletedAt                                            *time.Time
}

type NutritionPlan struct {
	ID, TenantID, CattleID, FeedTypeID string
	DailyQuantityKg                    float64
	StartDate                          time.Time
	EndDate                            *time.Time
	Notes                              string
	CreatedAt, UpdatedAt               time.Time
	CreatedBy, UpdatedBy               string
	DeletedAt                          *time.Time
}

type FeedConsumption struct {
	ID, TenantID, CattleID, FeedTypeID string
	QuantityKg                         float64
	FedAt                              time.Time
	FedBy                              string
	CreatedAt, UpdatedAt               time.Time
	CreatedBy, UpdatedBy               string
}

type FeedConsumptionReport struct {
	CattleID   string
	FeedTypeID string
	TotalKg    float64
}
