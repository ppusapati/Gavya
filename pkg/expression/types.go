package expression

import (
	"p9e.in/samavaya/packages/api/v1/data"
	"p9e.in/samavaya/packages/api/v1/query"

	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// FilterOperator represents a comparison operator used in filter conditions
type FilterOperator string

const (
	// String comparison operators
	OperatorEquals     FilterOperator = "$eq"
	OperatorNotEquals  FilterOperator = "$neq"
	OperatorContains   FilterOperator = "$contains"
	OperatorStartsWith FilterOperator = "$starts_with"
	OperatorEndsWith   FilterOperator = "$ends_with"
	OperatorIn         FilterOperator = "$in"
	OperatorNotIn      FilterOperator = "$nin"
	OperatorIsNull     FilterOperator = "$null"
	OperatorIsNotNull  FilterOperator = "$nnull"
	OperatorIsEmpty    FilterOperator = "$empty"
	OperatorIsNotEmpty FilterOperator = "$nempty"
	OperatorLike       FilterOperator = "$like"

	// Numeric comparison operators
	OperatorGreaterThan       FilterOperator = "$gt"
	OperatorGreaterThanEquals FilterOperator = "$gte"
	OperatorLessThan          FilterOperator = "$lt"
	OperatorLessThanEquals    FilterOperator = "$lte"
)

// Filter represents a single filter condition with field, operator, and value
type Filter struct {
	Field    string         `json:"field"`
	Operator FilterOperator `json:"operator"`
	Value    any            `json:"value"`
}

// DynamicFilter represents a filter that can handle multiple value types via DynamicValue
type DynamicFilter struct {
	Field    string             `json:"field"`
	Operator FilterOperator     `json:"operator"`
	Value    *data.DynamicValue `json:"value"`
}

// SearchCriteria provides a unified structure for search and filtering operations
// combining filters, pagination, sorting, and field selection.
// Used across all modules for consistent API request/response handling.
type SearchCriteria struct {
	// Basic Identifier Filters
	ID   *query.Int32FilterOperators  `json:"id,omitempty"`
	UUID *query.StringFilterOperation `json:"uuid,omitempty"`

	// Basic filters
	Filters []Filter `json:"filters"`

	// Search and Filter Operations
	SearchTerm *query.StringFilterOperation `json:"search_term,omitempty"`

	// Pagination Controls
	PageSize   int32 `json:"page_size,omitempty"`
	PageOffset int32 `json:"page_offset,omitempty"`

	// Sorting Controls
	Sort     []string `json:"sort,omitempty"`
	SortDesc *bool    `json:"sort_desc,omitempty"`

	// Field Mask for selective return
	FieldMask *fieldmaskpb.FieldMask `json:"field_mask,omitempty"`

	// Dynamic filters for complex queries
	DynamicFilters map[string]*data.DynamicValueFilter `json:"dynamic_filters,omitempty"`

	// Time-based Filters
	CreatedAtFrom *query.DateFilterOperators `json:"created_at_from,omitempty"`
	CreatedAtTo   *query.DateFilterOperators `json:"created_at_to,omitempty"`
	UpdatedAtFrom *query.DateFilterOperators `json:"updated_at_from,omitempty"`
	UpdatedAtTo   *query.DateFilterOperators `json:"updated_at_to,omitempty"`

	// Status and Active Filtering
	ActiveOnly *query.BooleanFilterOperators `json:"active_only,omitempty"`
	Statuses   []string                      `json:"statuses,omitempty"`

	// Tenant and Role Filtering
	TenantIds []string `json:"tenant_ids,omitempty"`
	RoleIds   []string `json:"role_ids,omitempty"`
}

// Extend provides a way to add domain-specific filters to the base SearchCriteria
func (sc *SearchCriteria) Extend(extendedFilters map[string]interface{}) *SearchCriteria {
	if sc.Filters == nil {
		sc.Filters = make([]Filter, 0)
	}
	// Domain-specific filters can be added by casting and merging
	return sc
}

// GetPageSize returns the page size, defaulting to a reasonable value if not set
func (sc *SearchCriteria) GetPageSize() int32 {
	if sc.PageSize <= 0 {
		return 20 // default page size
	}
	return sc.PageSize
}

// GetPageOffset returns the page offset (0-based)
func (sc *SearchCriteria) GetPageOffset() int32 {
	if sc.PageOffset < 0 {
		return 0
	}
	return sc.PageOffset
}

// HasSort returns true if any sorting is configured
func (sc *SearchCriteria) HasSort() bool {
	return len(sc.Sort) > 0
}

// IsSortDescending returns true if descending sort is requested
func (sc *SearchCriteria) IsSortDescending() bool {
	return sc.SortDesc != nil && *sc.SortDesc
}

// HasFilters returns true if any filters are configured
func (sc *SearchCriteria) HasFilters() bool {
	return len(sc.Filters) > 0 ||
		sc.SearchTerm != nil ||
		sc.ActiveOnly != nil ||
		len(sc.Statuses) > 0 ||
		sc.CreatedAtFrom != nil ||
		sc.CreatedAtTo != nil ||
		sc.UpdatedAtFrom != nil ||
		sc.UpdatedAtTo != nil
}
