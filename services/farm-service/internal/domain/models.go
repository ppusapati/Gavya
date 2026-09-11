package domain

import "time"

type Farm struct {
	ID        string     `json:"id"`
	TenantID  string     `json:"tenant_id"`
	Name      string     `json:"name"`
	Code      string     `json:"code"`
	Address   string     `json:"address"`
	City      string     `json:"city"`
	State     string     `json:"state"`
	Country   string     `json:"country"`
	Capacity  int        `json:"capacity"`
	ManagerID string     `json:"manager_id"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	CreatedBy string     `json:"created_by"`
	UpdatedBy string     `json:"updated_by"`
	DeletedAt *time.Time `json:"deleted_at"`
}

type FarmSection struct {
	ID               string     `json:"id"`
	TenantID         string     `json:"tenant_id"`
	FarmID           string     `json:"farm_id"`
	Name             string     `json:"name"`
	SectionType      string     `json:"section_type"`
	Capacity         int        `json:"capacity"`
	CurrentOccupancy int        `json:"current_occupancy"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	CreatedBy        string     `json:"created_by"`
	UpdatedBy        string     `json:"updated_by"`
	DeletedAt        *time.Time `json:"deleted_at"`
}
