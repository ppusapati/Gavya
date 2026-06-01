package models

import (
	"time"

	stderrors "errors"
	"p9e.in/samavaya/packages/ulid"
)

// Construction domain errors
var (
	errProjectNumberRequired  = stderrors.New("project number is required")
	errProjectNameRequired    = stderrors.New("project name is required")
	errContractorIDRequired   = stderrors.New("contractor ID is required")
	errClientIDRequired       = stderrors.New("client ID is required")
)

// ConstructionProject represents a construction project
type ConstructionProject struct {
	ID              string       `json:"id" db:"id,primarykey"`
	ProjectNumber   string       `json:"project_number" db:"project_number"`
	ProjectName     string       `json:"project_name" db:"project_name"`
	ContractorID    string       `json:"contractor_id" db:"contractor_id"`
	ClientID        string       `json:"client_id" db:"client_id"`
	Location        *GeoLocation `json:"location" db:"location"`
	ProjectType     string       `json:"project_type" db:"project_type"` // BUILDING, INFRASTRUCTURE, INDUSTRIAL, RESIDENTIAL
	Phase           string       `json:"phase" db:"phase"`               // PLANNING, DESIGN, EXECUTION, CLOSURE
	Status          string       `json:"status" db:"status"`             // DRAFT, APPROVED, ACTIVE, PAUSED, COMPLETED, CANCELLED
	StartDate       *time.Time   `json:"start_date" db:"start_date"`
	EndDate         *time.Time   `json:"end_date" db:"end_date"`
	BudgetAmount    float64      `json:"budget_amount" db:"budget_amount"`
	ActualAmount    float64      `json:"actual_amount" db:"actual_amount"`
	CurrencyCode    string       `json:"currency_code" db:"currency_code"`
	Scope           string       `json:"scope" db:"scope"`
	Notes           string       `json:"notes" db:"notes"`
	CreatedAt       time.Time    `json:"created_at" db:"created_at"`
	CreatedBy       string       `json:"created_by" db:"created_by"`
	UpdatedAt       time.Time    `json:"updated_at" db:"updated_at"`
	UpdatedBy       string       `json:"updated_by" db:"updated_by"`
}

// ProjectPhase represents a phase within a construction project
type ProjectPhase struct {
	ID            string     `json:"id" db:"id,primarykey"`
	ProjectID     string     `json:"project_id" db:"project_id"`
	PhaseName     string     `json:"phase_name" db:"phase_name"`
	Status        string     `json:"status" db:"status"` // PLANNING, IN_PROGRESS, COMPLETED, PAUSED
	ScheduledStart *time.Time `json:"scheduled_start" db:"scheduled_start"`
	ScheduledEnd  *time.Time `json:"scheduled_end" db:"scheduled_end"`
	ActualStart   *time.Time `json:"actual_start" db:"actual_start"`
	ActualEnd     *time.Time `json:"actual_end" db:"actual_end"`
	PlannedBudget float64    `json:"planned_budget" db:"planned_budget"`
	ActualBudget  float64    `json:"actual_budget" db:"actual_budget"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at" db:"updated_at"`
}

// ConstructionBoQ represents a Bill of Quantities for a construction project
type ConstructionBoQ struct {
	ID             string      `json:"id" db:"id,primarykey"`
	ProjectID      string      `json:"project_id" db:"project_id"`
	Items          []*BoQItem  `json:"items" db:"items"`
	TotalQuantity  float64     `json:"total_quantity" db:"total_quantity"`
	TotalAmount    float64     `json:"total_amount" db:"total_amount"`
	CurrencyCode   string      `json:"currency_code" db:"currency_code"`
	Status         string      `json:"status" db:"status"` // DRAFT, APPROVED, ACTIVE, COMPLETED
	CreatedAt      time.Time   `json:"created_at" db:"created_at"`
	CreatedBy      string      `json:"created_by" db:"created_by"`
	UpdatedAt      time.Time   `json:"updated_at" db:"updated_at"`
	UpdatedBy      string      `json:"updated_by" db:"updated_by"`
}

