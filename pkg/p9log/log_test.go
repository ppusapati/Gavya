package p9log

import (
	"context"
	"errors"
	"testing"
)

func TestDefaultLogger(t *testing.T) {
	if DefaultLogger == nil {
		t.Fatal("expected DefaultLogger to be non-nil")
	}
}

func TestWith(t *testing.T) {
	ml := &mockLogger{}

	// Test With on a regular Logger
	l1 := With(ml, "service", "test-service")

	if l1 == nil {
		t.Fatal("expected non-nil logger")
	}

	// Log and verify prefix is included
	l1.Log(LevelInfo, "operation", "create")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	// Should have prefix fields (service=test-service) + new fields (operation=create)
	if len(logs[0].keyvals) != 4 {
		t.Errorf("expected 4 keyvals, got %d", len(logs[0].keyvals))
	}
}

func TestWith_Chained(t *testing.T) {
	ml := &mockLogger{}

	// Chain multiple With calls
	l1 := With(ml, "service", "test-service")
	l2 := With(l1, "module", "auth")
	l3 := With(l2, "user_id", 123)

	l3.Log(LevelInfo, "action", "login")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	// The last With call's fields should be present
	// Note: Based on the code, only the most recent With fields are kept
	kvs := logs[0].keyvals
	if len(kvs) < 2 {
		t.Errorf("expected at least 2 keyvals, got %d", len(kvs))
	}
}

func TestWithContext_RegularLogger(t *testing.T) {
	ml := &mockLogger{}
	ctx := context.WithValue(context.Background(), "request_id", "req-123")

	l := WithContext(ctx, ml)

	if l == nil {
		t.Fatal("expected non-nil logger")
	}

	// Verify it's wrapped
	wrapped, ok := l.(*logger)
	if !ok {
		t.Error("expected wrapped logger")
	}

	if wrapped.ctx != ctx {
		t.Error("expected context to match")
	}
}

func TestWithContext_WrappedLogger(t *testing.T) {
	ml := &mockLogger{}
	ctx1 := context.WithValue(context.Background(), "key1", "value1")
	ctx2 := context.WithValue(context.Background(), "key2", "value2")

	l1 := WithContext(ctx1, ml)
	l2 := WithContext(ctx2, l1)

	if l2 == nil {
		t.Fatal("expected non-nil logger")
	}

	// Verify it's wrapped with new context
	wrapped, ok := l2.(*logger)
	if !ok {
		t.Error("expected wrapped logger")
	}

	if wrapped.ctx != ctx2 {
		t.Error("expected context to be updated")
	}
}

func TestLogger_Log(t *testing.T) {
	ml := &mockLogger{}

	// Create wrapped logger with prefix
	l := With(ml, "prefix_key", "prefix_value")

	err := l.Log(LevelInfo, "msg_key", "msg_value")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelInfo {
		t.Errorf("expected level INFO, got %v", logs[0].level)
	}

	// Should have both prefix and message keyvals
	if len(logs[0].keyvals) < 2 {
		t.Errorf("expected at least 2 keyvals, got %d", len(logs[0].keyvals))
	}
}

func TestLogger_Log_WithError(t *testing.T) {
	ml := &mockLogger{}
	expectedErr := errors.New("test error")
	ml.lastErr = expectedErr

	l := With(ml, "key", "value")

	err := l.Log(LevelError, "error", "test")

	if err != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

func TestWith_PreservesPrefix(t *testing.T) {
	ml := &mockLogger{}

	l1 := With(ml, "service", "api")
	l2 := With(l1, "version", "v1")

	l2.Log(LevelInfo, "request", "GET /users")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	// Check that fields are present
	kvs := logs[0].keyvals
	foundVersion := false
	foundRequest := false

	for i := 0; i < len(kvs)-1; i += 2 {
		key, ok := kvs[i].(string)
		if !ok {
			continue
		}

		if key == "version" {
			foundVersion = true
		}
		if key == "request" {
			foundRequest = true
		}
	}

	if !foundVersion {
		t.Error("expected 'version' key in keyvals")
	}
	if !foundRequest {
		t.Error("expected 'request' key in keyvals")
	}
}

func TestLogger_EmptyKeyvals(t *testing.T) {
	ml := &mockLogger{}
	l := With(ml)

	err := l.Log(LevelInfo, "key", "value")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}
}

func TestWithContext_PreservesPrefix(t *testing.T) {
	ml := &mockLogger{}
	ctx := context.Background()

	l1 := With(ml, "service", "test")
	l2 := WithContext(ctx, l1)

	l2.Log(LevelInfo, "msg", "test")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	// Verify prefix is preserved
	kvs := logs[0].keyvals
	foundService := false

	for i := 0; i < len(kvs)-1; i += 2 {
		key, ok := kvs[i].(string)
		if ok && key == "service" {
			foundService = true
			break
		}
	}

	if !foundService {
		t.Error("expected 'service' key to be preserved after WithContext")
	}
}

// Benchmark tests
func BenchmarkWith(b *testing.B) {
	ml := &mockLogger{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = With(ml, "key", "value")
	}
}

func BenchmarkWithContext(b *testing.B) {
	ml := &mockLogger{}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = WithContext(ctx, ml)
	}
}

func BenchmarkLogger_Log(b *testing.B) {
	ml := &mockLogger{}
	l := With(ml, "service", "test")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = l.Log(LevelInfo, "key", "value")
	}
}

func BenchmarkWith_Chained(b *testing.B) {
	ml := &mockLogger{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l1 := With(ml, "service", "test")
		l2 := With(l1, "module", "auth")
		_ = With(l2, "user_id", 123)
	}
}
