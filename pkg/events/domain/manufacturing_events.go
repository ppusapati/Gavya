package domain

// Manufacturing Module Event Types
const (
	// BOM Events
	EventTypeBOMCreated    EventType = "manufacturing.bom.created"
	EventTypeBOMUpdated    EventType = "manufacturing.bom.updated"
	EventTypeBOMApproved   EventType = "manufacturing.bom.approved"

	// Routing Events
	EventTypeRoutingCreated  EventType = "manufacturing.routing.created"
	EventTypeRoutingUpdated  EventType = "manufacturing.routing.updated"
	EventTypeRoutingOptimized EventType = "manufacturing.routing.optimized"

	// Production Order Events
	EventTypeProductionOrderCreated  EventType = "manufacturing.production.order.created"
	EventTypeProductionOrderReleased EventType = "manufacturing.production.order.released"
	EventTypeProductionOrderCompleted EventType = "manufacturing.production.order.completed"
	EventTypeProductionStarted       EventType = "manufacturing.production.started"
	EventTypeProductionCompleted     EventType = "manufacturing.production.completed"

	// Job Card Events
	EventTypeJobCardCreated   EventType = "manufacturing.jobcard.created"
	EventTypeJobCardStarted   EventType = "manufacturing.jobcard.started"
	EventTypeJobCardCompleted EventType = "manufacturing.jobcard.completed"

	// Shop Floor Events
	EventTypeShopFloorProductionStarted EventType = "manufacturing.shopfloor.production.started"
	EventTypeShopFloorDowntimeRecorded  EventType = "manufacturing.shopfloor.downtime.recorded"

	// Subcontracting Events
	EventTypeSubcontractOrderCreated   EventType = "manufacturing.subcontract.order.created"
	EventTypeSubcontractMaterialSent   EventType = "manufacturing.subcontract.material.sent"
	EventTypeSubcontractGoodsReceived  EventType = "manufacturing.subcontract.goods.received"

	// Work Center Events
	EventTypeWorkCenterUpdated        EventType = "manufacturing.workcenter.updated"
	EventTypeWorkCenterCapacityChanged EventType = "manufacturing.workcenter.capacity.changed"
	EventTypeWorkCenterMaintenanceScheduled EventType = "manufacturing.workcenter.maintenance.scheduled"

	// Materials Events
	EventTypeMaterialsAllocated EventType = "manufacturing.materials.allocated"
	EventTypeMaterialsConsumed  EventType = "manufacturing.materials.consumed"

	// Inventory-Manufacturing Integration Events
	EventTypeInventoryReserved EventType = "inventory.reserved"
)
