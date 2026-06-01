package domain

const (
	// HR Appraisal Events
	EventTypeAppraisalCompleted EventType = "hr.appraisal.completed"
	EventTypeAppraisalInitiated EventType = "hr.appraisal.initiated"

	// HR Attendance Events
	EventTypeAttendanceProcessed EventType = "hr.attendance.processed"
	EventTypeAttendanceRecorded  EventType = "hr.attendance.recorded"

	// HR Employee Events
	EventTypeEmployeeActivated EventType = "hr.employee.activated"
	EventTypeEmployeeOnboarded EventType = "hr.employee.onboarded"

	// HR Exit Events
	EventTypeExitCompleted EventType = "hr.exit.completed"
	EventTypeExitInitiated EventType = "hr.exit.initiated"

	// HR Expense Events
	EventTypeExpenseAllocated EventType = "hr.expense.allocated"
	EventTypeExpenseApproved  EventType = "hr.expense.approved"
	EventTypeExpenseSubmitted EventType = "hr.expense.submitted"

	// HR Leave Events
	EventTypeLeaveApproved  EventType = "hr.leave.approved"
	EventTypeLeaveRequested EventType = "hr.leave.requested"

	// HR Recruitment Events
	EventTypeRecruitmentOpened    EventType = "hr.recruitment.opened"
	EventTypeRecruitmentProcessed EventType = "hr.recruitment.processed"

	// HR Salary Events
	EventTypeSalaryRevised EventType = "hr.salary.revised"
	EventTypeSalaryUpdated EventType = "hr.salary.updated"

	// HR Training Events
	EventTypeTrainingCompleted EventType = "hr.training.completed"
	EventTypeTrainingScheduled EventType = "hr.training.scheduled"

	// Finance Events
	EventTypeCostAllocated             EventType = "finance.cost.allocated"
	EventTypeCostAllocationRequired    EventType = "finance.cost.allocation_required"
	EventTypeCostCenterExpenseRecorded EventType = "finance.costcenter.expense_recorded"
	EventTypeJournalRecorded           EventType = "finance.journal.recorded"
	EventTypeManualJournalSubmitted    EventType = "finance.journal.manual_submitted"

	// Purchase / Procurement Events
	EventTypeRFQSubmitted       EventType = "purchase.rfq.submitted"
	EventTypeRFQQuotesReceived  EventType = "purchase.rfq.quotes_received"
	EventTypeRFQAwarded         EventType = "purchase.rfq.awarded"
	EventTypePOCreated            EventType = "purchase.po.created"
	EventTypePOApproved           EventType = "purchase.po.approved"
	EventTypePOReceived           EventType = "purchase.po.received"
	EventTypePOConfirmed          EventType = "purchase.po.confirmed"
	EventTypePurchaseOrderCreated EventType = "purchase.purchaseorder.created"
	EventTypeInvoiceReceived           EventType = "purchase.invoice.received"
	EventTypeInvoiceApproved           EventType = "purchase.invoice.approved"
	EventTypeInvoiceMatched            EventType = "purchase.invoice.matched"
	EventTypePurchaseInvoiceSubmitted  EventType = "purchase.invoice.submitted"

	// Finance Receivable Events
	EventTypeInvoicePaid      EventType = "finance.receivable.invoice_paid"
	EventTypeReceiptRecorded  EventType = "finance.receivable.receipt_recorded"

	// Finance Tax Engine Events
	EventTypeTaxCalculationRequired EventType = "finance.tax.calculation_required"
	EventTypeTaxCalculated          EventType = "finance.tax.calculated"

	// Manufacturing Production Order Events
	EventTypeARReceiptCreated         EventType = "finance.ar.receipt.created"
	EventTypeProductionCostUpdated    EventType = "manufacturing.production.cost.updated"
	EventTypeManufacturingCostUpdated EventType = "manufacturing.cost.updated"

	// Analytics / Insights Events
	// TODO: Move to a real analytics events file when domain events are consolidated.
	EventTypeBusinessMetricsCalculated EventType = "analytics.business.metrics.calculated"
	EventTypeInsightsGenerated         EventType = "analytics.insights.generated"

	// Masters Data Events
	// TODO: Move to a real masters events file when domain events are consolidated.
	EventTypeMasterDataUpdated   EventType = "masters.data.updated"
	EventTypeMasterDataValidated EventType = "masters.data.validated"
)
