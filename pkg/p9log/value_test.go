package p9log

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestValue_Valuer(t *testing.T) {
	ctx := context.Background()

	valuer := func(ctx context.Context) interface{} {
		return "dynamic value"
	}

	result := Value(ctx, Valuer(valuer))
	if result != "dynamic value" {
		t.Errorf("expected 'dynamic value', got %v", result)
	}
}

func TestValue_NonValuer(t *testing.T) {
	ctx := context.Background()

	result := Value(ctx, "static value")
	if result != "static value" {
		t.Errorf("expected 'static value', got %v", result)
	}
}

func TestCaller(t *testing.T) {
	ctx := context.Background()
	caller := Caller(0)

	result := caller(ctx)
	resultStr, ok := result.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", result)
	}

	// Result should be in format "file.go:line"
	if !strings.Contains(resultStr, ":") {
		t.Errorf("expected result to contain ':', got %s", resultStr)
	}

	if !strings.HasSuffix(resultStr[:strings.Index(resultStr, ":")], ".go") {
		t.Errorf("expected filename to end with .go, got %s", resultStr)
	}
}

func TestCaller_WithDepth(t *testing.T) {
	ctx := context.Background()

	depths := []int{0, 1, 2, 3, 4}
	for _, depth := range depths {
		caller := Caller(depth)
		result := caller(ctx)

		resultStr, ok := result.(string)
		if !ok {
			t.Errorf("expected string result for depth %d, got %T", depth, result)
			continue
		}

		// Result should be in format "file.go:line"
		if !strings.Contains(resultStr, ":") {
			t.Errorf("depth %d: expected result to contain ':', got %s", depth, resultStr)
		}
	}
}

func TestTimestamp(t *testing.T) {
	ctx := context.Background()
	layout := time.DateTime

	timestamp := Timestamp(layout)
	result := timestamp(ctx)

	resultStr, ok := result.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", result)
	}

	// Parse to verify format
	_, err := time.Parse(layout, resultStr)
	if err != nil {
		t.Errorf("failed to parse timestamp: %v", err)
	}
}

func TestTimestamp_CustomFormat(t *testing.T) {
	ctx := context.Background()
	layout := "2006-01-02"

	timestamp := Timestamp(layout)
	result := timestamp(ctx)

	resultStr, ok := result.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", result)
	}

	// Parse to verify format
	_, err := time.Parse(layout, resultStr)
	if err != nil {
		t.Errorf("failed to parse timestamp with custom format: %v", err)
	}

	// Check format matches (should be YYYY-MM-DD)
	if len(resultStr) != 10 {
		t.Errorf("expected length 10 for YYYY-MM-DD format, got %d", len(resultStr))
	}
}

func TestDefaultCaller(t *testing.T) {
	if DefaultCaller == nil {
		t.Fatal("expected DefaultCaller to be non-nil")
	}

	ctx := context.Background()
	result := DefaultCaller(ctx)

	resultStr, ok := result.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", result)
	}

	if !strings.Contains(resultStr, ":") {
		t.Errorf("expected result to contain ':', got %s", resultStr)
	}
}

func TestDefaultTimestamp(t *testing.T) {
	if DefaultTimestamp == nil {
		t.Fatal("expected DefaultTimestamp to be non-nil")
	}

	ctx := context.Background()
	result := DefaultTimestamp(ctx)

	resultStr, ok := result.(string)
	if !ok {
		t.Fatalf("expected string result, got %T", result)
	}

	// Verify it's a valid timestamp
	_, err := time.Parse(time.DateTime, resultStr)
	if err != nil {
		t.Errorf("failed to parse default timestamp: %v", err)
	}
}

func TestContainsValuer(t *testing.T) {
	tests := []struct {
		name     string
		keyvals  []interface{}
		expected bool
	}{
		{
			name:     "contains valuer",
			keyvals:  []interface{}{"key", DefaultTimestamp},
			expected: true,
		},
		{
			name:     "no valuer",
			keyvals:  []interface{}{"key1", "value1", "key2", "value2"},
			expected: false,
		},
		{
			name:     "empty",
			keyvals:  []interface{}{},
			expected: false,
		},
		{
			name:     "multiple valuers",
			keyvals:  []interface{}{"key1", DefaultTimestamp, "key2", DefaultCaller},
			expected: true,
		},
		{
			name:     "valuer in wrong position (key position)",
			keyvals:  []interface{}{DefaultTimestamp, "value"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsValuer(tt.keyvals)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestBindValues(t *testing.T) {
	ctx := context.Background()

	staticValue := "static"
	dynamicValuer := func(ctx context.Context) interface{} {
		return "dynamic"
	}

	keyvals := []interface{}{
		"key1", staticValue,
		"key2", Valuer(dynamicValuer),
		"key3", "another static",
	}

	bindValues(ctx, keyvals)

	// Check that static values remain unchanged
	if keyvals[1] != staticValue {
		t.Errorf("expected static value to remain, got %v", keyvals[1])
	}

	// Check that valuer was resolved
	if keyvals[3] != "dynamic" {
		t.Errorf("expected 'dynamic', got %v", keyvals[3])
	}

	// Check that other static value remains unchanged
	if keyvals[5] != "another static" {
		t.Errorf("expected 'another static', got %v", keyvals[5])
	}
}

func TestBindValues_EmptySlice(t *testing.T) {
	ctx := context.Background()
	keyvals := []interface{}{}

	// Should not panic
	bindValues(ctx, keyvals)

	if len(keyvals) != 0 {
		t.Error("expected keyvals to remain empty")
	}
}

func TestBindValues_OnlyKeys(t *testing.T) {
	ctx := context.Background()
	keyvals := []interface{}{"key1"}

	// Should not panic with odd number of elements
	bindValues(ctx, keyvals)

	if len(keyvals) != 1 {
		t.Error("expected keyvals length to remain 1")
	}
}

// Benchmark tests
func BenchmarkCaller(b *testing.B) {
	ctx := context.Background()
	caller := Caller(3)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = caller(ctx)
	}
}

func BenchmarkTimestamp(b *testing.B) {
	ctx := context.Background()
	timestamp := Timestamp(time.DateTime)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = timestamp(ctx)
	}
}

func BenchmarkValue_Valuer(b *testing.B) {
	ctx := context.Background()
	valuer := func(ctx context.Context) interface{} {
		return "value"
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Value(ctx, Valuer(valuer))
	}
}

func BenchmarkValue_NonValuer(b *testing.B) {
	ctx := context.Background()
	value := "static value"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Value(ctx, value)
	}
}

func BenchmarkContainsValuer(b *testing.B) {
	keyvals := []interface{}{"key1", "value1", "key2", DefaultTimestamp, "key3", "value3"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = containsValuer(keyvals)
	}
}

func BenchmarkBindValues(b *testing.B) {
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		keyvals := []interface{}{"key1", "value1", "key2", DefaultTimestamp}
		bindValues(ctx, keyvals)
	}
}
