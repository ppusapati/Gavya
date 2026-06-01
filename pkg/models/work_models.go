package models

import (
	"time"

	stderrors "errors"
	"p9e.in/samavaya/packages/ulid"
)

// Work domain errors
var (
	errWorkOrderNumberRequired = stderrors.New("work order number is required")
	errWorkerIDRequired        = stderrors.New("worker ID is required")
	errCustomerIDRequired      = stderrors.New("customer ID is required")
)

// WorkOrder represents a work order for field service operations
type WorkOrder struct {
	ID               string    `json:"id" db:"id,primarykey"`
	OrderNumber      string    `json:"order_number" db:"order_number"`
	OrderType        string    `json:"order_type" db:"order_type"` // MAINTENANCE, REPAIR, INSTALLATION, INSPECTION
	WorkerID         string    `json:"worker_id" db:"worker_id"`
	CustomerID       string    `json:"customer_id" db:"customer_id"`
	Status           string    `json:"status" db:"status"` // DRAFT, SCHEDULED, ASSIGNED, IN_PROGRESS, COMPLETED, CANCELLED
	ScheduledDate    *time.Time `json:"scheduled_date" db:"scheduled_date"`
	PlannedDuration  int32     `json:"planned_duration" db:"planned_duration"` // in minutes
	ActualDuration   int32     `json:"actual_duration" db:"actual_duration"`   // in minutes
	Location         *GeoLocation `json:"location" db:"location"`
	ServiceCategory  string    `json:"service_category" db:"service_category"` // HVAC, PLUMBING, ELECTRICAL, etc.
	SkillsRequired   []string  `json:"skills_required" db:"skills_required"`
	Priority         string    `json:"priority" db:"priority"` // LOW, MEDIUM, HIGH, URGENT
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
	CreatedBy        string    `json:"created_by" db:"created_by"`
	UpdatedAt        time.Time `json:"updated_at" db:"updated_at"`
	UpdatedBy        string    `json:"updated_by" db:"updated_by"`
}

// FieldService represents field service execution details
type FieldService struct {
	ID               string       `json:"id" db:"id,primarykey"`
	WorkOrderID      string       `json:"work_order_id" db:"work_order_id"`
	Status           string       `json:"status" db:"status"` // PENDING, STARTED, IN_PROGRESS, COMPLETED, CANCELLED
	StartedAt        *time.Time   `json:"started_at" db:"started_at"`
	EndedAt          *time.Time   `json:"ended_at" db:"ended_at"`
	StartLocation    *GeoLocation `json:"start_location" db:"start_location"`
	EndLocation      *GeoLocation `json:"end_location" db:"end_location"`
	DistanceTraveled float64      `json:"distance_traveled" db:"distance_traveled"` // in km
	Notes            string       `json:"notes" db:"notes"`
	CreatedAt        time.Time    `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at" db:"updated_at"`
}

