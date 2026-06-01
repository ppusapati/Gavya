package builder

import (
	"testing"

	"p9e.in/samavaya/packages/models"
)

// Test entity with db tags
type User struct {
	ID        int64  `db:"id"`
	Name      string `db:"name"`
	Email     string `db:"email"`
	CreatedAt string `db:"created_at"`
	UpdatedAt string `db:"updated_at"`
}

// Test entity without db tags (uses lowercase field names)
type Product struct {
	ID          int64
	ProductName string
	Price       float64
}

func TestNewTypedQuery(t *testing.T) {
	tq := NewTypedQuery[User]("users")

	if tq.tableName != "users" {
		t.Errorf("expected tableName 'users', got '%s'", tq.tableName)
	}

	expectedCols := []string{"created_at", "email", "id", "name", "updated_at"}
	if len(tq.columns) != len(expectedCols) {
		t.Errorf("expected %d columns, got %d", len(expectedCols), len(tq.columns))
	}

	for i, col := range expectedCols {
		if tq.columns[i] != col {
			t.Errorf("expected column[%d] = '%s', got '%s'", i, col, tq.columns[i])
		}
	}
}

func TestNewTypedQuery_NoDBTags(t *testing.T) {
	tq := NewTypedQuery[Product]("products")

	expectedCols := []string{"id", "price", "productname"}
	if len(tq.columns) != len(expectedCols) {
		t.Errorf("expected %d columns, got %d", len(expectedCols), len(tq.columns))
	}

	for i, col := range expectedCols {
		if tq.columns[i] != col {
			t.Errorf("expected column[%d] = '%s', got '%s'", i, col, tq.columns[i])
		}
	}
}

func TestValidateFieldMask_Valid(t *testing.T) {
	tq := NewTypedQuery[User]("users")

	tests := []struct {
		name  string
		paths []string
	}{
		{"single field", []string{"name"}},
		{"multiple fields", []string{"id", "name", "email"}},
		{"all fields", []string{"id", "name", "email", "created_at", "updated_at"}},
		{"empty paths", []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tq.ValidateFieldMask(tt.paths)
			if err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}

func TestValidateFieldMask_Invalid(t *testing.T) {
	tq := NewTypedQuery[User]("users")

	tests := []struct {
		name  string
		paths []string
	}{
		{"single invalid", []string{"invalid_field"}},
		{"mixed valid/invalid", []string{"name", "invalid_field"}},
		{"multiple invalid", []string{"foo", "bar"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tq.ValidateFieldMask(tt.paths)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestValidateFieldMask_WithTablePrefix(t *testing.T) {
	tq := NewTypedQuery[User]("users")

	tests := []struct {
		name  string
		paths []string
		valid bool
	}{
		{"valid with prefix", []string{"users.name", "users.email"}, true},
		{"mixed prefix/no prefix", []string{"users.name", "email"}, true},
		{"invalid with prefix", []string{"users.invalid_field"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tq.ValidateFieldMask(tt.paths)
			if tt.valid && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
			if !tt.valid && err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestValidateDataModel(t *testing.T) {
	tq := NewTypedQuery[User]("users")

	tests := []struct {
		name   string
		dm     models.DataModel[User]
		valid  bool
	}{
		{
			name: "valid fields",
			dm: models.DataModel[User]{
				TableName:  "users",
				FieldNames: []string{"id", "name", "email"},
			},
			valid: true,
		},
		{
			name: "invalid fields",
			dm: models.DataModel[User]{
				TableName:  "users",
				FieldNames: []string{"id", "invalid_field"},
			},
			valid: false,
		},
		{
			name: "no fields (SELECT *)",
			dm: models.DataModel[User]{
				TableName:  "users",
				FieldNames: []string{},
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tq.ValidateDataModel(tt.dm)
			if tt.valid && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
			if !tt.valid && err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestIsValidColumn(t *testing.T) {
	tq := NewTypedQuery[User]("users")

	tests := []struct {
		col   string
		valid bool
	}{
		{"id", true},
		{"name", true},
		{"email", true},
		{"created_at", true},
		{"updated_at", true},
		{"invalid", false},
		{"foo", false},
	}

	for _, tt := range tests {
		t.Run(tt.col, func(t *testing.T) {
			valid := tq.IsValidColumn(tt.col)
			if valid != tt.valid {
				t.Errorf("expected IsValidColumn('%s') = %v, got %v", tt.col, tt.valid, valid)
			}
		})
	}
}

func TestGetValidColumns(t *testing.T) {
	tq := NewTypedQuery[User]("users")

	cols := tq.GetValidColumns()
	expectedCols := []string{"created_at", "email", "id", "name", "updated_at"}

	if len(cols) != len(expectedCols) {
		t.Errorf("expected %d columns, got %d", len(expectedCols), len(cols))
	}

	for i, col := range expectedCols {
		if cols[i] != col {
			t.Errorf("expected column[%d] = '%s', got '%s'", i, col, cols[i])
		}
	}
}

func TestSuggestColumn(t *testing.T) {
	tq := NewTypedQuery[User]("users")

	tests := []struct {
		invalid     string
		expectFirst string
	}{
		{"emal", "email"},           // typo
		{"nam", "name"},              // typo
		{"create_at", "created_at"},  // typo
		{"iddd", "id"},               // extra chars
	}

	for _, tt := range tests {
		t.Run(tt.invalid, func(t *testing.T) {
			suggestions := tq.SuggestColumn(tt.invalid)
			if len(suggestions) == 0 {
				t.Errorf("expected suggestions for '%s', got none", tt.invalid)
				return
			}
			if suggestions[0] != tt.expectFirst {
				t.Errorf("expected first suggestion '%s', got '%s'", tt.expectFirst, suggestions[0])
			}
		})
	}
}

func TestLevenshteinDistance(t *testing.T) {
	tests := []struct {
		s1       string
		s2       string
		expected int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"kitten", "sitting", 3},
		{"saturday", "sunday", 3},
	}

	for _, tt := range tests {
		t.Run(tt.s1+"_"+tt.s2, func(t *testing.T) {
			dist := levenshteinDistance(tt.s1, tt.s2)
			if dist != tt.expected {
				t.Errorf("levenshteinDistance('%s', '%s') = %d, expected %d",
					tt.s1, tt.s2, dist, tt.expected)
			}
		})
	}
}

func TestStripTablePrefix(t *testing.T) {
	tests := []struct {
		path      string
		tableName string
		expected  string
	}{
		{"user.name", "user", "name"},
		{"name", "user", "name"},
		{"users.email", "users", "email"},
		{"email", "users", "email"},
		{"other.field", "user", "other.field"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := stripTablePrefix(tt.path, tt.tableName)
			if result != tt.expected {
				t.Errorf("stripTablePrefix('%s', '%s') = '%s', expected '%s'",
					tt.path, tt.tableName, result, tt.expected)
			}
		})
	}
}

// Benchmark tests
func BenchmarkNewTypedQuery(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = NewTypedQuery[User]("users")
	}
}

func BenchmarkValidateFieldMask(b *testing.B) {
	tq := NewTypedQuery[User]("users")
	paths := []string{"id", "name", "email"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tq.ValidateFieldMask(paths)
	}
}

func BenchmarkIsValidColumn(b *testing.B) {
	tq := NewTypedQuery[User]("users")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tq.IsValidColumn("name")
	}
}
