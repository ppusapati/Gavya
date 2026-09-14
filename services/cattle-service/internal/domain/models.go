package domain

import (
	"time"

	"github.com/ppusapati/gavya/libs/integrity/exact"
)

// The weight column: NUMERIC(8,2), kilograms to the hundredth. A weight
// arriving from the wire is checked against this and held at its scale.
const (
	WeightScale     int32 = 2
	WeightPrecision int32 = 8
)

type Cattle struct {
	ID          string
	TenantID    string
	TagNumber   string
	Name        string
	BreedID     string
	DateOfBirth time.Time
	Gender      string // M/F
	Status      string // active/sold/deceased
	// Weight is a measurement, exact at the column's two decimals. The column
	// is nullable — an animal whose weight was never taken is a real thing —
	// and a NULL reads back as zero, which is what this service has always
	// written for one.
	Weight    exact.Fixed
	Color     string
	OwnerID   string
	FarmID    string
	CreatedAt time.Time
	UpdatedAt time.Time
	CreatedBy string
	UpdatedBy string
	DeletedAt *time.Time
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
