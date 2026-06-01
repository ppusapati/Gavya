package schema_test

import (
	"testing"

	"p9e.in/samavaya/packages/database/pgxpostgres/schema"
)

func TestNewTableSchema(t *testing.T) {
	columns := []string{"id", "name", "email", "created_at"}
	ts := schema.NewTableSchema("users", columns)

	if ts.TableName != "users" {
		t.Errorf("Expected table name 'users', got '%s'", ts.TableName)
	}

	for _, col := range columns {
		if !ts.HasColumn(col) {
			t.Errorf("Expected column '%s' to exist", col)
		}
	}
}

func TestValidateColumns_Valid(t *testing.T) {
	ts := schema.NewTableSchema("users", []string{"id", "name", "email", "created_at"})

	tests := []struct {
		name    string
		columns []string
	}{
		{"single column", []string{"id"}},
		{"multiple columns", []string{"id", "name", "email"}},
		{"all columns", []string{"id", "name", "email", "created_at"}},
		{"with wildcard", []string{"*"}},
		{"wildcard with columns", []string{"*", "id"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ts.ValidateColumns(tt.columns)
			if err != nil {
				t.Errorf("ValidateColumns() error = %v, want nil", err)
			}
		})
	}
}

func TestValidateColumns_Invalid(t *testing.T) {
	ts := schema.NewTableSchema("users", []string{"id", "name", "email", "created_at"})

	tests := []struct {
		name    string
		columns []string
		wantErr string
	}{
		{
			"single invalid column",
			[]string{"invalid"},
			"invalid column(s) 'invalid' for table 'users'",
		},
		{
			"multiple invalid columns",
			[]string{"invalid1", "invalid2"},
			"invalid column(s) 'invalid1, invalid2' for table 'users'",
		},
		{
			"mix of valid and invalid",
			[]string{"id", "invalid", "name"},
			"invalid column(s) 'invalid' for table 'users'",
		},
		{
			"typo in column name",
			[]string{"idd"},
			"invalid column(s) 'idd' for table 'users'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ts.ValidateColumns(tt.columns)
			if err == nil {
				t.Error("ValidateColumns() error = nil, want error")
				return
			}
			if !contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidateColumns() error = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestRegisterAlias(t *testing.T) {
	ts := schema.NewTableSchema("users", []string{"id", "name", "email"})

	tests := []struct {
		name        string
		alias       string
		actualCol   string
		shouldError bool
	}{
		{"valid alias", "user_id", "id", false},
		{"valid alias 2", "full_name", "name", false},
		{"invalid actual column", "alias", "nonexistent", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ts.RegisterAlias(tt.alias, tt.actualCol)
			if (err != nil) != tt.shouldError {
				t.Errorf("RegisterAlias() error = %v, shouldError = %v", err, tt.shouldError)
			}

			if !tt.shouldError {
				// Verify alias works in validation
				err := ts.ValidateColumns([]string{tt.alias})
				if err != nil {
					t.Errorf("ValidateColumns() with alias error = %v, want nil", err)
				}
			}
		})
	}
}

func TestResolveColumn(t *testing.T) {
	ts := schema.NewTableSchema("users", []string{"id", "name", "email"})
	ts.RegisterAlias("user_id", "id")
	ts.RegisterAlias("full_name", "name")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"direct column", "id", "id"},
		{"aliased column", "user_id", "id"},
		{"another alias", "full_name", "name"},
		{"non-aliased column", "email", "email"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ts.ResolveColumn(tt.input)
			if result != tt.expected {
				t.Errorf("ResolveColumn(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestAddColumn(t *testing.T) {
	ts := schema.NewTableSchema("users", []string{"id", "name"})

	// Initially should not have 'email'
	if ts.HasColumn("email") {
		t.Error("Expected 'email' column to not exist initially")
	}

	// Add column
	ts.AddColumn("email")

	// Now should have 'email'
	if !ts.HasColumn("email") {
		t.Error("Expected 'email' column to exist after AddColumn()")
	}

	// Validation should succeed
	err := ts.ValidateColumns([]string{"id", "name", "email"})
	if err != nil {
		t.Errorf("ValidateColumns() error = %v, want nil", err)
	}
}

func TestRemoveColumn(t *testing.T) {
	ts := schema.NewTableSchema("users", []string{"id", "name", "email"})
	ts.RegisterAlias("user_email", "email")

	// Initially should have 'email'
	if !ts.HasColumn("email") {
		t.Error("Expected 'email' column to exist initially")
	}

	// Remove column
	ts.RemoveColumn("email")

	// Now should not have 'email'
	if ts.HasColumn("email") {
		t.Error("Expected 'email' column to not exist after RemoveColumn()")
	}

	// Alias should also be removed
	if ts.HasColumn("user_email") {
		t.Error("Expected alias 'user_email' to be removed when column is removed")
	}

	// Validation should fail
	err := ts.ValidateColumns([]string{"email"})
	if err == nil {
		t.Error("ValidateColumns() error = nil, want error for removed column")
	}
}

func TestGetValidColumnsList(t *testing.T) {
	ts := schema.NewTableSchema("users", []string{"id", "name", "email", "created_at"})
	columns := ts.GetValidColumnsList()

	// Should be sorted
	expected := []string{"created_at", "email", "id", "name"}
	if len(columns) != len(expected) {
		t.Errorf("GetValidColumnsList() length = %d, want %d", len(columns), len(expected))
	}

	for i, col := range columns {
		if col != expected[i] {
			t.Errorf("GetValidColumnsList()[%d] = %q, want %q", i, col, expected[i])
		}
	}
}

func TestSchemaRegistry_RegisterAndGet(t *testing.T) {
	// Clean registry before test
	schema.ClearRegistry()

	// Register schema
	ts := schema.NewTableSchema("users", []string{"id", "name", "email"})
	schema.RegisterSchema("users", ts)

	// Retrieve schema
	retrieved := schema.GetSchema("users")
	if retrieved == nil {
		t.Fatal("GetSchema() returned nil, want schema")
	}

	if retrieved.TableName != "users" {
		t.Errorf("Retrieved schema table name = %q, want %q", retrieved.TableName, "users")
	}
}

func TestSchemaRegistry_GetNonExistent(t *testing.T) {
	schema.ClearRegistry()

	// Try to get non-existent schema
	retrieved := schema.GetSchema("nonexistent")
	if retrieved != nil {
		t.Error("GetSchema() for non-existent table should return nil")
	}
}

func TestValidateTableColumns_WithRegisteredSchema(t *testing.T) {
	schema.ClearRegistry()

	// Register schema
	ts := schema.NewTableSchema("users", []string{"id", "name", "email"})
	schema.RegisterSchema("users", ts)

	// Valid columns
	err := schema.ValidateTableColumns("users", []string{"id", "name"})
	if err != nil {
		t.Errorf("ValidateTableColumns() error = %v, want nil", err)
	}

	// Invalid columns
	err = schema.ValidateTableColumns("users", []string{"id", "invalid"})
	if err == nil {
		t.Error("ValidateTableColumns() error = nil, want error for invalid column")
	}
}

func TestValidateTableColumns_WithoutRegisteredSchema(t *testing.T) {
	schema.ClearRegistry()

	// Validate against non-registered schema - should succeed (graceful degradation)
	err := schema.ValidateTableColumns("nonexistent", []string{"any", "columns"})
	if err != nil {
		t.Errorf("ValidateTableColumns() with unregistered schema error = %v, want nil (graceful degradation)", err)
	}
}

func TestListRegisteredTables(t *testing.T) {
	schema.ClearRegistry()

	// Initially empty
	tables := schema.ListRegisteredTables()
	if len(tables) != 0 {
		t.Errorf("ListRegisteredTables() length = %d, want 0", len(tables))
	}

	// Register schemas
	schema.RegisterSchema("users", schema.NewTableSchema("users", []string{"id"}))
	schema.RegisterSchema("posts", schema.NewTableSchema("posts", []string{"id"}))
	schema.RegisterSchema("comments", schema.NewTableSchema("comments", []string{"id"}))

	// Should be sorted
	tables = schema.ListRegisteredTables()
	expected := []string{"comments", "posts", "users"}
	if len(tables) != len(expected) {
		t.Errorf("ListRegisteredTables() length = %d, want %d", len(tables), len(expected))
	}

	for i, table := range tables {
		if table != expected[i] {
			t.Errorf("ListRegisteredTables()[%d] = %q, want %q", i, table, expected[i])
		}
	}
}

func TestConcurrentAccess(t *testing.T) {
	schema.ClearRegistry()

	ts := schema.NewTableSchema("users", []string{"id", "name", "email"})
	schema.RegisterSchema("users", ts)

	// Test concurrent reads
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = ts.ValidateColumns([]string{"id", "name"})
				_ = ts.HasColumn("email")
				_ = ts.GetValidColumnsList()
				_ = schema.GetSchema("users")
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestConcurrentWrites(t *testing.T) {
	schema.ClearRegistry()

	ts := schema.NewTableSchema("users", []string{"id", "name"})

	// Test concurrent writes
	done := make(chan bool)
	for i := 0; i < 5; i++ {
		go func(n int) {
			colName := string(rune('a' + n))
			ts.AddColumn(colName)
			ts.RegisterAlias("alias_"+colName, colName)
			done <- true
		}(i)
	}

	for i := 0; i < 5; i++ {
		<-done
	}

	// Verify all columns were added
	for i := 0; i < 5; i++ {
		colName := string(rune('a' + i))
		if !ts.HasColumn(colName) {
			t.Errorf("Expected column '%s' to exist after concurrent add", colName)
		}
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsHelper(s, substr)))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
