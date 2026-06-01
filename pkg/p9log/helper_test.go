package p9log

import (
	"context"
	"sync"
	"testing"
)

// Mock logger for testing
type mockLogger struct {
	mu      sync.Mutex
	logs    []logEntry
	lastErr error
}

type logEntry struct {
	level   Level
	keyvals []interface{}
}

func (m *mockLogger) Log(level Level, keyvals ...interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logs = append(m.logs, logEntry{
		level:   level,
		keyvals: keyvals,
	})
	return m.lastErr
}

func (m *mockLogger) getLogs() []logEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]logEntry{}, m.logs...)
}

func (m *mockLogger) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = nil
}

func TestNewHelper(t *testing.T) {
	ml := &mockLogger{}
	h := NewHelper(ml)

	if h == nil {
		t.Fatal("expected non-nil Helper")
	}

	if h.msgKey != DefaultMessageKey {
		t.Errorf("expected msgKey=%q, got %q", DefaultMessageKey, h.msgKey)
	}

	if h.logger != ml {
		t.Error("expected logger to match mockLogger")
	}
}

func TestNewHelper_WithMessageKey(t *testing.T) {
	ml := &mockLogger{}
	customKey := "message"
	h := NewHelper(ml, WithMessageKey(customKey))

	if h.msgKey != customKey {
		t.Errorf("expected msgKey=%q, got %q", customKey, h.msgKey)
	}
}

func TestHelper_Log(t *testing.T) {
	ml := &mockLogger{}
	h := NewHelper(ml)

	h.Log(LevelInfo, "key1", "value1", "key2", "value2")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelInfo {
		t.Errorf("expected level INFO, got %v", logs[0].level)
	}

	if len(logs[0].keyvals) != 4 {
		t.Errorf("expected 4 keyvals, got %d", len(logs[0].keyvals))
	}
}

func TestHelper_Debug(t *testing.T) {
	ml := &mockLogger{}
	h := NewHelper(ml)

	h.Debug("test message")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelDebug {
		t.Errorf("expected level DEBUG, got %v", logs[0].level)
	}

	// Check message key and value
	if len(logs[0].keyvals) != 2 {
		t.Errorf("expected 2 keyvals, got %d", len(logs[0].keyvals))
	}

	if logs[0].keyvals[0] != DefaultMessageKey {
		t.Errorf("expected first keyval to be %q, got %v", DefaultMessageKey, logs[0].keyvals[0])
	}

	if logs[0].keyvals[1] != "test message" {
		t.Errorf("expected message %q, got %v", "test message", logs[0].keyvals[1])
	}
}

func TestHelper_Debugf(t *testing.T) {
	ml := &mockLogger{}
	h := NewHelper(ml)

	h.Debugf("test %s %d", "message", 42)

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelDebug {
		t.Errorf("expected level DEBUG, got %v", logs[0].level)
	}

	expectedMsg := "test message 42"
	if logs[0].keyvals[1] != expectedMsg {
		t.Errorf("expected message %q, got %v", expectedMsg, logs[0].keyvals[1])
	}
}

func TestHelper_Debugw(t *testing.T) {
	ml := &mockLogger{}
	h := NewHelper(ml)

	h.Debugw("key1", "value1", "key2", 123)

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelDebug {
		t.Errorf("expected level DEBUG, got %v", logs[0].level)
	}

	if len(logs[0].keyvals) != 4 {
		t.Errorf("expected 4 keyvals, got %d", len(logs[0].keyvals))
	}
}

func TestHelper_Info(t *testing.T) {
	ml := &mockLogger{}
	h := NewHelper(ml)

	h.Info("info message")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelInfo {
		t.Errorf("expected level INFO, got %v", logs[0].level)
	}
}

func TestHelper_Infof(t *testing.T) {
	ml := &mockLogger{}
	h := NewHelper(ml)

	h.Infof("formatted %s", "message")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelInfo {
		t.Errorf("expected level INFO, got %v", logs[0].level)
	}

	expectedMsg := "formatted message"
	if logs[0].keyvals[1] != expectedMsg {
		t.Errorf("expected message %q, got %v", expectedMsg, logs[0].keyvals[1])
	}
}

