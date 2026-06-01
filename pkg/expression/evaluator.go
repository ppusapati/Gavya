package expression

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Evaluator handles evaluation of rule expressions for validation, visibility, and business logic
type Evaluator interface {
	// EvaluateRule evaluates a rule expression against row/field data
	// Supports: field1 == "value" AND field2 > 100 OR field3 IN (1,2,3)
	EvaluateRule(rule string, data map[string]interface{}) (bool, error)

	// ValidateRuleExpression validates rule syntax without executing
	ValidateRuleExpression(rule string) error

	// EvaluateConditionalVisibility checks if field should be visible (hidden_when condition)
	// Returns true if field should be visible (i.e., NOT hidden)
	EvaluateConditionalVisibility(hiddenWhen string, rowData map[string]interface{}) (bool, error)

	// EvaluateConditionalReadonly checks if field should be readonly (readonly_when condition)
	// Returns true if field should be readonly
	EvaluateConditionalReadonly(readonlyWhen string, rowData map[string]interface{}) (bool, error)

	// EvaluateBusinessRule evaluates a business rule validation
	EvaluateBusinessRule(rule string, fieldValue interface{}, rowData map[string]interface{}) error
}

// evaluatorImpl implements Evaluator
type evaluatorImpl struct{}

// NewEvaluator creates a new rule evaluator instance
func NewEvaluator() Evaluator {
	return &evaluatorImpl{}
}

// Operator represents comparison operators used in expressions
type Operator string

const (
	OpEqual         Operator = "=="
	OpNotEqual      Operator = "!="
	OpLessThan      Operator = "<"
	OpLessThanEq    Operator = "<="
	OpGreaterThan   Operator = ">"
	OpGreaterThanEq Operator = ">="
	OpAnd           Operator = "AND"
	OpOr            Operator = "OR"
	OpIn            Operator = "IN"
	OpNotIn         Operator = "NOT IN"
	OpContains      Operator = "CONTAINS"
	OpStartsWith    Operator = "STARTS_WITH"
	OpEndsWith      Operator = "ENDS_WITH"
	OpMatches       Operator = "MATCHES"
)

// EvaluateRule evaluates a rule expression against data
// Supports: field1 == "value" AND field2 > 100 OR field3 IN (1,2,3)
func (e *evaluatorImpl) EvaluateRule(rule string, data map[string]interface{}) (bool, error) {
	if rule == "" {
		return true, nil
	}

	rule = strings.TrimSpace(rule)

	// Split by OR (lowest precedence)
	orConditions := e.splitByOperator(rule, "OR")
	for _, orPart := range orConditions {
		orResult := true

		// Split by AND (higher precedence)
		andConditions := e.splitByOperator(orPart, "AND")
		for _, andPart := range andConditions {
			result, err := e.evaluateCondition(strings.TrimSpace(andPart), data)
			if err != nil {
				return false, err
			}
			orResult = orResult && result
		}

		if orResult {
			return true, nil
		}
	}

	return false, nil
}

// evaluateCondition evaluates a single condition like "field1 == value"
func (e *evaluatorImpl) evaluateCondition(condition string, data map[string]interface{}) (bool, error) {
	condition = strings.TrimSpace(condition)

	// Extract field, operator, and value
	var fieldID string
	var op Operator
	var value interface{}

	// Try each operator (longest first to avoid partial matches)
	operators := []Operator{
		OpNotEqual, OpLessThanEq, OpGreaterThanEq, OpEqual, OpLessThan, OpGreaterThan,
		OpIn, OpNotIn, OpContains, OpStartsWith, OpEndsWith, OpMatches,
	}

	for _, operator := range operators {
		parts := strings.SplitN(condition, string(operator), 2)
		if len(parts) == 2 {
			fieldID = strings.TrimSpace(parts[0])
			op = operator
			value = strings.TrimSpace(parts[1])
			break
		}
	}

	if fieldID == "" {
		return false, fmt.Errorf("invalid condition: %s", condition)
	}

	// Get field value from data
	fieldValue, exists := data[fieldID]
	if !exists {
		return false, fmt.Errorf("field not found: %s", fieldID)
	}

	return e.compareValues(fieldValue, op, value)
}

