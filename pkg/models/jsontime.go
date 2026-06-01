package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// JSONTime wraps time.Time so we can control both
// JSON un/marshaling and SQL driver encoding.
type JSONTime time.Time

// UnmarshalJSON lets us parse either RFC3339 (“2025-05-16T15:32:25Z”)
// or your shorter form (“2025-05-16T15:32:25.000”).
func (jt *JSONTime) UnmarshalJSON(b []byte) error {
	// strip quotes
	s := string(b)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		s = s[1 : len(s)-1]
	}

	// try full RFC3339
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		*jt = JSONTime(t)
		return nil
	}
	// fallback to your millisecond‐precision form
	const layout = "2006-01-02T15:04:05.000"
	t, err := time.Parse(layout, s)
	if err != nil {
		return fmt.Errorf("JSONTime.UnmarshalJSON: cannot parse %q: %w", s, err)
	}
	*jt = JSONTime(t)
	return nil
}

// MarshalJSON always emits full RFC3339 (“…Z”).
func (jt JSONTime) MarshalJSON() ([]byte, error) {
	t := time.Time(jt)
	return json.Marshal(t.Format(time.RFC3339))
}

// Value implements driver.Valuer so GORM/pgx can
// turn JSONTime into a SQL TIMESTAMPTZ parameter.
func (jt JSONTime) Value() (driver.Value, error) {
	t := time.Time(jt)
	return t, nil
}

// Scan implements sql.Scanner so GORM can read
// TIMESTAMPTZ back into JSONTime when querying.
func (jt *JSONTime) Scan(src interface{}) error {
	if src == nil {
		*jt = JSONTime(time.Time{})
		return nil
	}

	switch v := src.(type) {
	case time.Time:
		*jt = JSONTime(v)
		return nil
	case []byte:
		// Postgres driver sometimes gives []byte
		t, err := time.Parse(time.RFC3339Nano, string(v))
		if err != nil {
			return fmt.Errorf("JSONTime.Scan: parse %q: %w", string(v), err)
		}
		*jt = JSONTime(t)
		return nil
	case string:
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			return fmt.Errorf("JSONTime.Scan: parse %q: %w", v, err)
		}
		*jt = JSONTime(t)
		return nil
	default:
		return fmt.Errorf("JSONTime.Scan: unsupported type %T", src)
	}
}
