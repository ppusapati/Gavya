# Proto Enum Consolidation Changelog

## Overview
This document records the consolidation of duplicate/reusable enums into a central `packages/proto/enum.proto` file.

## Created File
- **packages/proto/enum.proto** - Central location for generic, reusable enums

## Generic Enums Added to packages/proto/enum.proto

| Enum Name | Description | Used By |
|-----------|-------------|---------|
| Priority | LOW, MEDIUM, HIGH, CRITICAL priority levels | maintenance, formbuilder/approval |
| Frequency | DAILY to ANNUAL frequency options | depreciation |
| ApprovalStatus | PENDING, IN_PROGRESS, APPROVED, REJECTED, etc. | formbuilder/approval |
| ActionType | APPROVE, REJECT, DELEGATE, etc. | formbuilder/approval |
| WaveStatus | DRAFT, PLANNED, RELEASED, IN_PROGRESS, etc. | wms, fulfillment |
| PickListStatus | PENDING, ASSIGNED, IN_PROGRESS, COMPLETED, etc. | wms, fulfillment |
| PickType | SINGLE, BATCH, CLUSTER, ZONE | wms, fulfillment |
| Quarter | Q1, Q2, Q3, Q4 | (available for TDS, GST, etc.) |
| DayOfWeek | SUNDAY through SATURDAY | (available for scheduling) |
| Month | JANUARY through DECEMBER | (available for financial periods) |
| Gender | MALE, FEMALE, OTHER, PREFER_NOT_TO_SAY | (available for HR) |
| AddressType | RESIDENTIAL, COMMERCIAL, BILLING, SHIPPING, etc. | (available for addresses) |
| ContactType | EMAIL, PHONE, MOBILE, FAX, WHATSAPP | (available for contacts) |
| SortOrder | ASC, DESC | (available for queries) |
| GenericStatus | DRAFT, ACTIVE, INACTIVE, PENDING, etc. | (generic lifecycle status) |
| TimeUnit | SECONDS to YEARS | (available for durations/SLAs) |
| ComparisonOperator | EQUALS, NOT_EQUALS, GREATER_THAN, etc. | (available for filters/rules) |

## Files Updated

### 1. asset/maintenance/proto/maintenance.proto
- **Removed**: Local `Priority` enum
- **Added Import**: `packages/proto/enum.proto`
- **Updated References**: `Priority` -> `packages.api.v1.enums.Priority`

### 2. workflow/formbuilder/proto/approval.proto
- **Removed**: Local `Priority`, `ApprovalStatus`, `ActionType` enums
- **Added Import**: `packages/proto/enum.proto`
- **Updated References**:
  - `Priority` -> `packages.api.v1.enums.Priority`
  - `ApprovalStatus` -> `packages.api.v1.enums.ApprovalStatus`
  - `ActionType` -> `packages.api.v1.enums.ActionType`

### 3. asset/depreciation/proto/depreciation.proto
- **Removed**: Local `Frequency` enum
- **Added Import**: `packages/proto/enum.proto`
- **Updated References**: `Frequency` -> `packages.api.v1.enums.Frequency`

### 4. inventory/wms/proto/wms.proto
- **Removed**: Local `WaveStatus`, `PickListStatus`, `PickType` enums
- **Kept Local**: `WaveType` (WMS-specific values)
- **Added Import**: `packages/proto/enum.proto`
- **Updated References**:
  - `WaveStatus` -> `packages.api.v1.enums.WaveStatus`
  - `PickListStatus` -> `packages.api.v1.enums.PickListStatus`
  - `PickType` -> `packages.api.v1.enums.PickType`

### 5. fulfillment/fulfillment/proto/fulfillment.proto
- **Removed**: Local `WaveStatus`, `PickListStatus`, `PickType` enums
- **Kept Local**: `WaveType` (Fulfillment-specific values different from WMS)
- **Added Import**: `packages/proto/enum.proto`
- **Updated References**:
  - `WaveStatus` -> `packages.api.v1.enums.WaveStatus`
  - `PickListStatus` -> `packages.api.v1.enums.PickListStatus`
  - `PickType` -> `packages.api.v1.enums.PickType`

## Enums Kept Separate (Not Consolidated)

### workflow/approval/proto/approval.proto - ApprovalStatus
**Reason**: Different naming convention (e.g., `PENDING_APPROVAL` vs `APPROVAL_STATUS_PENDING`). Changing would be a breaking change for existing code.

### Domain-Specific Enums
The following enum types were analyzed but kept separate due to having domain-specific values:
- `ShipmentStatus` - Different values for WMS, Stock Transfer, and Shipping contexts
- `TaskStatus` - Different values for Workflow, WMS, Projects, and Financial Close
- `ReturnType` - Incompatible domains (product returns vs. tax returns)
- `ReturnStatus` - Different workflows for product vs. tax returns
- `ReconciliationStatus` - Different detail levels across banking, GST, cash management
- `DocumentType` - Domain-specific document types (Vehicle, E-Invoice, HR)
- `ReceiptStatus` - Different receipt workflows across finance, returns, purchase, inventory
- `TransactionType` - Domain-specific transaction types
- `VehicleType` - Asset classification vs. regulatory classification
- `WaveType` - Different values between WMS and Fulfillment

## Usage Instructions

To use a consolidated enum in your proto file:

1. Add the import statement:
```protobuf
import "packages/proto/enum.proto";
```

2. Use the fully qualified enum name:
```protobuf
message MyMessage {
  packages.api.v1.enums.Priority priority = 1;
  packages.api.v1.enums.ApprovalStatus status = 2;
}
```

## Package Information
- **Package**: `packages.api.v1.enums`
- **Go Package**: `p9e.in/samavaya/packages/api/v1/enums;enums`
