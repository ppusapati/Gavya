package domain

// Work Vertical Event Types
const (
	EventTypeWorkOrderCreated         = "work.order.created"
	EventTypeWorkOrderAssigned        = "work.order.assigned"
	EventTypeWorkOrderStarted         = "work.order.started"
	EventTypeWorkOrderCompleted       = "work.order.completed"
	EventTypeWorkOrderCancelled       = "work.order.cancelled"
	EventTypeFieldServiceStarted      = "work.field.service.started"
	EventTypeFieldServiceEnded        = "work.field.service.ended"
	EventTypeWorkerSkillCertified     = "work.worker.skill.certified"
	EventTypeServicePartsAllocated    = "work.service.parts.allocated"
	EventTypeServicePartsConsumed     = "work.service.parts.consumed"
	EventTypeFieldInventoryUpdated    = "work.field.inventory.updated"
	EventTypeWorkerPaymentProcessed   = "work.worker.payment.processed"
	EventTypeServiceCostAllocated     = "work.service.cost.allocated"
	EventTypeWorkOrderCostAllocated   = "work.order.cost.allocated"
	EventTypeFieldServiceExpenseRecorded = "work.field.service.expense.recorded"
	EventTypeWorkerPaymentScheduleCreated = "work.worker.payment.schedule.created"
)

// Construction Vertical Event Types
const (
	EventTypeConstructionProjectCreated       = "construction.project.created"
	EventTypeConstructionProjectPhaseChanged  = "construction.project.phase.changed"
	EventTypeConstructionBoQCreated           = "construction.boq.created"
	EventTypeConstructionEquipmentAllocated   = "construction.equipment.allocated"
	EventTypeConstructionEquipmentDeployed    = "construction.equipment.deployed"
	EventTypeConstructionMaterialAllocated    = "construction.material.allocated"
	EventTypeConstructionMaterialConsumed     = "construction.material.consumed"
	EventTypeConstructionMaterialReceived     = "construction.material.received"
	EventTypeSafetyIncidentReported           = "construction.safety.incident.reported"
	EventTypeSafetyInspectionCompleted        = "construction.safety.inspection.completed"
	EventTypeConstructionSiteCreated          = "construction.site.created"
	EventTypeConstructionSiteClosed           = "construction.site.closed"
	EventTypeProgressPaymentGenerated         = "construction.progress.payment.generated"
	EventTypeConstructionProjectCostRecorded   = "construction.project.cost.recorded"
	EventTypeSubcontractorInvoiceCreated      = "construction.subcontractor.invoice.created"
	EventTypeProjectGLCreated                 = "construction.project.gl.created"
	EventTypeCostVarianceDetected             = "construction.cost.variance.detected"
	EventTypeBudgetExceeded                   = "construction.budget.exceeded"
)

// TopicForEvent returns the Kafka topic for a given event type
func TopicForEvent(eventType string) string {
	switch eventType {
	case EventTypeWorkOrderCreated, EventTypeWorkOrderAssigned, EventTypeWorkOrderStarted,
		EventTypeWorkOrderCompleted, EventTypeWorkOrderCancelled, EventTypeFieldServiceStarted,
		EventTypeFieldServiceEnded, EventTypeWorkerSkillCertified, EventTypeServicePartsAllocated,
		EventTypeServicePartsConsumed, EventTypeFieldInventoryUpdated, EventTypeWorkerPaymentProcessed,
		EventTypeServiceCostAllocated, EventTypeWorkOrderCostAllocated, EventTypeFieldServiceExpenseRecorded,
		EventTypeWorkerPaymentScheduleCreated:
		return "samavaya.work.events"
	case EventTypeConstructionProjectCreated, EventTypeConstructionProjectPhaseChanged, EventTypeConstructionBoQCreated,
		EventTypeConstructionEquipmentAllocated, EventTypeConstructionEquipmentDeployed, EventTypeConstructionMaterialAllocated,
		EventTypeConstructionMaterialConsumed, EventTypeConstructionMaterialReceived, EventTypeSafetyIncidentReported,
		EventTypeSafetyInspectionCompleted, EventTypeConstructionSiteCreated, EventTypeConstructionSiteClosed,
		EventTypeProgressPaymentGenerated, EventTypeConstructionProjectCostRecorded, EventTypeSubcontractorInvoiceCreated,
		EventTypeProjectGLCreated, EventTypeCostVarianceDetected, EventTypeBudgetExceeded:
		return "samavaya.construction.events"
	default:
		return "samavaya.events"
	}
}

// WorkOrderCostAllocatedEvent is fired when work order cost is allocated
type WorkOrderCostAllocatedEvent struct {
	WorkOrderID   string
	AllocationID  string
	Amount        float64
	CostCenterID  string
	CostCategory  string
	AllocatedBy   string
	AllocatedAt   string
}

// FieldServiceExpenseRecordedEvent is fired when field service expense is recorded
type FieldServiceExpenseRecordedEvent struct {
	WorkOrderID   string
	ExpenseID     string
	ExpenseType   string
	Amount        float64
	CurrencyCode  string
	ExpenseDate   string
	RecordedBy    string
	RecordedAt    string
}

// WorkerPaymentScheduleCreatedEvent is fired when payment schedule is created
type WorkerPaymentScheduleCreatedEvent struct {
	WorkerID      string
	ScheduleID    string
	MonthlyAmount float64
	HourlyRate    float64
	CreatedBy     string
	CreatedAt     string
}

// ProjectGLCreatedEvent is fired when project GL is created
type ProjectGLCreatedEvent struct {
	ProjectID    string
	GLAccountCode string
	BudgetAmount float64
	CurrencyCode string
	CreatedBy    string
	CreatedAt    string
}

// ProjectCostRecordedEvent is fired when project cost is recorded
type ProjectCostRecordedEvent struct {
	ProjectID    string
	CostID       string
	CostType     string
	Amount       float64
	CostCenterID string
	RecordedBy   string
	RecordedAt   string
}

// CostVarianceDetectedEvent is fired when cost variance is detected
type CostVarianceDetectedEvent struct {
	ProjectID    string
	CostType     string
	Budgeted     float64
	Actual       float64
	Variance     float64
	VariancePercent float64
	DetectedAt   string
}

// BudgetExceededEvent is fired when project budget is exceeded
type BudgetExceededEvent struct {
	ProjectID    string
	BudgetAmount float64
	ActualAmount float64
	ExcessAmount float64
	ExceededBy   string
	ExceededAt   string
}

// ProgressPaymentGeneratedEvent is fired when progress payment is generated
type ProgressPaymentGeneratedEvent struct {
	ProjectID    string
	PaymentID    string
	Milestone    string
	PaymentAmount float64
	RetentionAmount float64
	GeneratedBy  string
	GeneratedAt  string
}

// ConstructionProjectCreatedEvent is fired when construction project is created
type ConstructionProjectCreatedEvent struct {
	ProjectID    string
	ProjectNumber string
	ProjectName  string
	ContractorID string
	ClientID     string
	BudgetAmount float64
	CreatedBy    string
	CreatedAt    string
}

// SafetyIncidentReportedEvent is fired when safety incident is reported
type SafetyIncidentReportedEvent struct {
	ProjectID    string
	IncidentID   string
	IncidentType string
	Severity     string
	Description  string
	ReportedBy   string
	ReportedAt   string
}