// BoQItem represents a single item in a Bill of Quantities
type BoQItem struct {
	ID            string  `json:"id" db:"id,primarykey"`
	BoQID         string  `json:"boq_id" db:"boq_id"`
	ItemNumber    string  `json:"item_number" db:"item_number"`
	Description   string  `json:"description" db:"description"`
	Unit          string  `json:"unit" db:"unit"` // SQFT, METER, KG, PIECES, etc.
	Quantity      float64 `json:"quantity" db:"quantity"`
	UnitRate      float64 `json:"unit_rate" db:"unit_rate"`
	Amount        float64 `json:"amount" db:"amount"` // quantity * unit_rate
	Category      string  `json:"category" db:"category"` // MATERIAL, LABOR, EQUIPMENT, SUBCONTRACTOR
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

// ConstructionEquipment represents equipment used in construction projects
type ConstructionEquipment struct {
	ID                  string     `json:"id" db:"id,primarykey"`
	EquipmentCode       string     `json:"equipment_code" db:"equipment_code"`
	Name                string     `json:"name" db:"name"`
	Type                string     `json:"type" db:"type"` // MACHINERY, VEHICLE, TOOL, SAFETY_EQUIPMENT
	SerialNumber        string     `json:"serial_number" db:"serial_number"`
	ProjectID           string     `json:"project_id" db:"project_id"`
	Status              string     `json:"status" db:"status"` // AVAILABLE, DEPLOYED, MAINTENANCE, RETIRED
	DeploymentDate      *time.Time `json:"deployment_date" db:"deployment_date"`
	RetirementDate      *time.Time `json:"retirement_date" db:"retirement_date"`
	HourlyRate          float64    `json:"hourly_rate" db:"hourly_rate"`
	CurrencyCode        string     `json:"currency_code" db:"currency_code"`
	MaintenanceSchedule string     `json:"maintenance_schedule" db:"maintenance_schedule"`
	CreatedAt           time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at" db:"updated_at"`
}

// ConstructionResource represents resources allocated to a project
type ConstructionResource struct {
	ID              string    `json:"id" db:"id,primarykey"`
	ProjectID       string    `json:"project_id" db:"project_id"`
	ResourceType    string    `json:"resource_type" db:"resource_type"` // MATERIAL, LABOR, EQUIPMENT
	ResourceID      string    `json:"resource_id" db:"resource_id"`
	Quantity        float64   `json:"quantity" db:"quantity"`
	UOM             string    `json:"uom" db:"uom"`
	AllocatedAmount float64   `json:"allocated_amount" db:"allocated_amount"`
	Status          string    `json:"status" db:"status"` // ALLOCATED, IN_USE, CONSUMED, RETURNED
	AllocatedAt     time.Time `json:"allocated_at" db:"allocated_at"`
	AllocatedBy     string    `json:"allocated_by" db:"allocated_by"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
}

// SafetyIncident represents a safety incident on a construction site
type SafetyIncident struct {
	ID               string    `json:"id" db:"id,primarykey"`
	ProjectID        string    `json:"project_id" db:"project_id"`
	IncidentType     string    `json:"incident_type" db:"incident_type"` // INJURY, NEAR_MISS, HAZARD, PROPERTY_DAMAGE
	Severity         string    `json:"severity" db:"severity"`           // LOW, MEDIUM, HIGH, CRITICAL
	Description      string    `json:"description" db:"description"`
	InjuryType       string    `json:"injury_type" db:"injury_type"` // MINOR, MODERATE, SEVERE
	AffectedWorkers  []string  `json:"affected_workers" db:"affected_workers"`
	ReportedAt       time.Time `json:"reported_at" db:"reported_at"`
	ReportedBy       string    `json:"reported_by" db:"reported_by"`
	Status           string    `json:"status" db:"status"` // REPORTED, INVESTIGATING, RESOLVED, CLOSED
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time `json:"updated_at" db:"updated_at"`
}

// ComplianceStatus represents compliance status for a project
type ComplianceStatus struct {
	ID               string     `json:"id" db:"id,primarykey"`
	ProjectID        string     `json:"project_id" db:"project_id"`
	Status           string     `json:"status" db:"status"` // COMPLIANT, NON_COMPLIANT, PARTIAL_COMPLIANT
	LastInspection   *time.Time `json:"last_inspection" db:"last_inspection"`
	NextInspection   *time.Time `json:"next_inspection" db:"next_inspection"`
	Violations       []string   `json:"violations" db:"violations"`
	ComplianceScore  float64    `json:"compliance_score" db:"compliance_score"`
	InspectorID      string     `json:"inspector_id" db:"inspector_id"`
	Notes            string     `json:"notes" db:"notes"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at" db:"updated_at"`
}

// ConstructionSite represents a construction site
type ConstructionSite struct {
	ID        string       `json:"id" db:"id,primarykey"`
	ProjectID string       `json:"project_id" db:"project_id"`
	SiteName  string       `json:"site_name" db:"site_name"`
	Location  *GeoLocation `json:"location" db:"location"`
	Status    string       `json:"status" db:"status"` // SETUP, ACTIVE, CLOSURE, CLOSED
	CreatedAt time.Time    `json:"created_at" db:"created_at"`
	ClosedAt  *time.Time   `json:"closed_at" db:"closed_at"`
	Manager   string       `json:"manager" db:"manager"`
	Contact   string       `json:"contact" db:"contact"`
	UpdatedAt time.Time    `json:"updated_at" db:"updated_at"`
}

// ProjectGLAccount represents GL account setup for a project
type ProjectGLAccount struct {
	ID              string    `json:"id" db:"id,primarykey"`
	ProjectID       string    `json:"project_id" db:"project_id"`
	GLAccountCode   string    `json:"gl_account_code" db:"gl_account_code"`
	AccountName     string    `json:"account_name" db:"account_name"`
	BudgetAmount    float64   `json:"budget_amount" db:"budget_amount"`
	CurrencyCode    string    `json:"currency_code" db:"currency_code"`
	Status          string    `json:"status" db:"status"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
	CreatedBy       string    `json:"created_by" db:"created_by"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
}

// ProjectCost represents a cost recorded for a project
type ProjectCost struct {
	ID            string    `json:"id" db:"id,primarykey"`
	ProjectID     string    `json:"project_id" db:"project_id"`
	CostType      string    `json:"cost_type" db:"cost_type"` // MATERIAL, LABOR, EQUIPMENT, SUBCONTRACTOR
	Amount        float64   `json:"amount" db:"amount"`
	CostCenterID  string    `json:"cost_center_id" db:"cost_center_id"`
	CostDate      time.Time `json:"cost_date" db:"cost_date"`
	Description   string    `json:"description" db:"description"`
	Status        string    `json:"status" db:"status"` // PENDING, APPROVED, POSTED
	RecordedAt    time.Time `json:"recorded_at" db:"recorded_at"`
	RecordedBy    string    `json:"recorded_by" db:"recorded_by"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

// ProgressPayment represents a progress payment for a project
type ProgressPayment struct {
	ID               string    `json:"id" db:"id,primarykey"`
	ProjectID        string    `json:"project_id" db:"project_id"`
	Milestone        string    `json:"milestone" db:"milestone"`
	CompletionPercent float64   `json:"completion_percent" db:"completion_percent"`
	PaymentAmount    float64   `json:"payment_amount" db:"payment_amount"`
	RetentionAmount  float64   `json:"retention_amount" db:"retention_amount"`
	NetPayment       float64   `json:"net_payment" db:"net_payment"`
	Status           string    `json:"status" db:"status"` // GENERATED, APPROVED, PROCESSED, PAID
	RecordedAt       time.Time `json:"recorded_at" db:"recorded_at"`
	RecordedBy       string    `json:"recorded_by" db:"recorded_by"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time `json:"updated_at" db:"updated_at"`
}

// NewConstructionProject creates a new construction project
func NewConstructionProject(projectNumber, projectName, contractorID, clientID string) *ConstructionProject {
	now := time.Now().UTC()
	return &ConstructionProject{
		ID:            ulid.New().String(),
		ProjectNumber: projectNumber,
		ProjectName:   projectName,
		ContractorID:  contractorID,
		ClientID:      clientID,
		Status:        "DRAFT",
		Phase:         "PLANNING",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// NewProjectPhase creates a new project phase
func NewProjectPhase(projectID, phaseName string) *ProjectPhase {
	now := time.Now().UTC()
	return &ProjectPhase{
		ID:        ulid.New().String(),
		ProjectID: projectID,
		PhaseName: phaseName,
		Status:    "PLANNING",
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// NewConstructionBoQ creates a new Bill of Quantities
func NewConstructionBoQ(projectID string) *ConstructionBoQ {
	now := time.Now().UTC()
	return &ConstructionBoQ{
		ID:        ulid.New().String(),
		ProjectID: projectID,
		Items:     []*BoQItem{},
		Status:    "DRAFT",
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// NewBoQItem creates a new BoQ item
func NewBoQItem(boqID, itemNumber, description string) *BoQItem {
	now := time.Now().UTC()
	return &BoQItem{
		ID:          ulid.New().String(),
		BoQID:       boqID,
		ItemNumber:  itemNumber,
		Description: description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// CalculateAmount calculates the total amount for a BoQ item
func (item *BoQItem) CalculateAmount() {
	item.Amount = item.Quantity * item.UnitRate
}

// ProjectMaterialPlan represents material plan for a project
type ProjectMaterialPlan struct {
	ID            string    `json:"id" db:"id,primarykey"`
	ProjectID     string    `json:"project_id" db:"project_id"`
	ProjectNumber string    `json:"project_number" db:"project_number"`
	Status        string    `json:"status" db:"status"` // DRAFT, APPROVED, ACTIVE, COMPLETED
	TotalPlannedCost float64 `json:"total_planned_cost" db:"total_planned_cost"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	CreatedBy     string    `json:"created_by" db:"created_by"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
	UpdatedBy     string    `json:"updated_by" db:"updated_by"`
}

// MaterialAllocation represents material allocation to a project phase
type MaterialAllocation struct {
	ID              string    `json:"id" db:"id,primarykey"`
	ProjectID       string    `json:"project_id" db:"project_id"`
	PhaseID         string    `json:"phase_id" db:"phase_id"`
	Status          string    `json:"status" db:"status"` // ALLOCATED, IN_USE, CONSUMED, RETURNED
	AllocatedAt     time.Time `json:"allocated_at" db:"allocated_at"`
	AllocatedBy     string    `json:"allocated_by" db:"allocated_by"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
}

// MaterialConsumption represents material consumption in a phase
type MaterialConsumption struct {
	ID            string    `json:"id" db:"id,primarykey"`
	ProjectID     string    `json:"project_id" db:"project_id"`
	PhaseID       string    `json:"phase_id" db:"phase_id"`
	Status        string    `json:"status" db:"status"` // RECORDED, APPROVED, POSTED
	RecordedAt    time.Time `json:"recorded_at" db:"recorded_at"`
	RecordedBy    string    `json:"recorded_by" db:"recorded_by"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at" db:"updated_at"`
}

// MaterialReceipt represents material receipt for a project
type MaterialReceipt struct {
	ID              string    `json:"id" db:"id,primarykey"`
	ProjectID       string    `json:"project_id" db:"project_id"`
	MaterialOrderID string    `json:"material_order_id" db:"material_order_id"`
	Status          string    `json:"status" db:"status"` // RECEIVED, INSPECTED, ACCEPTED, REJECTED
	ReceivedAt      time.Time `json:"received_at" db:"received_at"`
	ReceivedBy      string    `json:"received_by" db:"received_by"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
}

// Validate performs validation checks on construction project
func (p *ConstructionProject) Validate() error {
	if p.ProjectNumber == "" {
		return errProjectNumberRequired
	}
	if p.ProjectName == "" {
		return errProjectNameRequired
	}
	if p.ContractorID == "" {
		return errContractorIDRequired
	}
	if p.ClientID == "" {
		return errClientIDRequired
	}
	return nil
}

// ProjectBOM represents a Bill of Materials for a construction project
type ProjectBOM struct {
	ID              string         `json:"id" db:"id,primarykey"`
	ProjectID       string         `json:"project_id" db:"project_id"`
	ProjectNumber   string         `json:"project_number" db:"project_number"`
	PhaseID         string         `json:"phase_id" db:"phase_id"`
	PhaseName       string         `json:"phase_name" db:"phase_name"`
	BOMNumber       string         `json:"bom_number" db:"bom_number"`
	Materials       []BOMMaterial  `json:"materials" db:"materials"`
	TotalMaterialCost float64      `json:"total_material_cost" db:"total_material_cost"`
	TotalMaterials  int            `json:"total_materials" db:"total_materials"`
	Status          string         `json:"status" db:"status"` // draft, approved, in_progress, completed
	CreatedBy       string         `json:"created_by" db:"created_by"`
	CreatedAt       time.Time      `json:"created_at" db:"created_at"`
	UpdatedBy       string         `json:"updated_by" db:"updated_by"`
	UpdatedAt       time.Time      `json:"updated_at" db:"updated_at"`
}

// BOMMaterial represents a material in project BOM
type BOMMaterial struct {
	MaterialID    string  `json:"material_id" db:"material_id"`
	MaterialCode  string  `json:"material_code" db:"material_code"`
	MaterialName  string  `json:"material_name" db:"material_name"`
	MaterialType  string  `json:"material_type" db:"material_type"`
	Quantity      float64 `json:"quantity" db:"quantity"`
	UOM           string  `json:"uom" db:"uom"`
	UnitCost      float64 `json:"unit_cost" db:"unit_cost"`
	MaterialCost  float64 `json:"material_cost" db:"material_cost"`
	AllocatedQty  float64 `json:"allocated_qty" db:"allocated_qty"`
	ConsumedQty   float64 `json:"consumed_qty" db:"consumed_qty"`
	Notes         string  `json:"notes" db:"notes"`
}

// BOMAllocation represents allocation of BOM materials to phase
type BOMAllocation struct {
	ID              string                   `json:"id" db:"id,primarykey"`
	AllocationID    string                   `json:"allocation_id" db:"allocation_id"`
	BOMID           string                   `json:"bom_id" db:"bom_id"`
	ProjectID       string                   `json:"project_id" db:"project_id"`
	PhaseID         string                   `json:"phase_id" db:"phase_id"`
	Materials       []AllocatedMaterialItem  `json:"materials" db:"materials"`
	TotalAllocated  float64                  `json:"total_allocated" db:"total_allocated"`
	Status          string                   `json:"status" db:"status"`
	AllocatedBy     string                   `json:"allocated_by" db:"allocated_by"`
	AllocatedAt     time.Time                `json:"allocated_at" db:"allocated_at"`
	CreatedAt       time.Time                `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time                `json:"updated_at" db:"updated_at"`
}

// AllocatedMaterialItem represents allocated material details
type AllocatedMaterialItem struct {
	MaterialID        string  `json:"material_id" db:"material_id"`
	AllocatedQty      float64 `json:"allocated_qty" db:"allocated_qty"`
	WarehouseLocation string  `json:"warehouse_location" db:"warehouse_location"`
	Notes             string  `json:"notes" db:"notes"`
}