func TestHelper_Infow(t *testing.T) {
	ml := &mockLogger{}
	h := NewHelper(ml)

	h.Infow("operation", "create", "user_id", 123)

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelInfo {
		t.Errorf("expected level INFO, got %v", logs[0].level)
	}

	if len(logs[0].keyvals) != 4 {
		t.Errorf("expected 4 keyvals, got %d", len(logs[0].keyvals))
	}
}

func TestHelper_Warn(t *testing.T) {
	ml := &mockLogger{}
	h := NewHelper(ml)

	h.Warn("warning message")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelWarn {
		t.Errorf("expected level WARN, got %v", logs[0].level)
	}
}

func TestHelper_Warnf(t *testing.T) {
	ml := &mockLogger{}
	h := NewHelper(ml)

	h.Warnf("warning: %s", "test")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelWarn {
		t.Errorf("expected level WARN, got %v", logs[0].level)
	}
}

func TestHelper_Warnw(t *testing.T) {
	ml := &mockLogger{}
	h := NewHelper(ml)

	h.Warnw("warning", "deprecated_api")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelWarn {
		t.Errorf("expected level WARN, got %v", logs[0].level)
	}
}

func TestHelper_Error(t *testing.T) {
	ml := &mockLogger{}
	h := NewHelper(ml)

	h.Error("error message")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelError {
		t.Errorf("expected level ERROR, got %v", logs[0].level)
	}
}

func TestHelper_Errorf(t *testing.T) {
	ml := &mockLogger{}
	h := NewHelper(ml)

	h.Errorf("error: %s", "test")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelError {
		t.Errorf("expected level ERROR, got %v", logs[0].level)
	}
}

func TestHelper_Errorw(t *testing.T) {
	ml := &mockLogger{}
	h := NewHelper(ml)

	h.Errorw("error", "connection failed", "host", "localhost")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelError {
		t.Errorf("expected level ERROR, got %v", logs[0].level)
	}
}

func TestHelper_WithContext(t *testing.T) {
	ml := &mockLogger{}
	h := NewHelper(ml)

	ctx := context.WithValue(context.Background(), "test_key", "test_value")
	h2 := h.WithContext(ctx)

	if h2 == nil {
		t.Fatal("expected non-nil Helper")
	}

	if h2.msgKey != h.msgKey {
		t.Error("expected msgKey to be preserved")
	}

	// Original helper should be unchanged
	if h == h2 {
		t.Error("expected different Helper instance")
	}
}

func TestHelper_MultipleMessages(t *testing.T) {
	ml := &mockLogger{}
	h := NewHelper(ml)

	h.Info("message 1")
	h.Warn("message 2")
	h.Error("message 3")

	logs := ml.getLogs()
	if len(logs) != 3 {
		t.Fatalf("expected 3 log entries, got %d", len(logs))
	}

	if logs[0].level != LevelInfo {
		t.Error("expected first log to be INFO")
	}
	if logs[1].level != LevelWarn {
		t.Error("expected second log to be WARN")
	}
	if logs[2].level != LevelError {
		t.Error("expected third log to be ERROR")
	}
}

func TestDefaultMessageKey(t *testing.T) {
	expected := "msg"
	if DefaultMessageKey != expected {
		t.Errorf("DefaultMessageKey = %q, want %q", DefaultMessageKey, expected)
	}
}

// Benchmark tests
func BenchmarkHelper_Info(b *testing.B) {
	ml := &mockLogger{}
	h := NewHelper(ml)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Info("test message")
	}
}

func BenchmarkHelper_Infof(b *testing.B) {
	ml := &mockLogger{}
	h := NewHelper(ml)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Infof("test %s %d", "message", 42)
	}
}

func BenchmarkHelper_Infow(b *testing.B) {
	ml := &mockLogger{}
	h := NewHelper(ml)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Infow("key1", "value1", "key2", "value2")
	}
}

func BenchmarkHelper_WithContext(b *testing.B) {
	ml := &mockLogger{}
	h := NewHelper(ml)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = h.WithContext(ctx)
	}
}
