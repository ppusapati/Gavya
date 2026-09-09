package domain

import (
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"
)

type Vaccination struct {
	ID, TenantID, CattleID   string
	VaccineName, BatchNumber string
	AdministeredAt           time.Time
	NextDueDate              *time.Time
	VeterinarianID, Dosage   string
	CreatedAt, UpdatedAt     time.Time
	CreatedBy, UpdatedBy     string
	DeletedAt                *time.Time
}

type Treatment struct {
	ID, TenantID, CattleID   string
	DiagnosisCode, Diagnosis string
	MedicineName, Dosage     string
	TreatedAt                time.Time
	TreatedBy                string
	FollowUpDate             *time.Time
	Status                   string // ongoing/completed/monitoring
	CreatedAt, UpdatedAt     time.Time
	CreatedBy, UpdatedBy     string
	DeletedAt                *time.Time
}

type VetVisit struct {
	ID, TenantID, CattleID string
	VeterinarianID         string
	VisitDate              time.Time
	Purpose, Notes         string
	// Cost is exact and carries the currency it is in. A money record that does
	// not say which currency is a number, and one whose currency sits in a field
	// beside it is one refactor away from being added to an amount in another.
	//
	// It was a float64 read out of a NUMERIC(18,4) column, which carries four
	// decimals faithfully only below about 10^11 — so the schema had a CHECK
	// refusing anything larger, a limit of the Go read path written into the
	// database. See libs/integrity/money.ParseStored.
	Cost                 money.Money
	CreatedAt, UpdatedAt time.Time
	CreatedBy, UpdatedBy string
	DeletedAt            *time.Time
}
