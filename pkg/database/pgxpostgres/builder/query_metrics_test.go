package builder

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Mock metrics provider for testing
type mockMetricsProvider struct {
	dbOperations   []string
	dbRetries      []string
	dbConnections  float64
	httpRequests   []string
	serviceRequests []string
	circuitBreakerStates []string
	circuitBreakerFailures int
	circuitBreakerSuccesses int
}

func (m *mockMetricsProvider) RecordDBOperation(operation string, duration time.Duration, success bool) {
	m.dbOperations = append(m.dbOperations, operation)
}

func (m *mockMetricsProvider) RecordDBRetry(operation string) {
	m.dbRetries = append(m.dbRetries, operation)
}

func (m *mockMetricsProvider) SetDBConnections(count float64) {
	m.dbConnections = count
}

func (m *mockMetricsProvider) RecordHTTPRequest(handler, method string, status int, duration time.Duration) {
	m.httpRequests = append(m.httpRequests, handler+":"+method)
}

func (m *mockMetricsProvider) RecordServiceRequest(service, method string, success bool, duration time.Duration) {
	m.serviceRequests = append(m.serviceRequests, service+":"+method)
}

func (m *mockMetricsProvider) RecordCircuitBreakerState(serviceName string, state string) {
	m.circuitBreakerStates = append(m.circuitBreakerStates, serviceName+":"+state)
}

func (m *mockMetricsProvider) RecordCircuitBreakerFailure(name string) {
	m.circuitBreakerFailures++
}

func (m *mockMetricsProvider) RecordCircuitBreakerSuccess(name string) {
	m.circuitBreakerSuccesses++
}

func (m *mockMetricsProvider) RecordServiceRequestCount(serviceName string) {
	m.serviceRequests = append(m.serviceRequests, serviceName)
}

func (m *mockMetricsProvider) Shutdown(ctx context.Context) error {
	return nil
}

func TestNewQueryMetrics(t *testing.T) {
	provider := &mockMetricsProvider{}

	tests := []struct {
		name   string
		config QueryMetricsConfig
	}{
		{"enabled", QueryMetricsConfig{Enabled: true}},
		{"disabled", QueryMetricsConfig{Enabled: false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qm := NewQueryMetrics(provider, tt.config)
			if qm == nil {
				t.Fatal("expected non-nil QueryMetrics")
			}
			if qm.enabled != tt.config.Enabled {
				t.Errorf("expected enabled=%v, got %v", tt.config.Enabled, qm.enabled)
			}
		})
	}
}

func TestRecordQuery(t *testing.T) {
	provider := &mockMetricsProvider{}
	ctx := context.Background()

	tests := []struct {
		name          string
		config        QueryMetricsConfig
		table         string
		operation     string
		expectRecorded bool
	}{
		{
			name:          "enabled",
			config:        QueryMetricsConfig{Enabled: true},
			table:         "users",
			operation:     "SELECT",
			expectRecorded: true,
		},
		{
			name:          "disabled",
			config:        QueryMetricsConfig{Enabled: false},
			table:         "users",
			operation:     "SELECT",
			expectRecorded: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider.dbOperations = []string{}
			qm := NewQueryMetrics(provider, tt.config)

			qm.RecordQuery(ctx, tt.table, tt.operation, time.Millisecond, true)

			if tt.expectRecorded && len(provider.dbOperations) == 0 {
				t.Error("expected query to be recorded")
			}
			if !tt.expectRecorded && len(provider.dbOperations) > 0 {
				t.Error("expected query not to be recorded")
			}

			if tt.expectRecorded {
				expected := tt.table + "." + tt.operation
				if provider.dbOperations[0] != expected {
					t.Errorf("expected operation label '%s', got '%s'", expected, provider.dbOperations[0])
				}
			}
		})
	}
}

func TestRecordRetry(t *testing.T) {
	provider := &mockMetricsProvider{}
	ctx := context.Background()

	qm := NewQueryMetrics(provider, QueryMetricsConfig{Enabled: true})
	qm.RecordRetry(ctx, "users", "SELECT")

	if len(provider.dbRetries) == 0 {
		t.Error("expected retry to be recorded")
	}

	expected := "users.SELECT"
	if provider.dbRetries[0] != expected {
		t.Errorf("expected retry label '%s', got '%s'", expected, provider.dbRetries[0])
	}
}

