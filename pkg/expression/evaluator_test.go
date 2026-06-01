package expression

import (
	"testing"
)

func TestEvaluator_EvaluateRule(t *testing.T) {
	e := NewEvaluator()

	tests := []struct {
		name    string
		rule    string
		data    map[string]interface{}
		want    bool
		wantErr bool
	}{
		{
			name: "simple equality",
			rule: "status == \"approved\"",
			data: map[string]interface{}{"status": "approved"},
			want: true,
		},
		{
			name: "simple equality false",
			rule: "status == \"approved\"",
			data: map[string]interface{}{"status": "pending"},
			want: false,
		},
		{
			name: "numeric less than",
			rule: "amount < 1000",
			data: map[string]interface{}{"amount": 500},
			want: true,
		},
		{
			name: "numeric less than false",
			rule: "amount < 1000",
			data: map[string]interface{}{"amount": 2000},
			want: false,
		},
		{
			name: "not equal",
			rule: "status != \"rejected\"",
			data: map[string]interface{}{"status": "approved"},
			want: true,
		},
		{
			name: "IN operator",
			rule: "country IN (US,UK,CA)",
			data: map[string]interface{}{"country": "US"},
			want: true,
		},
		{
			name: "IN operator false",
			rule: "country IN (US,UK,CA)",
			data: map[string]interface{}{"country": "DE"},
			want: false,
		},
		{
			name: "NOT IN operator",
			rule: "status NOT IN (rejected,cancelled)",
			data: map[string]interface{}{"status": "approved"},
			want: true,
		},
		{
			name: "CONTAINS operator",
			rule: "description CONTAINS invoice",
			data: map[string]interface{}{"description": "This is an invoice document"},
			want: true,
		},
		{
			name: "STARTS_WITH operator",
			rule: "email STARTS_WITH admin",
			data: map[string]interface{}{"email": "admin@company.com"},
			want: true,
		},
		{
			name: "ENDS_WITH operator",
			rule: "email ENDS_WITH @company.com",
			data: map[string]interface{}{"email": "user@company.com"},
			want: true,
		},
		{
			name: "AND condition true",
			rule: "status == \"approved\" AND amount > 500",
			data: map[string]interface{}{"status": "approved", "amount": 1000},
			want: true,
		},
		{
			name: "AND condition false",
			rule: "status == \"approved\" AND amount > 5000",
			data: map[string]interface{}{"status": "approved", "amount": 1000},
			want: false,
		},
		{
			name: "OR condition true",
			rule: "status == \"approved\" OR amount > 5000",
			data: map[string]interface{}{"status": "approved", "amount": 100},
			want: true,
		},
		{
			name: "OR condition false",
			rule: "status == \"rejected\" OR amount > 5000",
			data: map[string]interface{}{"status": "approved", "amount": 100},
			want: false,
		},
		{
			name: "complex AND OR",
			rule: "status == \"pending\" AND (amount > 1000 OR priority == \"high\")",
			data: map[string]interface{}{"status": "pending", "amount": 500, "priority": "high"},
			want: true,
		},
		{
			name: "greater than or equal",
			rule: "amount >= 1000",
			data: map[string]interface{}{"amount": 1000},
			want: true,
		},
		{
			name: "less than or equal",
			rule: "amount <= 1000",
			data: map[string]interface{}{"amount": 1000},
			want: true,
		},
		{
			name: "empty rule defaults to true",
			rule: "",
			data: map[string]interface{}{},
			want: true,
		},
		{
			name: "missing field error",
			rule: "status == \"approved\"",
			data: map[string]interface{}{},
			wantErr: true,
		},
		{
			name: "MATCHES regex",
			rule: "email MATCHES ^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$",
			data: map[string]interface{}{"email": "test@example.com"},
			want: true,
		},
		{
			name: "MATCHES regex false",
			rule: "email MATCHES ^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$",
			data: map[string]interface{}{"email": "invalid-email"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := e.EvaluateRule(tt.rule, tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("EvaluateRule() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("EvaluateRule() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvaluator_ValidateRuleExpression(t *testing.T) {
	e := NewEvaluator()

	tests := []struct {
		name    string
		rule    string
		wantErr bool
	}{
		{
			name: "valid simple rule",
			rule: "status == \"approved\"",
		},
		{
			name: "valid complex rule",
			rule: "status == \"pending\" AND (amount > 1000 OR priority == \"high\")",
		},
		{
			name: "unbalanced opening paren",
			rule: "status == \"approved\"(AND amount > 1000",
			wantErr: true,
		},
		{
			name: "unbalanced closing paren",
			rule: "status == \"approved\") AND amount > 1000",
			wantErr: true,
		},
		{
			name: "no operator",
			rule: "status",
			wantErr: true,
		},
		{
			name: "empty rule",
			rule: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := e.ValidateRuleExpression(tt.rule)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRuleExpression() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEvaluator_ConditionalVisibility(t *testing.T) {
	e := NewEvaluator()

	tests := []struct {
		name      string
		hiddenWhen string
		data      map[string]interface{}
		want      bool
		wantErr   bool
	}{
		{
			name:      "visible when hidden_when is false",
			hiddenWhen: "status == \"rejected\"",
			data:      map[string]interface{}{"status": "approved"},
			want:      true,
		},
		{
			name:      "hidden when hidden_when is true",
			hiddenWhen: "status == \"rejected\"",
			data:      map[string]interface{}{"status": "rejected"},
			want:      false,
		},
		{
			name:      "visible by default when hidden_when is empty",
			hiddenWhen: "",
			data:      map[string]interface{}{},
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := e.EvaluateConditionalVisibility(tt.hiddenWhen, tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("EvaluateConditionalVisibility() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("EvaluateConditionalVisibility() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvaluator_ConditionalReadonly(t *testing.T) {
	e := NewEvaluator()

	tests := []struct {
		name         string
		readonlyWhen string
		data         map[string]interface{}
		want         bool
		wantErr      bool
	}{
		{
			name:         "readonly when condition is true",
			readonlyWhen: "status == \"approved\"",
			data:         map[string]interface{}{"status": "approved"},
			want:         true,
		},
		{
			name:         "not readonly when condition is false",
			readonlyWhen: "status == \"approved\"",
			data:         map[string]interface{}{"status": "pending"},
			want:         false,
		},
		{
			name:         "not readonly by default when readonlyWhen is empty",
			readonlyWhen: "",
			data:         map[string]interface{}{},
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := e.EvaluateConditionalReadonly(tt.readonlyWhen, tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("EvaluateConditionalReadonly() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("EvaluateConditionalReadonly() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvaluator_GridRowValidation(t *testing.T) {
	// Test grid row validation use case
	e := NewEvaluator()

	tests := []struct {
		name    string
		rule    string
		row     map[string]interface{}
		want    bool
		wantErr bool
	}{
		{
			name: "quantity and price total validation",
			rule: "quantity > 0 AND unit_price > 0",
			row:  map[string]interface{}{"quantity": 10, "unit_price": 50.00},
			want: true,
		},
		{
			name: "discount must be less than subtotal",
			rule: "discount < subtotal",
			row:  map[string]interface{}{"discount": 100, "subtotal": 500},
			want: true,
		},
		{
			name: "tax percentage valid range",
			rule: "tax_percent >= 0 AND tax_percent <= 100",
			row:  map[string]interface{}{"tax_percent": 18},
			want: true,
		},
		{
			name: "either qty or rate must be present",
			rule: "quantity > 0 OR rate > 0",
			row:  map[string]interface{}{"quantity": 0, "rate": 100},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := e.EvaluateRule(tt.rule, tt.row)
			if (err != nil) != tt.wantErr {
				t.Errorf("EvaluateRule() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("EvaluateRule() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvaluator_SearchCriteria(t *testing.T) {
	sc := &SearchCriteria{
		PageSize:   20,
		PageOffset: 0,
		Sort:       []string{"created_at"},
		Filters: []Filter{
			{
				Field:    "status",
				Operator: OperatorEquals,
				Value:    "active",
			},
		},
	}

	if sc.GetPageSize() != 20 {
		t.Errorf("GetPageSize() = %d, want 20", sc.GetPageSize())
	}

	if !sc.HasSort() {
		t.Error("HasSort() = false, want true")
	}

	if !sc.HasFilters() {
		t.Error("HasFilters() = false, want true")
	}

	if sc.IsSortDescending() {
		t.Error("IsSortDescending() = true, want false")
	}
}
