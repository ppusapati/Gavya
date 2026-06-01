package p9log

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewStdLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStdLogger(&buf)

	if logger == nil {
		t.Fatal("expected non-nil logger")
	}

	// Verify it implements Logger interface
	var _ Logger = logger
}

func TestStdLogger_Log(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStdLogger(&buf)

	err := logger.Log(LevelInfo, "key", "value")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "INFO") {
		t.Error("expected output to contain INFO")
	}
	if !strings.Contains(output, "key=value") {
		t.Error("expected output to contain key=value")
	}
}

func TestStdLogger_Log_EmptyKeyvals(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStdLogger(&buf)

	err := logger.Log(LevelInfo)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Empty keyvals should not produce output
	if buf.Len() > 0 {
		t.Error("expected no output for empty keyvals")
	}
}

func TestStdLogger_Log_UnpairedKeyvals(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStdLogger(&buf)

	err := logger.Log(LevelWarn, "key1", "value1", "key2")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "KEYVALS UNPAIRED") {
		t.Error("expected output to contain KEYVALS UNPAIRED for odd keyvals")
	}
}

func TestStdLogger_Log_AllLevels(t *testing.T) {
	tests := []struct {
		name  string
		level Level
	}{
		{"debug", LevelDebug},
		{"info", LevelInfo},
		{"warn", LevelWarn},
		{"error", LevelError},
		{"fatal", LevelFatal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := NewStdLogger(&buf)

			err := logger.Log(tt.level, "message", "test")
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			output := buf.String()
			expectedLevel := tt.level.String()
			if !strings.Contains(output, expectedLevel) {
				t.Errorf("expected output to contain %s, got %s", expectedLevel, output)
			}
		})
	}
}

func TestStdLogger_Log_MultipleKeyvals(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStdLogger(&buf)

	err := logger.Log(LevelInfo, "key1", "value1", "key2", "value2", "key3", "value3")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "key1=value1") {
		t.Error("expected output to contain key1=value1")
	}
	if !strings.Contains(output, "key2=value2") {
		t.Error("expected output to contain key2=value2")
	}
	if !strings.Contains(output, "key3=value3") {
		t.Error("expected output to contain key3=value3")
	}
}

func TestStdLogger_InterfaceCompliance(t *testing.T) {
	var buf bytes.Buffer
	logger := NewStdLogger(&buf)

	// Verify it implements Logger interface
	var _ Logger = logger
}

// Benchmark tests
func BenchmarkStdLogger_Log(b *testing.B) {
	var buf bytes.Buffer
	logger := NewStdLogger(&buf)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = logger.Log(LevelInfo, "key", "value")
	}
}

func BenchmarkStdLogger_Log_MultipleKeyvals(b *testing.B) {
	var buf bytes.Buffer
	logger := NewStdLogger(&buf)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = logger.Log(LevelInfo, "key1", "value1", "key2", "value2", "key3", "value3")
	}
}

func BenchmarkNewStdLogger(b *testing.B) {
	var buf bytes.Buffer

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewStdLogger(&buf)
	}
}
