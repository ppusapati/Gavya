package models

import (
	"p9e.in/samavaya/packages/api/v1/data"
	"p9e.in/samavaya/packages/api/v1/query" // Assuming you have a common query package

	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

type FilterOperator string

const (
	// String operators
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

	// Numeric operators
	OperatorGreaterThan       FilterOperator = "$gt"
	OperatorGreaterThanEquals FilterOperator = "$gte"
	OperatorLessThan          FilterOperator = "$lt"
	OperatorLessThanEquals    FilterOperator = "$lte"
)

// Filter represents a single filter condition
type Filter struct {
	Field    string         `json:"field"`
	Operator FilterOperator `json:"operator"`
	Value    any            `json:"value"`
}

// DynamicFilter represents a filter that can handle multiple value types
type DynamicFilter struct {
	Field    string             `json:"field"`
	Operator FilterOperator     `json:"operator"`
	Value    *data.DynamicValue `json:"value"`
}

// SearchCriteria provides a generic structure for search and filtering across different domains
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
	Sort []string `json:"sort,omitempty"`

	// Field Mask for selective return
	FieldMask *fieldmaskpb.FieldMask `json:"field_mask,omitempty"`

	// Dynamic filters for complex queries
	DynamicFilters map[string]*data.DynamicValueFilter `json:"dynamic_filters,omitempty"`

	SortDesc *bool `json:"sort_desc,omitempty"`

	// Time-based Filters
	CreatedAtFrom *query.DateFilterOperators `json:"created_at_from,omitempty"`
	CreatedAtTo   *query.DateFilterOperators `json:"created_at_to,omitempty"`
	UpdatedAtFrom *query.DateFilterOperators `json:"updated_at_from,omitempty"`
	UpdatedAtTo   *query.DateFilterOperators `json:"updated_at_to,omitempty"`

	// Status and Active Filtering
	ActiveOnly *query.BooleanFilterOperators `json:"active_only,omitempty"`
	Statuses   []string                      `json:"statuses,omitempty"`

	// Specific user domain filters
	TenantIds []string `json:"tenant_ids,omitempty"`
	RoleIds   []string `json:"role_ids,omitempty"`
	// Nested Filtering
}

// Extend provides a way to add domain-specific filters to the base SearchCriteria
func (sc *SearchCriteria) Extend(extendedFilters map[string]interface{}) *SearchCriteria {
	// You can implement logic to add additional filters dynamically
	return sc
}
