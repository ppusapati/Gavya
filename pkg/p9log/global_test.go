package p9log

import (
	"context"
	"testing"
)

func TestSetLogger(t *testing.T) {
	// Save original logger
	originalLogger := GetLogger()
	defer func() {
		// Restore original logger
		SetLogger(originalLogger)
	}()

	ml := &mockLogger{}
	SetLogger(ml)

	retrievedLogger := GetLogger()
	if retrievedLogger != ml {
		t.Error("expected SetLogger to update global logger")
	}
}

func TestGetLogger(t *testing.T) {
	logger := GetLogger()
	if logger == nil {
		t.Fatal("expected non-nil logger from GetLogger")
	}
}

func TestContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), "key", "value")
	helper := Context(ctx)

	if helper == nil {
		t.Fatal("expected non-nil Helper from Context")
	}
}

func TestGlobalLog(t *testing.T) {
	// Save and restore
	originalLogger := GetLogger()
	defer SetLogger(originalLogger)

	ml := &mockLogger{}
	SetLogger(ml)

	Log(LevelInfo, "key", "value")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelInfo {
		t.Errorf("expected level INFO, got %v", logs[0].level)
	}
}

func TestGlobalDebug(t *testing.T) {
	originalLogger := GetLogger()
	defer SetLogger(originalLogger)

	ml := &mockLogger{}
	SetLogger(ml)

	Debug("debug message")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelDebug {
		t.Errorf("expected level DEBUG, got %v", logs[0].level)
	}
}

func TestGlobalDebugf(t *testing.T) {
	originalLogger := GetLogger()
	defer SetLogger(originalLogger)

	ml := &mockLogger{}
	SetLogger(ml)

	Debugf("debug %s", "message")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelDebug {
		t.Errorf("expected level DEBUG, got %v", logs[0].level)
	}
}

func TestGlobalDebugw(t *testing.T) {
	originalLogger := GetLogger()
	defer SetLogger(originalLogger)

	ml := &mockLogger{}
	SetLogger(ml)

	Debugw("key", "value")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelDebug {
		t.Errorf("expected level DEBUG, got %v", logs[0].level)
	}
}

func TestGlobalInfo(t *testing.T) {
	originalLogger := GetLogger()
	defer SetLogger(originalLogger)

	ml := &mockLogger{}
	SetLogger(ml)

	Info("info message")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelInfo {
		t.Errorf("expected level INFO, got %v", logs[0].level)
	}
}

func TestGlobalInfof(t *testing.T) {
	originalLogger := GetLogger()
	defer SetLogger(originalLogger)

	ml := &mockLogger{}
	SetLogger(ml)

	Infof("info %s", "message")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelInfo {
		t.Errorf("expected level INFO, got %v", logs[0].level)
	}
}

func TestGlobalInfow(t *testing.T) {
	originalLogger := GetLogger()
	defer SetLogger(originalLogger)

	ml := &mockLogger{}
	SetLogger(ml)

	Infow("operation", "create")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelInfo {
		t.Errorf("expected level INFO, got %v", logs[0].level)
	}
}

func TestGlobalWarn(t *testing.T) {
	originalLogger := GetLogger()
	defer SetLogger(originalLogger)

	ml := &mockLogger{}
	SetLogger(ml)

	Warn("warning message")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelWarn {
		t.Errorf("expected level WARN, got %v", logs[0].level)
	}
}

func TestGlobalWarnf(t *testing.T) {
	originalLogger := GetLogger()
	defer SetLogger(originalLogger)

	ml := &mockLogger{}
	SetLogger(ml)

	Warnf("warning %s", "message")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelWarn {
		t.Errorf("expected level WARN, got %v", logs[0].level)
	}
}

func TestGlobalWarnw(t *testing.T) {
	originalLogger := GetLogger()
	defer SetLogger(originalLogger)

	ml := &mockLogger{}
	SetLogger(ml)

	Warnw("warning", "deprecated")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelWarn {
		t.Errorf("expected level WARN, got %v", logs[0].level)
	}
}

func TestGlobalError(t *testing.T) {
	originalLogger := GetLogger()
	defer SetLogger(originalLogger)

	ml := &mockLogger{}
	SetLogger(ml)

	Error("error message")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelError {
		t.Errorf("expected level ERROR, got %v", logs[0].level)
	}
}

func TestGlobalErrorf(t *testing.T) {
	originalLogger := GetLogger()
	defer SetLogger(originalLogger)

	ml := &mockLogger{}
	SetLogger(ml)

	Errorf("error %s", "message")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelError {
		t.Errorf("expected level ERROR, got %v", logs[0].level)
	}
}

func TestGlobalErrorw(t *testing.T) {
	originalLogger := GetLogger()
	defer SetLogger(originalLogger)

	ml := &mockLogger{}
	SetLogger(ml)

	Errorw("error", "failed")

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(logs))
	}

	if logs[0].level != LevelError {
		t.Errorf("expected level ERROR, got %v", logs[0].level)
	}
}

func TestLoggerAppliance_SetLogger(t *testing.T) {
	appliance := &loggerAppliance{}
	ml := &mockLogger{}

	appliance.SetLogger(ml)

	if appliance.Logger != ml {
		t.Error("expected Logger to be set")
	}

	if appliance.helper == nil {
		t.Error("expected helper to be initialized")
	}
}

func TestLoggerAppliance_GetLogger(t *testing.T) {
	appliance := &loggerAppliance{}
	ml := &mockLogger{}
	appliance.Logger = ml

	retrieved := appliance.GetLogger()
	if retrieved != ml {
		t.Error("expected GetLogger to return the set logger")
	}
}

// Benchmark tests
func BenchmarkGlobalInfo(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Info("test message")
	}
}

func BenchmarkGlobalInfow(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Infow("key", "value")
	}
}

func BenchmarkSetLogger(b *testing.B) {
	ml := &mockLogger{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SetLogger(ml)
	}
}
