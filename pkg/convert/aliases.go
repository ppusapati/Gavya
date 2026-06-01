// Package convert exposes top-level alias helpers used by business modules.
//
// These aliases delegate to the implementations in the sql subpackage so that
// callers can use a single import path without needing to know about the
// internal layout of the convert package.
package convert

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	csql "p9e.in/samavaya/packages/convert/sql"
)

// PgDateToTimePtr converts a pgtype.Date to *time.Time.
func PgDateToTimePtr(d pgtype.Date) *time.Time {
	return csql.TimePtrFromDate(d)
}

// TimePtrToPgDate converts *time.Time to pgtype.Date.
func TimePtrToPgDate(t *time.Time) pgtype.Date {
	return csql.DateFromTimePtr(t)
}

// PgTimestamptzToTimePtr converts pgtype.Timestamptz to *time.Time.
func PgTimestamptzToTimePtr(ts pgtype.Timestamptz) *time.Time {
	return csql.TimePtrFromTimestamptz(ts)
}

// TimePtrToPgTimestamptz converts *time.Time to pgtype.Timestamptz.
func TimePtrToPgTimestamptz(t *time.Time) pgtype.Timestamptz {
	return csql.TimestamptzFromTimePtr(t)
}

// NumericToFloat64 converts pgtype.Numeric to float64.
func NumericToFloat64(n pgtype.Numeric) float64 {
	return csql.Float64FromNumeric(n)
}

// Float64ToNumeric converts float64 to pgtype.Numeric.
func Float64ToNumeric(f float64) pgtype.Numeric {
	return csql.NumericFromFloat64(f)
}

// PgNumericToFloat64 is an alias for NumericToFloat64.
func PgNumericToFloat64(n pgtype.Numeric) float64 {
	return csql.Float64FromNumeric(n)
}

// Float64ToPgNumeric is an alias for Float64ToNumeric.
func Float64ToPgNumeric(f float64) pgtype.Numeric {
	return csql.NumericFromFloat64(f)
}
