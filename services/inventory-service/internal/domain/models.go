package domain

import "time"

type Warehouse struct {
	ID        string     `json:"id"`
	TenantID  string     `json:"tenant_id"`
	Name      string     `json:"name"`
	Code      string     `json:"code"`
	Address   string     `json:"address"`
	ManagerID string     `json:"manager_id"`
	Status    string     `json:"status"` // active/inactive
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	CreatedBy string     `json:"created_by"`
	UpdatedBy string     `json:"updated_by"`
	DeletedAt *time.Time `json:"deleted_at"`
}

type InventoryItem struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	WarehouseID      string    `json:"warehouse_id"`
	SKUID            string    `json:"sku_id"`
	QuantityOnHand   float64   `json:"quantity_on_hand"`
	QuantityReserved float64   `json:"quantity_reserved"`
	ReorderPoint     float64   `json:"reorder_point"`
	MaxStock         float64   `json:"max_stock"`
	LastUpdatedAt    time.Time `json:"last_updated_at"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	CreatedBy        string    `json:"created_by"`
	UpdatedBy        string    `json:"updated_by"`
}

type StockMovement struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	WarehouseID   string    `json:"warehouse_id"`
	SKUID         string    `json:"sku_id"`
	MovementType  string    `json:"movement_type"` // in/out/adjustment/transfer
	Quantity      float64   `json:"quantity"`
	ReferenceID   string    `json:"reference_id"`
	ReferenceType string    `json:"reference_type"`
	Notes         string    `json:"notes"`
	MovedAt       time.Time `json:"moved_at"`
	MovedBy       string    `json:"moved_by"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	CreatedBy     string    `json:"created_by"`
	UpdatedBy     string    `json:"updated_by"`
}

type Batch struct {
	ID             string     `json:"id"`
	TenantID       string     `json:"tenant_id"`
	WarehouseID    string     `json:"warehouse_id"`
	SKUID          string     `json:"sku_id"`
	BatchNumber    string     `json:"batch_number"`
	Quantity       float64    `json:"quantity"`
	ManufacturedAt *time.Time `json:"manufactured_at"`
	ExpiresAt      *time.Time `json:"expires_at"`
	Status         string     `json:"status"` // available/expired/recalled
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	CreatedBy      string     `json:"created_by"`
	UpdatedBy      string     `json:"updated_by"`
	DeletedAt      *time.Time `json:"deleted_at"`
}
