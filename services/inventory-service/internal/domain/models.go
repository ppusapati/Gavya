package domain

import "time"

type Warehouse struct {
	ID        string
	TenantID  string
	Name      string
	Code      string
	Address   string
	ManagerID string
	Status    string // active/inactive
	CreatedAt time.Time
	UpdatedAt time.Time
	CreatedBy string
	UpdatedBy string
	DeletedAt *time.Time
}

type InventoryItem struct {
	ID               string
	TenantID         string
	WarehouseID      string
	SKUID            string
	QuantityOnHand   float64
	QuantityReserved float64
	ReorderPoint     float64
	MaxStock         float64
	LastUpdatedAt    time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
	CreatedBy        string
	UpdatedBy        string
}

type StockMovement struct {
	ID            string
	TenantID      string
	WarehouseID   string
	SKUID         string
	MovementType  string // in/out/adjustment/transfer
	Quantity      float64
	ReferenceID   string
	ReferenceType string
	Notes         string
	MovedAt       time.Time
	MovedBy       string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	CreatedBy     string
	UpdatedBy     string
}

type Batch struct {
	ID             string
	TenantID       string
	WarehouseID    string
	SKUID          string
	BatchNumber    string
	Quantity       float64
	ManufacturedAt *time.Time
	ExpiresAt      *time.Time
	Status         string // available/expired/recalled
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CreatedBy      string
	UpdatedBy      string
	DeletedAt      *time.Time
}
