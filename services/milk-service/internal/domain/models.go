package domain

import "time"

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
	QuantityLiters float64
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
	FatPercent float64
	SNFPercent float64
	Lactose    float64
	TestedAt   time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
	CreatedBy  string
	UpdatedBy  string
}
