package domain

import (
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"
)

type Vaccination struct {
	ID             string     `json:"id"`
	TenantID       string     `json:"tenant_id"`
	CattleID       string     `json:"cattle_id"`
	VaccineName    string     `json:"vaccine_name"`
	BatchNumber    string     `json:"batch_number"`
	AdministeredAt time.Time  `json:"administered_at"`
	NextDueDate    *time.Time `json:"next_due_date"`
	VeterinarianID string     `json:"veterinarian_id"`
	Dosage         string     `json:"dosage"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	CreatedBy      string     `json:"created_by"`
	UpdatedBy      string     `json:"updated_by"`
	DeletedAt      *time.Time `json:"deleted_at"`
}

type Treatment struct {
	ID            string     `json:"id"`
	TenantID      string     `json:"tenant_id"`
	CattleID      string     `json:"cattle_id"`
	DiagnosisCode string     `json:"diagnosis_code"`
	Diagnosis     string     `json:"diagnosis"`
	MedicineName  string     `json:"medicine_name"`
	Dosage        string     `json:"dosage"`
	TreatedAt     time.Time  `json:"treated_at"`
	TreatedBy     string     `json:"treated_by"`
	FollowUpDate  *time.Time `json:"follow_up_date"`
	Status        string     `json:"status"` // ongoing/completed/monitoring
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	CreatedBy     string     `json:"created_by"`
	UpdatedBy     string     `json:"updated_by"`
	DeletedAt     *time.Time `json:"deleted_at"`
}

type VetVisit struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	CattleID       string    `json:"cattle_id"`
	VeterinarianID string    `json:"veterinarian_id"`
	VisitDate      time.Time `json:"visit_date"`
	Purpose        string    `json:"purpose"`
	Notes          string    `json:"notes"`
	// Cost is exact and carries the currency it is in. A money record that does
	// not say which currency is a number, and one whose currency sits in a field
	// beside it is one refactor away from being added to an amount in another.
	//
	// It was a float64 read out of a NUMERIC(18,4) column, which carries four
	// decimals faithfully only below about 10^11 — so the schema had a CHECK
	// refusing anything larger, a limit of the Go read path written into the
	// database. See libs/integrity/money.ParseStored.
	Cost      money.Money `json:"cost"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
	CreatedBy string      `json:"created_by"`
	UpdatedBy string      `json:"updated_by"`
	DeletedAt *time.Time  `json:"deleted_at"`
}
