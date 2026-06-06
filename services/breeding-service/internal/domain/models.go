package domain

import "time"

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
	CalfID                               *string
	CalvingDate                         time.Time
	CalfGender                          string
	CalfWeight                          float64
	Complications, Status               string // normal/assisted/emergency
	CreatedAt, UpdatedAt                time.Time
	CreatedBy, UpdatedBy                string
	DeletedAt                           *time.Time
}