// compareValues performs the actual comparison
func (e *evaluatorImpl) compareValues(fieldValue interface{}, op Operator, ruleValue interface{}) (bool, error) {
	ruleStr := fmt.Sprintf("%v", ruleValue)
	ruleStr = strings.Trim(ruleStr, "\"'")

	switch op {
	case OpEqual:
		return e.equals(fieldValue, ruleStr)

	case OpNotEqual:
		result, err := e.equals(fieldValue, ruleStr)
		return !result, err

	case OpLessThan:
		return e.lessThan(fieldValue, ruleStr)

	case OpLessThanEq:
		eq, _ := e.equals(fieldValue, ruleStr)
		lt, _ := e.lessThan(fieldValue, ruleStr)
		return eq || lt, nil

	case OpGreaterThan:
		lt, err := e.lessThan(fieldValue, ruleStr)
		eq, _ := e.equals(fieldValue, ruleStr)
		return !lt && !eq, err

	case OpGreaterThanEq:
		lt, _ := e.lessThan(fieldValue, ruleStr)
		return !lt, nil

	case OpIn:
		return e.in(fieldValue, ruleStr)

	case OpNotIn:
		result, err := e.in(fieldValue, ruleStr)
		return !result, err

	case OpContains:
		return e.contains(fieldValue, ruleStr)

	case OpStartsWith:
		return e.startsWith(fieldValue, ruleStr)

	case OpEndsWith:
		return e.endsWith(fieldValue, ruleStr)

	case OpMatches:
		return e.matches(fieldValue, ruleStr)

	default:
		return false, fmt.Errorf("unknown operator: %s", op)
	}
}

// equals performs equality comparison with case-insensitive string comparison
func (e *evaluatorImpl) equals(fieldValue interface{}, ruleValue string) (bool, error) {
	fStr := fmt.Sprintf("%v", fieldValue)
	return strings.EqualFold(strings.TrimSpace(fStr), ruleValue), nil
}

// lessThan performs numeric less than comparison
func (e *evaluatorImpl) lessThan(fieldValue interface{}, ruleValue string) (bool, error) {
	fNum, err := e.toFloat(fieldValue)
	if err != nil {
		return false, err
	}

	rNum, err := strconv.ParseFloat(ruleValue, 64)
	if err != nil {
		return false, fmt.Errorf("invalid numeric value: %s", ruleValue)
	}

	return fNum < rNum, nil
}

// in checks if value is in comma-separated list
func (e *evaluatorImpl) in(fieldValue interface{}, ruleValue string) (bool, error) {
	values := strings.Split(ruleValue, ",")
	fStr := fmt.Sprintf("%v", fieldValue)

	for _, v := range values {
		if strings.EqualFold(strings.TrimSpace(fStr), strings.TrimSpace(v)) {
			return true, nil
		}
	}
	return false, nil
}

// contains checks if string contains substring
func (e *evaluatorImpl) contains(fieldValue interface{}, ruleValue string) (bool, error) {
	fStr := fmt.Sprintf("%v", fieldValue)
	return strings.Contains(fStr, ruleValue), nil
}

// startsWith checks if string starts with prefix
func (e *evaluatorImpl) startsWith(fieldValue interface{}, ruleValue string) (bool, error) {
	fStr := fmt.Sprintf("%v", fieldValue)
	return strings.HasPrefix(fStr, ruleValue), nil
}

// endsWith checks if string ends with suffix
func (e *evaluatorImpl) endsWith(fieldValue interface{}, ruleValue string) (bool, error) {
	fStr := fmt.Sprintf("%v", fieldValue)
	return strings.HasSuffix(fStr, ruleValue), nil
}

// matches checks if string matches regex pattern
func (e *evaluatorImpl) matches(fieldValue interface{}, ruleValue string) (bool, error) {
	fStr := fmt.Sprintf("%v", fieldValue)
	matched, err := regexp.MatchString(ruleValue, fStr)
	if err != nil {
		return false, fmt.Errorf("invalid regex pattern: %w", err)
	}
	return matched, nil
}

