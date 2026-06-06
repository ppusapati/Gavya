package domain

import "time"

type Farm struct {
	ID, TenantID, Name, Code, Address, City, State, Country string
	Capacity                                                  int
	ManagerID, Status                                         string
	CreatedAt, UpdatedAt                                      time.Time
	CreatedBy, UpdatedBy                                      string
	DeletedAt                                                 *time.Time
}

type FarmSection struct {
	ID, TenantID, FarmID, Name, SectionType string
	Capacity, CurrentOccupancy              int
	CreatedAt, UpdatedAt                    time.Time
	CreatedBy, UpdatedBy                    string
	DeletedAt                               *time.Time
}
