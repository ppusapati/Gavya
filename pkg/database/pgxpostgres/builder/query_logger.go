package builder

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"p9e.in/samavaya/packages/p9log"
)

// QueryLogger provides optional SQL query logging for debugging.
// It logs queries with sanitized parameters to prevent exposing sensitive data.
type QueryLogger struct {
	helper  *p9log.Helper
	enabled bool
	verbose bool // If true, logs parameter values (sanitized)
}

// QueryLogConfig configures query logging behavior.
type QueryLogConfig struct {
	Enabled bool // Enable query logging
	Verbose bool // Log parameter values (sanitized)
}

var (
	// Global query logger instance
	globalQueryLogger *QueryLogger

	// Patterns to detect sensitive data
	sensitivePatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(password|secret|token|key|credential)`),
		regexp.MustCompile(`(?i)(ssn|social_security)`),
		regexp.MustCompile(`(?i)(credit_card|cvv|card_number)`),
	}
)

// NewQueryLogger creates a new QueryLogger instance.
func NewQueryLogger(logger p9log.Logger, config QueryLogConfig) *QueryLogger {
	return &QueryLogger{
		helper:  p9log.NewHelper(logger),
		enabled: config.Enabled,
		verbose: config.Verbose,
	}
}

// SetGlobalQueryLogger sets the global query logger used by all query functions.
func SetGlobalQueryLogger(logger *QueryLogger) {
	globalQueryLogger = logger
}

// GetGlobalQueryLogger returns the global query logger (may be nil).
func GetGlobalQueryLogger() *QueryLogger {
	return globalQueryLogger
}

// LogQuery logs a SQL query with optional parameters.
// Parameters are sanitized if they appear to contain sensitive data.
func (ql *QueryLogger) LogQuery(ctx context.Context, operation string, query string, args []interface{}, duration time.Duration) {
	if !ql.enabled {
		return
	}

	keyvals := []interface{}{
		"operation", operation,
		"query", query,
		"duration_ms", duration.Milliseconds(),
	}

	if ql.verbose && len(args) > 0 {
		sanitized := sanitizeParams(args)
		keyvals = append(keyvals, "params", sanitized, "param_count", len(args))
	} else if len(args) > 0 {
		keyvals = append(keyvals, "param_count", len(args))
	}

	ql.helper.WithContext(ctx).Infow(keyvals...)
}

// LogQueryError logs a failed SQL query with error details.
func (ql *QueryLogger) LogQueryError(ctx context.Context, operation string, query string, args []interface{}, err error, duration time.Duration) {
	if !ql.enabled {
		return
	}

	keyvals := []interface{}{
		"operation", operation,
		"query", query,
		"error", err.Error(),
		"duration_ms", duration.Milliseconds(),
	}

	if ql.verbose && len(args) > 0 {
		sanitized := sanitizeParams(args)
		keyvals = append(keyvals, "params", sanitized)
	}

	ql.helper.WithContext(ctx).Errorw(keyvals...)
}

// sanitizeParams sanitizes query parameters to prevent logging sensitive data.
// Detects common sensitive field names and redacts their values.
func sanitizeParams(args []interface{}) []interface{} {
	if len(args) == 0 {
		return args
	}

	sanitized := make([]interface{}, len(args))
	for i, arg := range args {
		sanitized[i] = sanitizeValue(arg)
	}
	return sanitized
}

// sanitizeValue sanitizes a single parameter value.
func sanitizeValue(val interface{}) interface{} {
	if val == nil {
		return nil
	}

	// Check if value contains sensitive data based on string representation
	strVal := fmt.Sprintf("%v", val)
	if isSensitive(strVal) {
		return "[REDACTED]"
	}

	// For string values, check content
	if s, ok := val.(string); ok {
		if len(s) > 100 {
			return s[:100] + "... [TRUNCATED]"
		}
		if containsSensitivePattern(s) {
			return "[REDACTED]"
		}
	}

	return val
}

// isSensitive checks if a value should be considered sensitive.
// This is a simple heuristic - customize based on your security requirements.
func isSensitive(val string) bool {
	// Very long strings might be tokens/secrets
	if len(val) > 500 {
		return true
	}

	// Base64-like strings that are very long
	if isBase64Like(val) && len(val) > 64 {
		return true
	}

	return false
}

// containsSensitivePattern checks if a string matches known sensitive patterns.
func containsSensitivePattern(s string) bool {
	for _, pattern := range sensitivePatterns {
		if pattern.MatchString(s) {
			return true
		}
	}
	return false
}

// isBase64Like checks if a string looks like base64 encoding.
func isBase64Like(s string) bool {
	if len(s) < 16 {
		return false
	}

	// Count alphanumeric and base64 chars
	validChars := 0
	for _, ch := range s {
		if (ch >= 'a' && ch <= 'z') ||
			(ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') ||
			ch == '+' || ch == '/' || ch == '=' {
			validChars++
		}
	}

	// If >90% are base64 chars, it's likely base64
	return float64(validChars)/float64(len(s)) > 0.9
}

// formatQueryWithParams creates a readable query string with parameters interpolated.
// This is for debugging only - NEVER use this for actual query execution!
func formatQueryWithParams(query string, args []interface{}) string {
	if len(args) == 0 {
		return query
	}

	result := query
	for i, arg := range args {
		placeholder := fmt.Sprintf("$%d", i+1)
		sanitized := sanitizeValue(arg)

		var replacement string
		switch v := sanitized.(type) {
		case string:
			replacement = fmt.Sprintf("'%s'", strings.ReplaceAll(v, "'", "''"))
		case nil:
			replacement = "NULL"
		default:
			replacement = fmt.Sprintf("%v", v)
		}

		result = strings.Replace(result, placeholder, replacement, 1)
	}

	return result
}

// WithQueryLogging is a helper that wraps query execution with logging.
// Example usage:
//
//	result, err := WithQueryLogging(ctx, "SELECT", query, args, func() (interface{}, error) {
//	    return db.Query(ctx, query, args...)
//	})
func WithQueryLogging(ctx context.Context, operation string, query string, args []interface{}, fn func() (interface{}, error)) (interface{}, error) {
	if globalQueryLogger == nil || !globalQueryLogger.enabled {
		return fn()
	}

	start := time.Now()
	result, err := fn()
	duration := time.Since(start)

	if err != nil {
		globalQueryLogger.LogQueryError(ctx, operation, query, args, err, duration)
	} else {
		globalQueryLogger.LogQuery(ctx, operation, query, args, duration)
	}

	return result, err
}
