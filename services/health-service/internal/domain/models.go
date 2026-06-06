package domain

import "time"

type Vaccination struct {
	ID, TenantID, CattleID   string
	VaccineName, BatchNumber  string
	AdministeredAt            time.Time
	NextDueDate               *time.Time
	VeterinarianID, Dosage    string
	CreatedAt, UpdatedAt      time.Time
	CreatedBy, UpdatedBy      string
	DeletedAt                 *time.Time
}

type Treatment struct {
	ID, TenantID, CattleID    string
	DiagnosisCode, Diagnosis  string
	MedicineName, Dosage      string
	TreatedAt                 time.Time
	TreatedBy                 string
	FollowUpDate              *time.Time
	Status                    string // ongoing/completed/monitoring
	CreatedAt, UpdatedAt      time.Time
	CreatedBy, UpdatedBy      string
	DeletedAt                 *time.Time
}

type VetVisit struct {
	ID, TenantID, CattleID    string
	VeterinarianID            string
	VisitDate                 time.Time
	Purpose, Notes            string
	Cost                      float64
	CreatedAt, UpdatedAt      time.Time
	CreatedBy, UpdatedBy      string
	DeletedAt                 *time.Time
}