// toFloat converts value to float64
func (e *evaluatorImpl) toFloat(value interface{}) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case int32:
		return float64(v), nil
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, fmt.Errorf("cannot convert to number: %v", value)
		}
		return f, nil
	default:
		return 0, fmt.Errorf("cannot convert to number: %v", value)
	}
}

// splitByOperator splits string by operator while respecting parentheses
func (e *evaluatorImpl) splitByOperator(s string, operator string) []string {
	var parts []string
	var current strings.Builder
	parenDepth := 0

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if ch == '(' {
			parenDepth++
			current.WriteByte(ch)
		} else if ch == ')' {
			parenDepth--
			current.WriteByte(ch)
		} else if parenDepth == 0 && i+len(operator) <= len(s) && s[i:i+len(operator)] == operator {
			if part := strings.TrimSpace(current.String()); part != "" {
				parts = append(parts, part)
			}
			current.Reset()
			i += len(operator) - 1
		} else {
			current.WriteByte(ch)
		}
	}

	if part := strings.TrimSpace(current.String()); part != "" {
		parts = append(parts, part)
	}

	return parts
}

// ValidateRuleExpression validates rule syntax without executing
func (e *evaluatorImpl) ValidateRuleExpression(rule string) error {
	if rule == "" {
		return nil
	}

	// Check for balanced parentheses
	parenCount := 0
	for _, ch := range rule {
		if ch == '(' {
			parenCount++
		} else if ch == ')' {
			parenCount--
		}
		if parenCount < 0 {
			return fmt.Errorf("unbalanced parentheses in rule: %s", rule)
		}
	}
	if parenCount != 0 {
		return fmt.Errorf("unbalanced parentheses in rule: %s", rule)
	}

	// Basic validation - should contain at least one condition
	hasOperator := false
	for _, op := range []string{"==", "!=", "<", "<=", ">", ">=", "IN", "NOT IN", "CONTAINS", "STARTS_WITH", "ENDS_WITH", "MATCHES"} {
		if strings.Contains(rule, op) {
			hasOperator = true
			break
		}
	}

	if !hasOperator {
		return fmt.Errorf("rule must contain at least one operator: %s", rule)
	}

	return nil
}

// EvaluateConditionalVisibility checks if field should be visible (hidden_when condition)
// Returns true if field should be visible (i.e., NOT hidden)
func (e *evaluatorImpl) EvaluateConditionalVisibility(hiddenWhen string, rowData map[string]interface{}) (bool, error) {
	if hiddenWhen == "" {
		return true, nil // visible by default
	}

	hidden, err := e.EvaluateRule(hiddenWhen, rowData)
	if err != nil {
		return true, fmt.Errorf("failed to evaluate hidden_when: %w", err)
	}

	return !hidden, nil // return opposite (visible = not hidden)
}

// EvaluateConditionalReadonly checks if field should be readonly (readonly_when condition)
// Returns true if field should be readonly
func (e *evaluatorImpl) EvaluateConditionalReadonly(readonlyWhen string, rowData map[string]interface{}) (bool, error) {
	if readonlyWhen == "" {
		return false, nil // not readonly by default
	}

	readonly, err := e.EvaluateRule(readonlyWhen, rowData)
	if err != nil {
		return false, fmt.Errorf("failed to evaluate readonly_when: %w", err)
	}

	return readonly, nil
}

// EvaluateBusinessRule evaluates a business rule validation
func (e *evaluatorImpl) EvaluateBusinessRule(rule string, fieldValue interface{}, rowData map[string]interface{}) error {
	if rule == "" {
		return nil
	}

	// Create data context with current field value
	context := make(map[string]interface{})
	for k, v := range rowData {
		context[k] = v
	}
	context["_value"] = fieldValue

	result, err := e.EvaluateRule(rule, context)
	if err != nil {
		return fmt.Errorf("failed to evaluate business rule: %w", err)
	}

	if !result {
		return fmt.Errorf("validation failed: business rule not satisfied")
	}

	return nil
}