func TestWithQueryMetrics(t *testing.T) {
	provider := &mockMetricsProvider{}
	ctx := context.Background()

	// Test with metrics enabled
	qm := NewQueryMetrics(provider, QueryMetricsConfig{Enabled: true})
	SetGlobalQueryMetrics(qm)

	_, err := WithQueryMetrics(ctx, "users", "SELECT", func() (interface{}, error) {
		return "result", nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(provider.dbOperations) == 0 {
		t.Error("expected query to be recorded")
	}

	expected := "users.SELECT"
	if provider.dbOperations[0] != expected {
		t.Errorf("expected operation label '%s', got '%s'", expected, provider.dbOperations[0])
	}

	// Test with error
	provider.dbOperations = []string{}

	testErr := errors.New("query failed")
	_, err = WithQueryMetrics(ctx, "products", "INSERT", func() (interface{}, error) {
		return nil, testErr
	})

	if err != testErr {
		t.Errorf("expected error %v, got %v", testErr, err)
	}

	if len(provider.dbOperations) == 0 {
		t.Error("expected query to be recorded even on error")
	}

	expected = "products.INSERT"
	if provider.dbOperations[0] != expected {
		t.Errorf("expected operation label '%s', got '%s'", expected, provider.dbOperations[0])
	}

	// Test with metrics disabled
	SetGlobalQueryMetrics(nil)
	provider.dbOperations = []string{}

	_, err = WithQueryMetrics(ctx, "orders", "UPDATE", func() (interface{}, error) {
		return "result", nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(provider.dbOperations) > 0 {
		t.Error("expected query not to be recorded when metrics are disabled")
	}
}

func TestWithQueryMetricsAndLogging(t *testing.T) {
	provider := &mockMetricsProvider{}
	logger := &mockLogger{}
	ctx := context.Background()

	// Setup both metrics and logging
	qm := NewQueryMetrics(provider, QueryMetricsConfig{Enabled: true})
	ql := NewQueryLogger(logger, QueryLogConfig{Enabled: true, Verbose: true})
	SetGlobalQueryMetrics(qm)
	SetGlobalQueryLogger(ql)

	query := "SELECT * FROM users WHERE id = $1"
	args := []interface{}{123}

	_, err := WithQueryMetricsAndLogging(ctx, "users", "SELECT", query, args, func() (interface{}, error) {
		return "result", nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Check metrics were recorded
	if len(provider.dbOperations) == 0 {
		t.Error("expected metrics to be recorded")
	}

	// Check logs were recorded
	if logger.infoCount == 0 {
		t.Error("expected query to be logged")
	}

	// Test with error
	provider.dbOperations = []string{}
	logger.infoCount = 0
	logger.errorCount = 0

	testErr := errors.New("query failed")
	_, err = WithQueryMetricsAndLogging(ctx, "products", "DELETE", query, args, func() (interface{}, error) {
		return nil, testErr
	})

	if err != testErr {
		t.Errorf("expected error %v, got %v", testErr, err)
	}

	// Check metrics were recorded
	if len(provider.dbOperations) == 0 {
		t.Error("expected metrics to be recorded even on error")
	}

	// Check error was logged
	if logger.errorCount == 0 {
		t.Error("expected error to be logged")
	}

	// Test with both disabled
	SetGlobalQueryMetrics(nil)
	SetGlobalQueryLogger(nil)
	provider.dbOperations = []string{}
	logger.infoCount = 0

	_, err = WithQueryMetricsAndLogging(ctx, "orders", "UPDATE", query, args, func() (interface{}, error) {
		return "result", nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(provider.dbOperations) > 0 {
		t.Error("expected query not to be recorded when metrics are disabled")
	}

	if logger.infoCount > 0 {
		t.Error("expected query not to be logged when logging is disabled")
	}
}

func TestSetGetGlobalQueryMetrics(t *testing.T) {
	provider := &mockMetricsProvider{}
	qm := NewQueryMetrics(provider, QueryMetricsConfig{Enabled: true})

	SetGlobalQueryMetrics(qm)
	retrieved := GetGlobalQueryMetrics()

	if retrieved != qm {
		t.Error("SetGlobalQueryMetrics/GetGlobalQueryMetrics failed")
	}

	SetGlobalQueryMetrics(nil)
	retrieved = GetGlobalQueryMetrics()

	if retrieved != nil {
		t.Error("expected nil after setting nil metrics")
	}
}

func TestGetStats(t *testing.T) {
	provider := &mockMetricsProvider{}
	qm := NewQueryMetrics(provider, QueryMetricsConfig{Enabled: true})

	stats := qm.GetStats("users", "SELECT")

	if stats == nil {
		t.Error("expected non-nil stats")
	}

	if stats.Table != "users" {
		t.Errorf("expected table 'users', got '%s'", stats.Table)
	}

	if stats.Operation != "SELECT" {
		t.Errorf("expected operation 'SELECT', got '%s'", stats.Operation)
	}
}

// Benchmark tests
func BenchmarkRecordQuery(b *testing.B) {
	provider := &mockMetricsProvider{}
	qm := NewQueryMetrics(provider, QueryMetricsConfig{Enabled: true})
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		qm.RecordQuery(ctx, "users", "SELECT", time.Millisecond, true)
	}
}

func BenchmarkWithQueryMetrics(b *testing.B) {
	provider := &mockMetricsProvider{}
	qm := NewQueryMetrics(provider, QueryMetricsConfig{Enabled: true})
	SetGlobalQueryMetrics(qm)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = WithQueryMetrics(ctx, "users", "SELECT", func() (interface{}, error) {
			return "result", nil
		})
	}
}
