// Package expression provides unified expression evaluation and filtering capabilities
// across the samavaya ERP backend.
//
// The package consolidates three key concerns:
//
// 1. FILTERING: Database query building from filter objects
//    - Filter and DynamicFilter types for representing conditions
//    - FilterOperator constants for comparison operations
//    - Conversion utilities for SQL query generation
//
// 2. RULE EVALUATION: In-memory expression evaluation for business logic
//    - RuleEvaluator for parsing and evaluating logical expressions
//    - Support for complex conditions with AND/OR operators
//    - Field references, type coercion, regex pattern matching
//
// 3. SEARCH CRITERIA: Unified search and filter request structure
//    - SearchCriteria combining pagination, sorting, filtering, field masking
//    - Support for time-based, status-based, and domain-specific filters
//
// Usage Examples:
//
// Filter Operators (for SQL queries):
//    filter := expression.Filter{
//        Field:    "amount",
//        Operator: expression.OperatorGreaterThan,
//        Value:    1000,
//    }
//
// Rule Evaluation (for business logic):
//    evaluator := expression.NewEvaluator()
//    result, err := evaluator.EvaluateRule("amount > 1000 AND status == \"approved\"", data)
//
// Search Criteria (for API requests):
//    criteria := &expression.SearchCriteria{
//        Filters: []expression.Filter{...},
//        PageSize: 20,
//        Sort: []string{"created_at"},
//    }
//
// Integration Points:
// - FormBuilder: Grid row validation, conditional visibility/readonly
// - MetaSearch: Advanced filter expressions
// - ReportBuilder: Complex filtering and aggregation rules
// - Database: SQL WHERE clause generation
// - Authorization: Rule-based access control
package expression
