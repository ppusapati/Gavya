package domain

import (
	"time"

	"github.com/ppusapati/gavya/libs/integrity/exact"
)

// The columns a reading is held in: quantity_liters is NUMERIC(8,3), litres to
// the millilitre; the three analyses are NUMERIC(5,2). A value arriving from
// the wire is checked against these and held at their scale.
const (
	LitreScale       int32 = 3
	LitrePrecision   int32 = 8
	PercentScale     int32 = 2
	PercentPrecision int32 = 5
)

type MilkSession struct {
	ID          string
	TenantID    string
	CattleID    string
	SessionDate time.Time
	ShiftType   string // morning/afternoon/evening
	Status      string // pending/completed/cancelled
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CreatedBy   string
	UpdatedBy   string
	DeletedAt   *time.Time
}

type MilkRecord struct {
	ID             string
	TenantID       string
	SessionID      string
	CattleID       string
	QuantityLiters exact.Fixed
	RecordedAt     time.Time
	RecordedBy     string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CreatedBy      string
	UpdatedBy      string
	DeletedAt      *time.Time
}

type MilkQuality struct {
	ID         string
	TenantID   string
	RecordID   string
	FatPercent exact.Fixed
	SNFPercent exact.Fixed
	Lactose    exact.Fixed
	TestedAt   time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
	CreatedBy  string
	UpdatedBy  string
}
