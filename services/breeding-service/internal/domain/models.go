package domain

import (
	"strings"
	"time"
)

type BreedingCycle struct {
	ID        string     `json:"id"`
	TenantID  string     `json:"tenant_id"`
	CattleID  string     `json:"cattle_id"`
	HeatDate  time.Time  `json:"heat_date"`
	Status    string     `json:"status"` // heat/inseminated/pregnant/calved/failed
	Notes     string     `json:"notes"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	CreatedBy string     `json:"created_by"`
	UpdatedBy string     `json:"updated_by"`
	DeletedAt *time.Time `json:"deleted_at"`
}

type Insemination struct {
	ID            string     `json:"id"`
	TenantID      string     `json:"tenant_id"`
	CycleID       string     `json:"cycle_id"`
	CattleID      string     `json:"cattle_id"`
	BullID        *string    `json:"bull_id"`
	SemenBatchID  *string    `json:"semen_batch_id"`
	InseminatedAt time.Time  `json:"inseminated_at"`
	Method        string     `json:"method"` // natural/AI
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	CreatedBy     string     `json:"created_by"`
	UpdatedBy     string     `json:"updated_by"`
	DeletedAt     *time.Time `json:"deleted_at"`
}

type Pregnancy struct {
	ID                  string     `json:"id"`
	TenantID            string     `json:"tenant_id"`
	CattleID            string     `json:"cattle_id"`
	InseminationID      string     `json:"insemination_id"`
	ConfirmedAt         time.Time  `json:"confirmed_at"`
	ExpectedCalvingDate time.Time  `json:"expected_calving_date"`
	Status              string     `json:"status"` // active/delivered/aborted
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	CreatedBy           string     `json:"created_by"`
	UpdatedBy           string     `json:"updated_by"`
	DeletedAt           *time.Time `json:"deleted_at"`
}

type CalvingRecord struct {
	ID            string     `json:"id"`
	TenantID      string     `json:"tenant_id"`
	PregnancyID   string     `json:"pregnancy_id"`
	CattleID      string     `json:"cattle_id"`
	CalfID        *string    `json:"calf_id"`
	CalvingDate   time.Time  `json:"calving_date"`
	CalfGender    string     `json:"calf_gender"`
	CalfWeight    float64    `json:"calf_weight"`
	Complications string     `json:"complications"`
	Status        string     `json:"status"` // normal/assisted/emergency
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	CreatedBy     string     `json:"created_by"`
	UpdatedBy     string     `json:"updated_by"`
	DeletedAt     *time.Time `json:"deleted_at"`
}

// Breeding states, named so a comparison against a literal cannot drift from
// what the service writes.
const (
	CycleOpen        = "open"
	CycleInseminated = "inseminated"
	CyclePregnant    = "pregnant"
	CycleClosed      = "closed"

	// Calf sexes, as the column records them: one character each.
	CalfFemale = "F"
	CalfMale   = "M"

	// How the calving went.
	CalvingNormal    = "normal"
	CalvingAssisted  = "assisted"
	CalvingEmergency = "emergency"

	PregnancyActive    = "active"
	PregnancyDelivered = "delivered"
	PregnancyLost      = "lost"
)

// NormaliseCalfSex accepts what a person would type and returns what the column
// stores.
//
// calf_gender is VARCHAR(1), and nothing validated it: sending "female" — the
// obvious value for a string field of that name — reached PostgreSQL as a
// length violation and was reported to the caller as an internal failure. The
// column's shape is not something a client should have to know.
func NormaliseCalfSex(v string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "f", "female", "heifer":
		return CalfFemale, true
	case "m", "male", "bull":
		return CalfMale, true
	case "":
		// Unrecorded is allowed: the column is nullable, and a stillbirth may
		// genuinely have no sex recorded.
		return "", true
	default:
		return "", false
	}
}

// ValidCalvingStatus reports whether this is how a calving is described.
func ValidCalvingStatus(v string) bool {
	switch v {
	case CalvingNormal, CalvingAssisted, CalvingEmergency:
		return true
	default:
		return false
	}
}
