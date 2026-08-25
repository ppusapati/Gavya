package domain

import (
	"strings"
	"time"
)

type BreedingCycle struct {
	ID, TenantID, CattleID string
	HeatDate               time.Time
	Status                 string // heat/inseminated/pregnant/calved/failed
	Notes                  string
	CreatedAt, UpdatedAt   time.Time
	CreatedBy, UpdatedBy   string
	DeletedAt              *time.Time
}

type Insemination struct {
	ID, TenantID, CycleID, CattleID string
	BullID, SemenBatchID            *string
	InseminatedAt                   time.Time
	Method                          string // natural/AI
	CreatedAt, UpdatedAt            time.Time
	CreatedBy, UpdatedBy            string
	DeletedAt                       *time.Time
}

type Pregnancy struct {
	ID, TenantID, CattleID, InseminationID string
	ConfirmedAt, ExpectedCalvingDate       time.Time
	Status                                 string // active/delivered/aborted
	CreatedAt, UpdatedAt                   time.Time
	CreatedBy, UpdatedBy                   string
	DeletedAt                              *time.Time
}

type CalvingRecord struct {
	ID, TenantID, PregnancyID, CattleID string
	CalfID                              *string
	CalvingDate                         time.Time
	CalfGender                          string
	CalfWeight                          float64
	Complications, Status               string // normal/assisted/emergency
	CreatedAt, UpdatedAt                time.Time
	CreatedBy, UpdatedBy                string
	DeletedAt                           *time.Time
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
