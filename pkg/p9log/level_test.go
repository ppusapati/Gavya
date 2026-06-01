package p9log

import (
	"testing"
)

func TestLevel_String(t *testing.T) {
	tests := []struct {
		name     string
		level    Level
		expected string
	}{
		{"debug", LevelDebug, "DEBUG"},
		{"info", LevelInfo, "INFO"},
		{"warn", LevelWarn, "WARN"},
		{"error", LevelError, "ERROR"},
		{"fatal", LevelFatal, "FATAL"},
		{"unknown", Level(99), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.level.String()
			if got != tt.expected {
				t.Errorf("Level.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Level
	}{
		{"debug lowercase", "debug", LevelDebug},
		{"debug uppercase", "DEBUG", LevelDebug},
		{"debug mixed", "DeBuG", LevelDebug},
		{"info lowercase", "info", LevelInfo},
		{"info uppercase", "INFO", LevelInfo},
		{"warn lowercase", "warn", LevelWarn},
		{"warn uppercase", "WARN", LevelWarn},
		{"error lowercase", "error", LevelError},
		{"error uppercase", "ERROR", LevelError},
		{"fatal lowercase", "fatal", LevelFatal},
		{"fatal uppercase", "FATAL", LevelFatal},
		{"unknown", "unknown", LevelInfo}, // defaults to Info
		{"empty", "", LevelInfo},           // defaults to Info
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseLevel(tt.input)
			if got != tt.expected {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestLevelValues(t *testing.T) {
	tests := []struct {
		name  string
		level Level
		value int8
	}{
		{"debug", LevelDebug, -1},
		{"info", LevelInfo, 0},
		{"warn", LevelWarn, 1},
		{"error", LevelError, 2},
		{"fatal", LevelFatal, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if int8(tt.level) != tt.value {
				t.Errorf("%s level value = %d, want %d", tt.name, int8(tt.level), tt.value)
			}
		})
	}
}

func TestLevelKey(t *testing.T) {
	expected := "level"
	if LevelKey != expected {
		t.Errorf("LevelKey = %q, want %q", LevelKey, expected)
	}
}

func TestLevelOrdering(t *testing.T) {
	// Verify level ordering
	if LevelDebug >= LevelInfo {
		t.Error("expected LevelDebug < LevelInfo")
	}
	if LevelInfo >= LevelWarn {
		t.Error("expected LevelInfo < LevelWarn")
	}
	if LevelWarn >= LevelError {
		t.Error("expected LevelWarn < LevelError")
	}
	if LevelError >= LevelFatal {
		t.Error("expected LevelError < LevelFatal")
	}
}

func TestParseLevel_RoundTrip(t *testing.T) {
	levels := []Level{LevelDebug, LevelInfo, LevelWarn, LevelError, LevelFatal}

	for _, level := range levels {
		t.Run(level.String(), func(t *testing.T) {
			str := level.String()
			parsed := ParseLevel(str)
			if parsed != level {
				t.Errorf("ParseLevel(%q) = %v, want %v", str, parsed, level)
			}
		})
	}
}

// Benchmark tests
func BenchmarkLevel_String(b *testing.B) {
	level := LevelInfo

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = level.String()
	}
}

func BenchmarkParseLevel(b *testing.B) {
	input := "INFO"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ParseLevel(input)
	}
}