// WorkerSkill represents worker skill certifications
type WorkerSkill struct {
	ID          string     `json:"id" db:"id,primarykey"`
	WorkerID    string     `json:"worker_id" db:"worker_id"`
	SkillID     string     `json:"skill_id" db:"skill_id"`
	CertifiedAt *time.Time `json:"certified_at" db:"certified_at"`
	ExpiresAt   *time.Time `json:"expires_at" db:"expires_at"`
	CertifiedBy string     `json:"certified_by" db:"certified_by"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
}

// WorkOrderResource represents resources allocated to a work order
type WorkOrderResource struct {
	ID            string    `json:"id" db:"id,primarykey"`
	WorkOrderID   string    `json:"work_order_id" db:"work_order_id"`
	ResourceType  string    `json:"resource_type" db:"resource_type"` // PART, TOOL, MATERIAL
	ResourceID    string    `json:"resource_id" db:"resource_id"`
	Quantity      float64   `json:"quantity" db:"quantity"`
	UOM           string    `json:"uom" db:"uom"` // PIECES, KG, LITERS, etc.
	AllocatedAt   time.Time `json:"allocated_at" db:"allocated_at"`
	AllocatedBy   string    `json:"allocated_by" db:"allocated_by"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

// GeoLocation represents geographic coordinates and address
type GeoLocation struct {
	Latitude  float64   `json:"latitude" db:"latitude"`
	Longitude float64   `json:"longitude" db:"longitude"`
	Address   string    `json:"address" db:"address"`
	PlaceName string    `json:"place_name" db:"place_name"`
	RecordedAt time.Time `json:"recorded_at" db:"recorded_at"`
}

// WorkOrderCost represents cost allocation for work orders
type WorkOrderCost struct {
	ID            string    `json:"id" db:"id,primarykey"`
	WorkOrderID   string    `json:"work_order_id" db:"work_order_id"`
	CostType      string    `json:"cost_type" db:"cost_type"` // LABOR, PARTS, TRAVEL, MATERIALS
	Amount        float64   `json:"amount" db:"amount"`
	CostCenterID  string    `json:"cost_center_id" db:"cost_center_id"`
	CostCategory  string    `json:"cost_category" db:"cost_category"`
	Description   string    `json:"description" db:"description"`
	RecordedAt    time.Time `json:"recorded_at" db:"recorded_at"`
	RecordedBy    string    `json:"recorded_by" db:"recorded_by"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

// CostAllocation represents cost allocation details
type CostAllocation struct {
	ID            string    `json:"id" db:"id,primarykey"`
	WorkOrderID   string    `json:"work_order_id" db:"work_order_id"`
	CostCenterID  string    `json:"cost_center_id" db:"cost_center_id"`
	Amount        float64   `json:"amount" db:"amount"`
	CostCategory  string    `json:"cost_category" db:"cost_category"`
	Description   string    `json:"description" db:"description"`
	Status        string    `json:"status" db:"status"` // PENDING, APPROVED, POSTED
	AllocatedAt   time.Time `json:"allocated_at" db:"allocated_at"`
	AllocatedBy   string    `json:"allocated_by" db:"allocated_by"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

// FieldServiceExpense represents field service expenses
type FieldServiceExpense struct {
	ID              string    `json:"id" db:"id,primarykey"`
	WorkOrderID     string    `json:"work_order_id" db:"work_order_id"`
	ExpenseType     string    `json:"expense_type" db:"expense_type"` // TRAVEL, PARTS, ACCOMMODATION, MEALS
	Amount          float64   `json:"amount" db:"amount"`
	CurrencyCode    string    `json:"currency_code" db:"currency_code"`
	ExpenseDate     time.Time `json:"expense_date" db:"expense_date"`
	Description     string    `json:"description" db:"description"`
	ReceiptURL      string    `json:"receipt_url" db:"receipt_url"`
	Status          string    `json:"status" db:"status"` // SUBMITTED, APPROVED, REJECTED, REIMBURSED
	RecordedAt      time.Time `json:"recorded_at" db:"recorded_at"`
	RecordedBy      string    `json:"recorded_by" db:"recorded_by"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
}

// WorkerPaymentSchedule represents payment schedules for workers
type WorkerPaymentSchedule struct {
	ID              string    `json:"id" db:"id,primarykey"`
	WorkerID        string    `json:"worker_id" db:"worker_id"`
	HourlyRate      float64   `json:"hourly_rate" db:"hourly_rate"`
	MonthlyHours    int32     `json:"monthly_hours" db:"monthly_hours"`
	MonthlyAmount   float64   `json:"monthly_amount" db:"monthly_amount"`
	PaymentMethod   string    `json:"payment_method" db:"payment_method"` // BANK, CASH, CHECK
	BankAccount     string    `json:"bank_account" db:"bank_account"`
	Status          string    `json:"status" db:"status"` // ACTIVE, INACTIVE, SUSPENDED
	EffectiveFrom   time.Time `json:"effective_from" db:"effective_from"`
	EffectiveTo     *time.Time `json:"effective_to" db:"effective_to"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
	CreatedBy       string    `json:"created_by" db:"created_by"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
	UpdatedBy       string    `json:"updated_by" db:"updated_by"`
}

// NewWorkOrder creates a new work order with a generated ID
func NewWorkOrder(orderNumber, orderType, workerID, customerID string) *WorkOrder {
	now := time.Now().UTC()
	return &WorkOrder{
		ID:          ulid.New().String(),
		OrderNumber: orderNumber,
		OrderType:   orderType,
		WorkerID:    workerID,
		CustomerID:  customerID,
		Status:      "DRAFT",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// NewFieldService creates a new field service record
func NewFieldService(workOrderID string) *FieldService {
	now := time.Now().UTC()
	return &FieldService{
		ID:          ulid.New().String(),
		WorkOrderID: workOrderID,
		Status:      "PENDING",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// NewWorkerSkill creates a new worker skill record
func NewWorkerSkill(workerID, skillID string) *WorkerSkill {
	now := time.Now().UTC()
	return &WorkerSkill{
		ID:        ulid.New().String(),
		WorkerID:  workerID,
		SkillID:   skillID,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// NewWorkOrderResource creates a new work order resource
func NewWorkOrderResource(workOrderID, resourceType, resourceID string) *WorkOrderResource {
	now := time.Now().UTC()
	return &WorkOrderResource{
		ID:          ulid.New().String(),
		WorkOrderID: workOrderID,
		ResourceType: resourceType,
		ResourceID:  resourceID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// PartAllocation represents parts allocated to work order
type PartAllocation struct {
	ID           string    `json:"id" db:"id,primarykey"`
	WorkOrderID  string    `json:"work_order_id" db:"work_order_id"`
	LocationID   string    `json:"location_id" db:"location_id"`
	Status       string    `json:"status" db:"status"` // ALLOCATED, CONSUMED, RETURNED, CANCELLED
	AllocatedAt  time.Time `json:"allocated_at" db:"allocated_at"`
	AllocatedBy  string    `json:"allocated_by" db:"allocated_by"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// PartConsumption represents consumed parts from work order
type PartConsumption struct {
	ID            string    `json:"id" db:"id,primarykey"`
	WorkOrderID   string    `json:"work_order_id" db:"work_order_id"`
	AllocationID  string    `json:"allocation_id" db:"allocation_id"`
	Status        string    `json:"status" db:"status"` // RECORDED, APPROVED, POSTED
	ConsumedAt    time.Time `json:"consumed_at" db:"consumed_at"`
	ConsumedBy    string    `json:"consumed_by" db:"consumed_by"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

// FieldStockTransfer represents stock transfer between field locations
type FieldStockTransfer struct {
	ID             string    `json:"id" db:"id,primarykey"`
	FromLocationID string    `json:"from_location_id" db:"from_location_id"`
	ToLocationID   string    `json:"to_location_id" db:"to_location_id"`
	Status         string    `json:"status" db:"status"` // INITIATED, COMPLETED, CANCELLED
	TransferredAt  time.Time `json:"transferred_at" db:"transferred_at"`
	TransferredBy  string    `json:"transferred_by" db:"transferred_by"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" db:"updated_at"`
}

// Validate performs validation checks on work order
func (w *WorkOrder) Validate() error {
	if w.OrderNumber == "" {
		return errWorkOrderNumberRequired
	}
	if w.WorkerID == "" {
		return errWorkerIDRequired
	}
	if w.CustomerID == "" {
		return errCustomerIDRequired
	}
	return nil
}

// AssemblyBOM represents a Bill of Materials for assembly operations
type AssemblyBOM struct {
	ID               string         `json:"id" db:"id,primarykey"`
	BOMID            string         `json:"bom_id" db:"bom_id"`
	AssemblyID       string         `json:"assembly_id" db:"assembly_id"`
	AssemblyCode     string         `json:"assembly_code" db:"assembly_code"`
	AssemblyName     string         `json:"assembly_name" db:"assembly_name"`
	WorkType         string         `json:"work_type" db:"work_type"` // assembly, subassembly, component
	Components       []BOMComponent `json:"components" db:"components"`
	BOMNumber        string         `json:"bom_number" db:"bom_number"`
	TotalBOMCost     float64        `json:"total_bom_cost" db:"total_bom_cost"`
	MaterialCost     float64        `json:"material_cost" db:"material_cost"`
	LaborCost        float64        `json:"labor_cost" db:"labor_cost"`
	OverheadCost     float64        `json:"overhead_cost" db:"overhead_cost"`
	ScrapCost        float64        `json:"scrap_cost" db:"scrap_cost"`
	ComponentCount   int            `json:"component_count" db:"component_count"`
	Status           string         `json:"status" db:"status"` // draft, approved, active, obsolete
	CreatedBy        string         `json:"created_by" db:"created_by"`
	CreatedAt        time.Time      `json:"created_at" db:"created_at"`
	UpdatedBy        string         `json:"updated_by" db:"updated_by"`
	UpdatedAt        time.Time      `json:"updated_at" db:"updated_at"`
}

// BOMComponent represents a component in assembly BOM
type BOMComponent struct {
	ComponentID   string  `json:"component_id" db:"component_id"`
	ComponentCode string  `json:"component_code" db:"component_code"`
	ComponentName string  `json:"component_name" db:"component_name"`
	Quantity      float64 `json:"quantity" db:"quantity"`
	UOM           string  `json:"uom" db:"uom"`
	UnitCost      float64 `json:"unit_cost" db:"unit_cost"`
	ComponentCost float64 `json:"component_cost" db:"component_cost"`
	ScrapFactor   string  `json:"scrap_factor" db:"scrap_factor"`
	Notes         string  `json:"notes" db:"notes"`
}
