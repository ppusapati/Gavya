package builder

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"p9e.in/samavaya/packages/p9log"
)

// Mock logger for testing
type mockLogger struct {
	infoCount  int
	errorCount int
}

func (m *mockLogger) Log(level p9log.Level, keyvals ...interface{}) error {
	if level == p9log.LevelInfo {
		m.infoCount++
	} else if level == p9log.LevelError {
		m.errorCount++
	}
	return nil
}

func TestNewQueryLogger(t *testing.T) {
	logger := &mockLogger{}

	tests := []struct {
		name   string
		config QueryLogConfig
	}{
		{"enabled", QueryLogConfig{Enabled: true, Verbose: false}},
		{"enabled verbose", QueryLogConfig{Enabled: true, Verbose: true}},
		{"disabled", QueryLogConfig{Enabled: false, Verbose: false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ql := NewQueryLogger(logger, tt.config)
			if ql == nil {
				t.Fatal("expected non-nil QueryLogger")
			}
			if ql.enabled != tt.config.Enabled {
				t.Errorf("expected enabled=%v, got %v", tt.config.Enabled, ql.enabled)
			}
			if ql.verbose != tt.config.Verbose {
				t.Errorf("expected verbose=%v, got %v", tt.config.Verbose, ql.verbose)
			}
		})
	}
}

func TestLogQuery(t *testing.T) {
	logger := &mockLogger{}
	ctx := context.Background()

	tests := []struct {
		name      string
		config    QueryLogConfig
		operation string
		query     string
		args      []interface{}
		expectLog bool
	}{
		{
			name:      "enabled without params",
			config:    QueryLogConfig{Enabled: true, Verbose: false},
			operation: "SELECT",
			query:     "SELECT * FROM users",
			args:      nil,
			expectLog: true,
		},
		{
			name:      "enabled with params",
			config:    QueryLogConfig{Enabled: true, Verbose: true},
			operation: "SELECT",
			query:     "SELECT * FROM users WHERE id = $1",
			args:      []interface{}{123},
			expectLog: true,
		},
		{
			name:      "disabled",
			config:    QueryLogConfig{Enabled: false, Verbose: false},
			operation: "SELECT",
			query:     "SELECT * FROM users",
			args:      nil,
			expectLog: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger.infoCount = 0
			ql := NewQueryLogger(logger, tt.config)

			ql.LogQuery(ctx, tt.operation, tt.query, tt.args, time.Millisecond)

			if tt.expectLog && logger.infoCount == 0 {
				t.Error("expected query to be logged")
			}
			if !tt.expectLog && logger.infoCount > 0 {
				t.Error("expected query not to be logged")
			}
		})
	}
}

func TestLogQueryError(t *testing.T) {
	logger := &mockLogger{}
	ctx := context.Background()

	ql := NewQueryLogger(logger, QueryLogConfig{Enabled: true, Verbose: true})

	testErr := errors.New("query failed")
	ql.LogQueryError(ctx, "SELECT", "SELECT * FROM users", []interface{}{123}, testErr, time.Millisecond)

	if logger.errorCount == 0 {
		t.Error("expected error to be logged")
	}
}

func TestSanitizeValue(t *testing.T) {
	tests := []struct {
		name     string
		value    interface{}
		expected interface{}
	}{
		{"nil", nil, nil},
		{"int", 123, 123},
		{"bool", true, true},
		{"short string", "hello", "hello"},
		{"very long string", strings.Repeat("y", 600), "[REDACTED]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeValue(tt.value)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsSensitive(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		sensitive bool
	}{
		{"short string", "hello", false},
		{"very long", strings.Repeat("b", 600), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSensitive(tt.value)
			if result != tt.sensitive {
				t.Errorf("expected isSensitive=%v, got %v", tt.sensitive, result)
			}
		})
	}
}

func TestContainsSensitivePattern(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		sensitive bool
	}{
		{"password field", "user_password", true},
		{"secret field", "api_secret", true},
		{"token field", "access_token", true},
		{"safe field", "user_name", false},
		{"safe field 2", "email_address", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsSensitivePattern(tt.value)
			if result != tt.sensitive {
				t.Errorf("expected containsSensitivePattern=%v, got %v", tt.sensitive, result)
			}
		})
	}
}

func TestIsBase64Like(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		isBase64 bool
	}{
		{"short string", "abc", false},
		{"base64", "YWJjZGVmZ2hpamtsbW5vcA==", true},
		{"not base64", "hello world 123!", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isBase64Like(tt.value)
			if result != tt.isBase64 {
				t.Errorf("expected isBase64Like=%v, got %v", tt.isBase64, result)
			}
		})
	}
}

func TestWithQueryLogging(t *testing.T) {
	logger := &mockLogger{}
	ctx := context.Background()

	// Test with logging enabled
	ql := NewQueryLogger(logger, QueryLogConfig{Enabled: true, Verbose: true})
	SetGlobalQueryLogger(ql)

	_, err := WithQueryLogging(ctx, "SELECT", "SELECT * FROM users", nil, func() (interface{}, error) {
		return "result", nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if logger.infoCount == 0 {
		t.Error("expected query to be logged")
	}

	// Test with error
	logger.infoCount = 0
	logger.errorCount = 0

	testErr := errors.New("query failed")
	_, err = WithQueryLogging(ctx, "SELECT", "SELECT * FROM users", nil, func() (interface{}, error) {
		return nil, testErr
	})

	if err != testErr {
		t.Errorf("expected error %v, got %v", testErr, err)
	}

	if logger.errorCount == 0 {
		t.Error("expected error to be logged")
	}

	// Test with logging disabled
	SetGlobalQueryLogger(nil)
	logger.infoCount = 0

	_, err = WithQueryLogging(ctx, "SELECT", "SELECT * FROM users", nil, func() (interface{}, error) {
		return "result", nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if logger.infoCount > 0 {
		t.Error("expected query not to be logged when logger is nil")
	}
}

func TestSetGetGlobalQueryLogger(t *testing.T) {
	logger := &mockLogger{}
	ql := NewQueryLogger(logger, QueryLogConfig{Enabled: true})

	SetGlobalQueryLogger(ql)
	retrieved := GetGlobalQueryLogger()

	if retrieved != ql {
		t.Error("SetGlobalQueryLogger/GetGlobalQueryLogger failed")
	}

	SetGlobalQueryLogger(nil)
	retrieved = GetGlobalQueryLogger()

	if retrieved != nil {
		t.Error("expected nil after setting nil logger")
	}
}
