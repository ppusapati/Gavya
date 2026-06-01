package metrics

import (
	"context"
	"p9e.in/samavaya/packages/api/v1/config"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// NewProvider Tests
// =============================================================================

func TestNewProvider_DisabledMetrics(t *testing.T) {
	cfg := &config.Observability{
		Metrics: &config.Observability_Metrics{
			Enabled: false,
		},
	}

	provider, err := NewProvider(cfg)

	assert.NoError(t, err)
	assert.NotNil(t, provider)
	assert.IsType(t, &noopMetricsProvider{}, provider)
}

// TestNewProvider_PrometheusProvider - Combined with AllMethods test to avoid registry conflicts
// Prometheus uses global registration via promauto which conflicts across tests

func TestNewProvider_OpenTelemetryProvider(t *testing.T) {
	cfg := &config.Observability{
		ServiceName: &config.ServiceName{
			ServiceName: "test-otel-service",
		},
		Metrics: &config.Observability_Metrics{
			Enabled:  true,
			Provider: config.Observability_Metrics_OPENTELEMETRY,
		},
	}

	provider, err := NewProvider(cfg)

	assert.NoError(t, err)
	assert.NotNil(t, provider)
	assert.IsType(t, &OpenTelemetryProvider{}, provider)

	otelProvider := provider.(*OpenTelemetryProvider)
	assert.NotNil(t, otelProvider.meter)
	assert.NotNil(t, otelProvider.dbOperationDuration)
}

func TestNewProvider_DatadogProvider(t *testing.T) {
	cfg := &config.Observability{
		ServiceName: &config.ServiceName{
			ServiceName: "test-datadog-service",
		},
		Metrics: &config.Observability_Metrics{
			Enabled:  true,
			Provider: config.Observability_Metrics_DATADOG,
			Endpoint: "localhost",
			Port:     8125,
		},
	}

	provider, err := NewProvider(cfg)

	// Datadog may fail if StatsD isn't running, but we verify creation logic
	if err == nil {
		assert.NotNil(t, provider)
		assert.IsType(t, &DatadogProvider{}, provider)

		ddProvider := provider.(*DatadogProvider)
		assert.Equal(t, "test-datadog-service", ddProvider.serviceName)
		assert.NotNil(t, ddProvider.client)
		assert.NotNil(t, ddProvider.dbOperationCount)

		// Clean up
		_ = ddProvider.Shutdown(context.Background())
	}
}

func TestNewProvider_DefaultServiceName(t *testing.T) {
	// Test with OpenTelemetry to avoid Prometheus registry conflicts
	cfg := &config.Observability{
		ServiceName: &config.ServiceName{
			ServiceName: "", // Empty service name
		},
		Metrics: &config.Observability_Metrics{
			Enabled:  true,
			Provider: config.Observability_Metrics_OPENTELEMETRY,
		},
	}

	provider, err := NewProvider(cfg)

	assert.NoError(t, err)
	otelProvider := provider.(*OpenTelemetryProvider)
	assert.NotNil(t, otelProvider)
	// Service name is used in meter creation but not exposed
}

func TestNewProvider_UnsupportedProvider(t *testing.T) {
	cfg := &config.Observability{
		ServiceName: &config.ServiceName{
			ServiceName: "test",
		},
		Metrics: &config.Observability_Metrics{
			Enabled:  true,
			Provider: 999, // Invalid provider
		},
	}

	provider, err := NewProvider(cfg)

	assert.Error(t, err)
	assert.NotNil(t, provider)
	assert.IsType(t, &noopMetricsProvider{}, provider)
	assert.Contains(t, err.Error(), "unsupported metrics provider")
}

// =============================================================================
// PrometheusProvider Tests
// =============================================================================

func TestPrometheusProvider_AllMethods(t *testing.T) {
	// Create provider via NewProvider factory to test both factory and methods
	cfg := &config.Observability{
		ServiceName: &config.ServiceName{
			ServiceName: "test-prom-service",
		},
		Metrics: &config.Observability_Metrics{
			Enabled:  true,
			Provider: config.Observability_Metrics_PROMETHEUS,
			Port:     9091,
		},
	}

	providerInterface, err := NewProvider(cfg)
	assert.NoError(t, err)
	assert.NotNil(t, providerInterface)
	assert.IsType(t, &PrometheusProvider{}, providerInterface)

	provider := providerInterface.(*PrometheusProvider)
	assert.Equal(t, "test-prom-service", provider.serviceName)
	assert.NotNil(t, provider.registration)
	assert.NotNil(t, provider.dbOperationDuration)

	// Test RecordDBOperation - successful operation
	provider.RecordDBOperation("SELECT", 100*time.Millisecond, true)
	// Test failed operation
	provider.RecordDBOperation("INSERT", 50*time.Millisecond, false)
	assert.NotNil(t, provider.dbOperationDuration)

	// Test RecordDBRetry
	provider.RecordDBRetry("SELECT")
	provider.RecordDBRetry("UPDATE")
	assert.NotNil(t, provider.dbOperationRetries)

	// Test SetDBConnections
	provider.SetDBConnections(10)
	provider.SetDBConnections(25)
	provider.SetDBConnections(5)
	assert.NotNil(t, provider.dbConnectionsOpen)

	// Test RecordHTTPRequest
	provider.RecordHTTPRequest("GetUser", "GET", 200, 75*time.Millisecond)
	provider.RecordHTTPRequest("CreateUser", "POST", 201, 120*time.Millisecond)
	provider.RecordHTTPRequest("GetUser", "GET", 404, 10*time.Millisecond)
	assert.NotNil(t, provider.httpRequestDuration)

	// Test RecordCircuitBreakerState
	provider.RecordCircuitBreakerState("auth-service", "open")
	provider.RecordCircuitBreakerState("auth-service", "half-open")
	provider.RecordCircuitBreakerState("payment-service", "closed")
	assert.NotNil(t, provider.circuitBreakerState)

	// Test RecordCircuitBreakerFailure
	provider.RecordCircuitBreakerFailure("auth-service")
	provider.RecordCircuitBreakerFailure("payment-service")
	assert.NotNil(t, provider.circuitBreakerFailure)

	// Test RecordCircuitBreakerSuccess
	provider.RecordCircuitBreakerSuccess("auth-service")
	provider.RecordCircuitBreakerSuccess("auth-service")
	provider.RecordCircuitBreakerSuccess("payment-service")
	assert.NotNil(t, provider.circuitBreakerSuccess)

	// Test RecordServiceRequestCount
	provider.RecordServiceRequestCount("user-service")
	provider.RecordServiceRequestCount("order-service")
	provider.RecordServiceRequestCount("user-service")
	assert.NotNil(t, provider.serviceRequestCount)

	// Test Shutdown
	shutdownErr := provider.Shutdown(context.Background())
	assert.NoError(t, shutdownErr)
}


// =============================================================================
// OpenTelemetryProvider Tests
// =============================================================================

func TestOpenTelemetryProvider_RecordDBOperation(t *testing.T) {
	provider := createTestOpenTelemetryProvider(t)

	provider.RecordDBOperation("SELECT", 100*time.Millisecond, true)
	provider.RecordDBOperation("INSERT", 50*time.Millisecond, false)

	assert.NotNil(t, provider.dbOperationDuration)
}

func TestOpenTelemetryProvider_RecordDBRetry(t *testing.T) {
	provider := createTestOpenTelemetryProvider(t)

	provider.RecordDBRetry("SELECT")
	provider.RecordDBRetry("UPDATE")

	assert.NotNil(t, provider.dbOperationRetries)
}

func TestOpenTelemetryProvider_SetDBConnections(t *testing.T) {
	provider := createTestOpenTelemetryProvider(t)

	provider.SetDBConnections(15)
	provider.SetDBConnections(30)

	assert.NotNil(t, provider.dbConnectionsOpen)
}

func TestOpenTelemetryProvider_RecordHTTPRequest(t *testing.T) {
	provider := createTestOpenTelemetryProvider(t)

	provider.RecordHTTPRequest("GetUser", "GET", 200, 75*time.Millisecond)
	provider.RecordHTTPRequest("CreateUser", "POST", 500, 200*time.Millisecond)

	assert.NotNil(t, provider.httpRequestDuration)
}

func TestOpenTelemetryProvider_RecordCircuitBreakerState(t *testing.T) {
	provider := createTestOpenTelemetryProvider(t)

	provider.RecordCircuitBreakerState("auth-service", "open")
	provider.RecordCircuitBreakerState("payment-service", "closed")

	assert.NotNil(t, provider.circuitBreakerState)
}

func TestOpenTelemetryProvider_RecordCircuitBreakerFailure(t *testing.T) {
	provider := createTestOpenTelemetryProvider(t)

	provider.RecordCircuitBreakerFailure("auth-service")
	provider.RecordCircuitBreakerFailure("payment-service")

	assert.NotNil(t, provider.circuitBreakerFailure)
}

func TestOpenTelemetryProvider_RecordCircuitBreakerSuccess(t *testing.T) {
	provider := createTestOpenTelemetryProvider(t)

	provider.RecordCircuitBreakerSuccess("auth-service")
	provider.RecordCircuitBreakerSuccess("payment-service")

	assert.NotNil(t, provider.circuitBreakerSuccess)
}

func TestOpenTelemetryProvider_RecordServiceRequestCount(t *testing.T) {
	provider := createTestOpenTelemetryProvider(t)

	provider.RecordServiceRequestCount("user-service")
	provider.RecordServiceRequestCount("order-service")

	assert.NotNil(t, provider.serviceRequestCount)
}

func TestOpenTelemetryProvider_Shutdown(t *testing.T) {
	provider := createTestOpenTelemetryProvider(t)

	err := provider.Shutdown(context.Background())

	assert.NoError(t, err)
}

// =============================================================================
// DatadogProvider Tests (with mock client simulation)
// =============================================================================

func TestDatadogProvider_RecordDBOperation(t *testing.T) {
	// Skip if StatsD not available
	provider, err := createTestDatadogProvider(t)
	if err != nil {
		t.Skip("Datadog StatsD not available")
	}
	defer provider.Shutdown(context.Background())

	provider.RecordDBOperation("SELECT", 100*time.Millisecond, true)
	provider.RecordDBOperation("INSERT", 50*time.Millisecond, false)

	assert.Contains(t, provider.dbOperationCount, "SELECT")
	assert.Equal(t, int64(1), provider.dbOperationCount["SELECT"])
}

func TestDatadogProvider_RecordDBRetry(t *testing.T) {
	provider, err := createTestDatadogProvider(t)
	if err != nil {
		t.Skip("Datadog StatsD not available")
	}
	defer provider.Shutdown(context.Background())

	provider.RecordDBRetry("SELECT")
	provider.RecordDBRetry("UPDATE")

	// No panics expected
	assert.NotNil(t, provider.client)
}

func TestDatadogProvider_SetDBConnections(t *testing.T) {
	provider, err := createTestDatadogProvider(t)
	if err != nil {
		t.Skip("Datadog StatsD not available")
	}
	defer provider.Shutdown(context.Background())

	provider.SetDBConnections(20)

	assert.Equal(t, 20.0, provider.dbConnections)
}

func TestDatadogProvider_RecordHTTPRequest(t *testing.T) {
	provider, err := createTestDatadogProvider(t)
	if err != nil {
		t.Skip("Datadog StatsD not available")
	}
	defer provider.Shutdown(context.Background())

	provider.RecordHTTPRequest("GetUser", "GET", 200, 75*time.Millisecond)
	provider.RecordHTTPRequest("CreateUser", "POST", 201, 120*time.Millisecond)

	assert.NotNil(t, provider.client)
}

func TestDatadogProvider_RecordCircuitBreakerState(t *testing.T) {
	provider, err := createTestDatadogProvider(t)
	if err != nil {
		t.Skip("Datadog StatsD not available")
	}
	defer provider.Shutdown(context.Background())

	provider.RecordCircuitBreakerState("auth-service", "open")
	provider.RecordCircuitBreakerState("auth-service", "half-open")

	assert.Contains(t, provider.circuitBreakerState, "auth-service")
	assert.Equal(t, int64(2), provider.circuitBreakerState["auth-service"])
}

func TestDatadogProvider_RecordCircuitBreakerFailure(t *testing.T) {
	provider, err := createTestDatadogProvider(t)
	if err != nil {
		t.Skip("Datadog StatsD not available")
	}
	defer provider.Shutdown(context.Background())

	provider.RecordCircuitBreakerFailure("auth-service")
	provider.RecordCircuitBreakerFailure("auth-service")

	assert.Contains(t, provider.circuitBreakerFailure, "auth-service")
	assert.Equal(t, int64(2), provider.circuitBreakerFailure["auth-service"])
}

func TestDatadogProvider_RecordCircuitBreakerSuccess(t *testing.T) {
	provider, err := createTestDatadogProvider(t)
	if err != nil {
		t.Skip("Datadog StatsD not available")
	}
	defer provider.Shutdown(context.Background())

	provider.RecordCircuitBreakerSuccess("payment-service")

	assert.Contains(t, provider.circuitBreakerSuccess, "payment-service")
	assert.Equal(t, int64(1), provider.circuitBreakerSuccess["payment-service"])
}

func TestDatadogProvider_RecordServiceRequestCount(t *testing.T) {
	provider, err := createTestDatadogProvider(t)
	if err != nil {
		t.Skip("Datadog StatsD not available")
	}
	defer provider.Shutdown(context.Background())

	provider.RecordServiceRequestCount("user-service")
	provider.RecordServiceRequestCount("user-service")

	assert.Contains(t, provider.serviceRequestCount, "user-service")
	assert.Equal(t, int64(2), provider.serviceRequestCount["user-service"])
}

func TestDatadogProvider_Shutdown(t *testing.T) {
	provider, err := createTestDatadogProvider(t)
	if err != nil {
		t.Skip("Datadog StatsD not available")
	}

	err = provider.Shutdown(context.Background())

	assert.NoError(t, err)
}

// =============================================================================
// noopMetricsProvider Tests
// =============================================================================

func TestNoopProvider_AllMethods(t *testing.T) {
	provider := &noopMetricsProvider{}

	// All methods should execute without panics
	provider.RecordDBOperation("SELECT", 100*time.Millisecond, true)
	provider.RecordDBRetry("UPDATE")
	provider.SetDBConnections(10)
	provider.RecordHTTPRequest("GetUser", "GET", 200, 50*time.Millisecond)
	provider.RecordCircuitBreakerState("auth", "open")
	provider.RecordCircuitBreakerFailure("auth")
	provider.RecordCircuitBreakerSuccess("auth")
	provider.RecordServiceRequestCount("user-service")

	err := provider.Shutdown(context.Background())

	assert.NoError(t, err)
}

// =============================================================================
// Utility Functions Tests
// =============================================================================

func TestBoolToString(t *testing.T) {
	tests := []struct {
		name     string
		input    bool
		expected string
	}{
		{"True value", true, "true"},
		{"False value", false, "false"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := boolToString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Concurrent Access Tests
// =============================================================================

func TestDatadogProvider_ConcurrentAccess(t *testing.T) {
	provider, err := createTestDatadogProvider(t)
	if err != nil {
		t.Skip("Datadog StatsD not available")
	}
	defer provider.Shutdown(context.Background())

	// Simulate concurrent metric recording
	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func(id int) {
			provider.RecordDBOperation("SELECT", time.Duration(id)*time.Millisecond, true)
			provider.RecordServiceRequestCount("user-service")
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify concurrent updates worked
	assert.GreaterOrEqual(t, provider.dbOperationCount["SELECT"], int64(10))
}

// =============================================================================
// Test Helper Functions
// =============================================================================

func createTestPrometheusProvider(t *testing.T) *PrometheusProvider {
	t.Helper()

	cfg := &config.Observability{
		ServiceName: &config.ServiceName{
			ServiceName: "test-service",
		},
		Metrics: &config.Observability_Metrics{
			Enabled:  true,
			Provider: config.Observability_Metrics_PROMETHEUS,
			Port:     19090, // Non-conflicting port
		},
	}

	provider, err := newPrometheusProvider("test-service", cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)

	return provider
}

func createTestOpenTelemetryProvider(t *testing.T) *OpenTelemetryProvider {
	t.Helper()

	cfg := &config.Observability{
		ServiceName: &config.ServiceName{
			ServiceName: "test-otel-service",
		},
		Metrics: &config.Observability_Metrics{
			Enabled:  true,
			Provider: config.Observability_Metrics_OPENTELEMETRY,
		},
	}

	provider, err := newOpenTelemetryProvider("test-otel-service", cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)

	return provider
}

func createTestDatadogProvider(t *testing.T) (*DatadogProvider, error) {
	t.Helper()

	cfg := &config.Observability{
		ServiceName: &config.ServiceName{
			ServiceName: "test-datadog-service",
		},
		Metrics: &config.Observability_Metrics{
			Enabled:  true,
			Provider: config.Observability_Metrics_DATADOG,
			Endpoint: "localhost",
			Port:     8125,
		},
	}

	return newDatadogProvider("test-datadog-service", cfg)
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkOpenTelemetryProvider_RecordDBOperation(b *testing.B) {
	provider, _ := newOpenTelemetryProvider("bench", &config.Observability{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		provider.RecordDBOperation("SELECT", 100*time.Millisecond, true)
	}
}

func BenchmarkNoopProvider_RecordDBOperation(b *testing.B) {
	provider := &noopMetricsProvider{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		provider.RecordDBOperation("SELECT", 100*time.Millisecond, true)
	}
}
