package p9log

import (
	"testing"
)

func TestNewFilter(t *testing.T) {
	ml := &mockLogger{}
	filter := NewFilter(ml)

	if filter == nil {
		t.Fatal("expected non-nil Filter")
	}

	if filter.logger != ml {
		t.Error("expected logger to match mockLogger")
	}

	if filter.key == nil {
		t.Error("expected key map to be initialized")
	}

	if filter.value == nil {
		t.Error("expected value map to be initialized")
	}
}

func TestFilterLevel(t *testing.T) {
	ml := &mockLogger{}
	filter := NewFilter(ml, FilterLevel(LevelWarn))

	if filter.level != LevelWarn {
		t.Errorf("expected level WARN, got %v", filter.level)
	}

	// Log below threshold should be filtered
	err := filter.Log(LevelInfo, "key", "value")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	logs := ml.getLogs()
	if len(logs) != 0 {
		t.Errorf("expected 0 logs (filtered), got %d", len(logs))
	}

	// Log at or above threshold should pass
	ml.reset()
	err = filter.Log(LevelWarn, "key", "value")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	logs = ml.getLogs()
	if len(logs) != 1 {
		t.Errorf("expected 1 log, got %d", len(logs))
	}
}

func TestFilterKey(t *testing.T) {
	ml := &mockLogger{}
	filter := NewFilter(ml, FilterKey("password", "secret"))

	err := filter.Log(LevelInfo, "username", "user123", "password", "mypass", "token", "abc")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}

	// Check that password value is replaced with fuzzyStr
	keyvals := logs[0].keyvals
	foundPassword := false
	for i := 0; i < len(keyvals)-1; i += 2 {
		if keyvals[i] == "password" {
			foundPassword = true
			if keyvals[i+1] != fuzzyStr {
				t.Errorf("expected password value to be %q, got %v", fuzzyStr, keyvals[i+1])
			}
		}
	}

	if !foundPassword {
		t.Error("expected to find password key in keyvals")
	}
}

func TestFilterValue(t *testing.T) {
	ml := &mockLogger{}
	sensitiveValue := "sensitive-data"
	filter := NewFilter(ml, FilterValue(sensitiveValue))

	err := filter.Log(LevelInfo, "key1", "normal", "key2", sensitiveValue, "key3", "test")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}

	// Check that sensitive value is replaced with fuzzyStr
	keyvals := logs[0].keyvals
	foundSensitive := false
	for i := 1; i < len(keyvals); i += 2 {
		if keyvals[i] == fuzzyStr {
			foundSensitive = true
		}
		if keyvals[i] == sensitiveValue {
			t.Error("expected sensitive value to be filtered")
		}
	}

	if !foundSensitive {
		t.Error("expected to find fuzzyStr in keyvals")
	}
}

func TestFilterFunc(t *testing.T) {
	ml := &mockLogger{}

	// Filter out logs containing "skip" key
	filterFn := func(level Level, keyvals ...interface{}) bool {
		for i := 0; i < len(keyvals); i += 2 {
			if keyvals[i] == "skip" {
				return true // Skip this log
			}
		}
		return false
	}

	filter := NewFilter(ml, FilterFunc(filterFn))

	// This should be skipped
	err := filter.Log(LevelInfo, "skip", "true")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	logs := ml.getLogs()
	if len(logs) != 0 {
		t.Errorf("expected 0 logs (filtered by func), got %d", len(logs))
	}

	// This should pass
	ml.reset()
	err = filter.Log(LevelInfo, "keep", "value")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	logs = ml.getLogs()
	if len(logs) != 1 {
		t.Errorf("expected 1 log, got %d", len(logs))
	}
}

func TestFilter_CombinedOptions(t *testing.T) {
	ml := &mockLogger{}
	filter := NewFilter(ml,
		FilterLevel(LevelInfo),
		FilterKey("password"),
		FilterValue("secret123"),
	)

	// Test level filtering
	err := filter.Log(LevelDebug, "key", "value")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	logs := ml.getLogs()
	if len(logs) != 0 {
		t.Error("expected debug log to be filtered by level")
	}

	// Test key and value filtering
	ml.reset()
	err = filter.Log(LevelInfo, "username", "user", "password", "pass123", "token", "secret123")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	logs = ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}

	keyvals := logs[0].keyvals
	fuzzyCount := 0
	for i := 1; i < len(keyvals); i += 2 {
		if keyvals[i] == fuzzyStr {
			fuzzyCount++
		}
	}

	if fuzzyCount != 2 {
		t.Errorf("expected 2 fuzzy values (password and secret123), got %d", fuzzyCount)
	}
}

func TestFilter_WithWrappedLogger(t *testing.T) {
	ml := &mockLogger{}
	wrappedLogger := With(ml, "service", "test-service")
	filter := NewFilter(wrappedLogger, FilterKey("password"))

	err := filter.Log(LevelInfo, "password", "secret")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}

	// Check that password is filtered even with prefix
	keyvals := logs[0].keyvals
	foundFuzzy := false
	for i := 1; i < len(keyvals); i += 2 {
		if keyvals[i] == fuzzyStr {
			foundFuzzy = true
		}
	}

	if !foundFuzzy {
		t.Error("expected password to be filtered with wrapped logger")
	}
}

func TestFilter_EmptyKeyvals(t *testing.T) {
	ml := &mockLogger{}
	filter := NewFilter(ml, FilterKey("key"))

	err := filter.Log(LevelInfo)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Errorf("expected 1 log, got %d", len(logs))
	}
}

func TestFilter_OddKeyvals(t *testing.T) {
	ml := &mockLogger{}
	filter := NewFilter(ml, FilterKey("key1"))

	// Odd number of keyvals
	err := filter.Log(LevelInfo, "key1", "value1", "key2")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Errorf("expected 1 log, got %d", len(logs))
	}

	// Should filter key1's value
	keyvals := logs[0].keyvals
	if keyvals[1] != fuzzyStr {
		t.Errorf("expected key1 value to be filtered, got %v", keyvals[1])
	}
}

func TestFilter_MultipleKeys(t *testing.T) {
	ml := &mockLogger{}
	filter := NewFilter(ml, FilterKey("password", "token", "apikey"))

	err := filter.Log(LevelInfo,
		"username", "user",
		"password", "pass",
		"token", "tok",
		"apikey", "key",
		"data", "normal",
	)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	logs := ml.getLogs()
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}

	keyvals := logs[0].keyvals
	fuzzyCount := 0
	for i := 1; i < len(keyvals); i += 2 {
		if keyvals[i] == fuzzyStr {
			fuzzyCount++
		}
	}

	if fuzzyCount != 3 {
		t.Errorf("expected 3 filtered values, got %d", fuzzyCount)
	}
}

// Benchmark tests
func BenchmarkFilter_Log(b *testing.B) {
	ml := &mockLogger{}
	filter := NewFilter(ml, FilterLevel(LevelInfo))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = filter.Log(LevelInfo, "key", "value")
	}
}

func BenchmarkFilter_Log_WithKeyFiltering(b *testing.B) {
	ml := &mockLogger{}
	filter := NewFilter(ml, FilterKey("password", "token"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = filter.Log(LevelInfo, "username", "user", "password", "pass", "data", "value")
	}
}

func BenchmarkFilter_Log_WithFilterFunc(b *testing.B) {
	ml := &mockLogger{}
	filterFn := func(level Level, keyvals ...interface{}) bool {
		return false
	}
	filter := NewFilter(ml, FilterFunc(filterFn))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = filter.Log(LevelInfo, "key", "value")
	}
}
