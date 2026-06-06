package domain

import "time"

type Cattle struct {
	ID          string
	TenantID    string
	TagNumber   string
	Name        string
	BreedID     string
	DateOfBirth time.Time
	Gender      string // M/F
	Status      string // active/sold/deceased
	Weight      float64
	Color       string
	OwnerID     string
	FarmID      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CreatedBy   string
	UpdatedBy   string
	DeletedAt   *time.Time
}

type Breed struct {
	ID          string
	TenantID    string
	Name        string
	Origin      string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CreatedBy   string
	UpdatedBy   string
	DeletedAt   *time.Time
}

type CattleLineage struct {
	ID       string
	TenantID string
	CattleID string
	SireID   *string
	DamID    *string
}
