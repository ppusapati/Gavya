package expression

import (
	"fmt"
	"time"

	"p9e.in/samavaya/packages/errors"
)

// TimeIntelligence generates PostgreSQL SQL expressions for time-based analytics.
// It produces SQL fragments (WHERE clauses, window functions, etc.) that the
// query engine injects into generated SQL for BI dashboards and reports.
type TimeIntelligence struct {
	dateColumn      string // e.g. "order_date"
	fiscalYearStart int    // month (1-12), default 4 for April (Indian fiscal year)
}

// NewTimeIntelligence creates a new TimeIntelligence instance.
// dateColumn is the SQL column name used in generated expressions.
// fiscalYearStart is the month (1-12) when the fiscal year begins.
func NewTimeIntelligence(dateColumn string, fiscalYearStart int) *TimeIntelligence {
	if fiscalYearStart < 1 || fiscalYearStart > 12 {
		fiscalYearStart = 4 // default to Indian fiscal year (April)
	}
	return &TimeIntelligence{
		dateColumn:      dateColumn,
		fiscalYearStart: fiscalYearStart,
	}
}

// formatDate returns a date formatted as a PostgreSQL date literal.
func formatDate(t time.Time) string {
	return t.Format("2006-01-02")
}

// ---------------------------------------------------------------------------
// Period-to-Date Functions
// ---------------------------------------------------------------------------

// YTD returns a SQL WHERE clause fragment for year-to-date filtering.
// It filters rows from January 1 of the reference year through the reference date.
func (ti *TimeIntelligence) YTD(referenceDate time.Time) string {
	yearStart := time.Date(referenceDate.Year(), time.January, 1, 0, 0, 0, 0, referenceDate.Location())
	return fmt.Sprintf(
		"(%s >= '%s' AND %s <= '%s')",
		ti.dateColumn, formatDate(yearStart),
		ti.dateColumn, formatDate(referenceDate),
	)
}

// QTD returns a SQL WHERE clause fragment for quarter-to-date filtering.
// It filters rows from the start of the calendar quarter through the reference date.
func (ti *TimeIntelligence) QTD(referenceDate time.Time) string {
	qMonth := quarterStartMonth(referenceDate.Month())
	qStart := time.Date(referenceDate.Year(), qMonth, 1, 0, 0, 0, 0, referenceDate.Location())
	return fmt.Sprintf(
		"(%s >= '%s' AND %s <= '%s')",
		ti.dateColumn, formatDate(qStart),
		ti.dateColumn, formatDate(referenceDate),
	)
}

// MTD returns a SQL WHERE clause fragment for month-to-date filtering.
func (ti *TimeIntelligence) MTD(referenceDate time.Time) string {
	mStart := time.Date(referenceDate.Year(), referenceDate.Month(), 1, 0, 0, 0, 0, referenceDate.Location())
	return fmt.Sprintf(
		"(%s >= '%s' AND %s <= '%s')",
		ti.dateColumn, formatDate(mStart),
		ti.dateColumn, formatDate(referenceDate),
	)
}

// WTD returns a SQL WHERE clause fragment for week-to-date filtering (ISO week, Monday start).
func (ti *TimeIntelligence) WTD(referenceDate time.Time) string {
	weekday := referenceDate.Weekday()
	if weekday == time.Sunday {
		weekday = 7
	}
	daysSinceMonday := int(weekday) - 1
	wStart := referenceDate.AddDate(0, 0, -daysSinceMonday)
	return fmt.Sprintf(
		"(%s >= '%s' AND %s <= '%s')",
		ti.dateColumn, formatDate(wStart),
		ti.dateColumn, formatDate(referenceDate),
	)
}

// FYTD returns a SQL WHERE clause fragment for fiscal year-to-date filtering.
func (ti *TimeIntelligence) FYTD(referenceDate time.Time) string {
	fyStart := fiscalYearStartDate(referenceDate, ti.fiscalYearStart)
	return fmt.Sprintf(
		"(%s >= '%s' AND %s <= '%s')",
		ti.dateColumn, formatDate(fyStart),
		ti.dateColumn, formatDate(referenceDate),
	)
}

// FQTD returns a SQL WHERE clause fragment for fiscal quarter-to-date filtering.
func (ti *TimeIntelligence) FQTD(referenceDate time.Time) string {
	fqStart := fiscalQuarterStartDate(referenceDate, ti.fiscalYearStart)
	return fmt.Sprintf(
		"(%s >= '%s' AND %s <= '%s')",
		ti.dateColumn, formatDate(fqStart),
		ti.dateColumn, formatDate(referenceDate),
	)
}

// ---------------------------------------------------------------------------
// Same Period Last Year/Quarter/Month
// ---------------------------------------------------------------------------

// SPLY returns a SQL WHERE clause for the same period last year.
// It takes the current YTD range and shifts it back by one year.
func (ti *TimeIntelligence) SPLY(referenceDate time.Time) string {
	yearStart := time.Date(referenceDate.Year(), time.January, 1, 0, 0, 0, 0, referenceDate.Location())
	prevYearStart := yearStart.AddDate(-1, 0, 0)
	prevRef := referenceDate.AddDate(-1, 0, 0)
	return fmt.Sprintf(
		"(%s >= '%s' AND %s <= '%s')",
		ti.dateColumn, formatDate(prevYearStart),
		ti.dateColumn, formatDate(prevRef),
	)
}

// SPLQ returns a SQL WHERE clause for the same period last quarter.
// It takes the current QTD range and shifts it back by one quarter (3 months).
func (ti *TimeIntelligence) SPLQ(referenceDate time.Time) string {
	qMonth := quarterStartMonth(referenceDate.Month())
	qStart := time.Date(referenceDate.Year(), qMonth, 1, 0, 0, 0, 0, referenceDate.Location())
	prevQStart := qStart.AddDate(0, -3, 0)
	prevRef := referenceDate.AddDate(0, -3, 0)
	return fmt.Sprintf(
		"(%s >= '%s' AND %s <= '%s')",
		ti.dateColumn, formatDate(prevQStart),
		ti.dateColumn, formatDate(prevRef),
	)
}

// SPLM returns a SQL WHERE clause for the same period last month.
// It takes the current MTD range and shifts it back by one month.
func (ti *TimeIntelligence) SPLM(referenceDate time.Time) string {
	mStart := time.Date(referenceDate.Year(), referenceDate.Month(), 1, 0, 0, 0, 0, referenceDate.Location())
	prevMStart := mStart.AddDate(0, -1, 0)
	prevRef := referenceDate.AddDate(0, -1, 0)
	return fmt.Sprintf(
		"(%s >= '%s' AND %s <= '%s')",
		ti.dateColumn, formatDate(prevMStart),
		ti.dateColumn, formatDate(prevRef),
	)
}

// ---------------------------------------------------------------------------
// Rolling Windows
// ---------------------------------------------------------------------------

// RollingDays returns a SQL WHERE clause for the last N days ending at referenceDate.
func (ti *TimeIntelligence) RollingDays(n int, referenceDate time.Time) string {
	start := referenceDate.AddDate(0, 0, -n)
	return fmt.Sprintf(
		"(%s > '%s' AND %s <= '%s')",
		ti.dateColumn, formatDate(start),
		ti.dateColumn, formatDate(referenceDate),
	)
}

// RollingWeeks returns a SQL WHERE clause for the last N weeks ending at referenceDate.
func (ti *TimeIntelligence) RollingWeeks(n int, referenceDate time.Time) string {
	start := referenceDate.AddDate(0, 0, -n*7)
	return fmt.Sprintf(
		"(%s > '%s' AND %s <= '%s')",
		ti.dateColumn, formatDate(start),
		ti.dateColumn, formatDate(referenceDate),
	)
}

// RollingMonths returns a SQL WHERE clause for the last N months ending at referenceDate.
func (ti *TimeIntelligence) RollingMonths(n int, referenceDate time.Time) string {
	start := referenceDate.AddDate(0, -n, 0)
	return fmt.Sprintf(
		"(%s > '%s' AND %s <= '%s')",
		ti.dateColumn, formatDate(start),
		ti.dateColumn, formatDate(referenceDate),
	)
}

// RollingQuarters returns a SQL WHERE clause for the last N quarters ending at referenceDate.
func (ti *TimeIntelligence) RollingQuarters(n int, referenceDate time.Time) string {
	start := referenceDate.AddDate(0, -n*3, 0)
	return fmt.Sprintf(
		"(%s > '%s' AND %s <= '%s')",
		ti.dateColumn, formatDate(start),
		ti.dateColumn, formatDate(referenceDate),
	)
}

// ---------------------------------------------------------------------------
// Period Comparisons
// ---------------------------------------------------------------------------

// PeriodOverPeriod returns SQL fragments for comparing two arbitrary periods
// and computing percentage growth between them.
func (ti *TimeIntelligence) PeriodOverPeriod(currentStart, currentEnd, previousStart, previousEnd time.Time) PeriodComparison {
	currentSQL := fmt.Sprintf(
		"(%s >= '%s' AND %s <= '%s')",
		ti.dateColumn, formatDate(currentStart),
		ti.dateColumn, formatDate(currentEnd),
	)
	previousSQL := fmt.Sprintf(
		"(%s >= '%s' AND %s <= '%s')",
		ti.dateColumn, formatDate(previousStart),
		ti.dateColumn, formatDate(previousEnd),
	)
	growthSQL := fmt.Sprintf(
		"(CASE WHEN previous_period_total = 0 THEN NULL "+
			"ELSE ((current_period_total - previous_period_total) / NULLIF(previous_period_total, 0)) * 100.0 END)",
	)
	return PeriodComparison{
		CurrentPeriodSQL:  currentSQL,
		PreviousPeriodSQL: previousSQL,
		GrowthSQL:         growthSQL,
	}
}

// ---------------------------------------------------------------------------
// Growth / Change (window function SQL expressions)
// ---------------------------------------------------------------------------

// YoYGrowthSQL returns a SQL expression for year-over-year growth percentage.
// It uses LAG over 12 monthly periods to compare the current value with the same
// month in the previous year.
func (ti *TimeIntelligence) YoYGrowthSQL(measureColumn string) string {
	return fmt.Sprintf(
		"((%s - LAG(%s, 12) OVER (ORDER BY %s)) / NULLIF(LAG(%s, 12) OVER (ORDER BY %s), 0)) * 100.0",
		measureColumn, measureColumn, ti.dateColumn,
		measureColumn, ti.dateColumn,
	)
}

// QoQGrowthSQL returns a SQL expression for quarter-over-quarter growth percentage.
// It uses LAG over 4 quarterly periods.
func (ti *TimeIntelligence) QoQGrowthSQL(measureColumn string) string {
	return fmt.Sprintf(
		"((%s - LAG(%s, 4) OVER (ORDER BY %s)) / NULLIF(LAG(%s, 4) OVER (ORDER BY %s), 0)) * 100.0",
		measureColumn, measureColumn, ti.dateColumn,
		measureColumn, ti.dateColumn,
	)
}

// MoMGrowthSQL returns a SQL expression for month-over-month growth percentage.
// It uses LAG over 1 period.
func (ti *TimeIntelligence) MoMGrowthSQL(measureColumn string) string {
	return fmt.Sprintf(
		"((%s - LAG(%s, 1) OVER (ORDER BY %s)) / NULLIF(LAG(%s, 1) OVER (ORDER BY %s), 0)) * 100.0",
		measureColumn, measureColumn, ti.dateColumn,
		measureColumn, ti.dateColumn,
	)
}

// ---------------------------------------------------------------------------
// Moving Averages (window function SQL expressions)
// ---------------------------------------------------------------------------

// MovingAverageSQL returns a SQL expression for a moving average over the last N periods.
func (ti *TimeIntelligence) MovingAverageSQL(measureColumn string, periods int) string {
	if periods < 1 {
		periods = 1
	}
	return fmt.Sprintf(
		"AVG(%s) OVER (ORDER BY %s ROWS BETWEEN %d PRECEDING AND CURRENT ROW)",
		measureColumn, ti.dateColumn, periods-1,
	)
}

// CumulativeSumSQL returns a SQL expression for a cumulative (running) sum.
func (ti *TimeIntelligence) CumulativeSumSQL(measureColumn string) string {
	return fmt.Sprintf(
		"SUM(%s) OVER (ORDER BY %s ROWS UNBOUNDED PRECEDING)",
		measureColumn, ti.dateColumn,
	)
}

// ---------------------------------------------------------------------------
// Fiscal Calendar Helpers
// ---------------------------------------------------------------------------

// FiscalYear returns a SQL expression that computes the fiscal year number for
// the given date expression. When the fiscal year starts in April (month=4),
// any date from April onward belongs to the fiscal year labeled by the calendar
// year of that April; dates before April belong to the previous fiscal year.
//
// Example with fiscalYearStart=4:
//
//	March 2026 -> FY 2025 (EXTRACT(YEAR FROM date) - 1 + 1 = 2025+1? No.)
//
// The formula: if month >= fiscalYearStart then year, else year - 1.
// But fiscal year is often labeled as the ending year. For Indian FY starting Apr 2025,
// the label is FY 2025-26, and we return the starting year: 2025.
func (ti *TimeIntelligence) FiscalYear(dateExpr string) string {
	if ti.fiscalYearStart == 1 {
		return fmt.Sprintf("EXTRACT(YEAR FROM %s)::int", dateExpr)
	}
	return fmt.Sprintf(
		"(CASE WHEN EXTRACT(MONTH FROM %s) >= %d THEN EXTRACT(YEAR FROM %s) ELSE EXTRACT(YEAR FROM %s) - 1 END)::int",
		dateExpr, ti.fiscalYearStart,
		dateExpr, dateExpr,
	)
}

// FiscalQuarter returns a SQL expression that computes the fiscal quarter (1-4)
// for the given date expression, based on the configured fiscal year start month.
func (ti *TimeIntelligence) FiscalQuarter(dateExpr string) string {
	if ti.fiscalYearStart == 1 {
		return fmt.Sprintf("EXTRACT(QUARTER FROM %s)::int", dateExpr)
	}
	// Shift month so that fiscalYearStart becomes month 1, then compute quarter.
	// fiscal_month = ((calendar_month - fiscalYearStart + 12) % 12) + 1
	// fiscal_quarter = ceil(fiscal_month / 3)
	return fmt.Sprintf(
		"CEIL(((EXTRACT(MONTH FROM %s)::int - %d + 12) %% 12 + 1) / 3.0)::int",
		dateExpr, ti.fiscalYearStart,
	)
}

// FiscalMonth returns a SQL expression that computes the fiscal month (1-12)
// for the given date expression. Fiscal month 1 corresponds to fiscalYearStart.
func (ti *TimeIntelligence) FiscalMonth(dateExpr string) string {
	if ti.fiscalYearStart == 1 {
		return fmt.Sprintf("EXTRACT(MONTH FROM %s)::int", dateExpr)
	}
	return fmt.Sprintf(
		"((EXTRACT(MONTH FROM %s)::int - %d + 12) %% 12 + 1)",
		dateExpr, ti.fiscalYearStart,
	)
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

// ValidateConfig checks that the TimeIntelligence instance is properly configured.
func (ti *TimeIntelligence) ValidateConfig() error {
	if ti.dateColumn == "" {
		return errors.InvalidArgumentf("date column must not be empty")
	}
	if ti.fiscalYearStart < 1 || ti.fiscalYearStart > 12 {
		return errors.InvalidArgumentf("fiscal year start month must be between 1 and 12, got %d", ti.fiscalYearStart)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// quarterStartMonth returns the first month of the calendar quarter containing m.
func quarterStartMonth(m time.Month) time.Month {
	return time.Month(((int(m) - 1) / 3) * 3 + 1)
}

// fiscalYearStartDate returns the date when the fiscal year containing referenceDate starts.
func fiscalYearStartDate(referenceDate time.Time, fiscalYearStart int) time.Time {
	fyStartMonth := time.Month(fiscalYearStart)
	year := referenceDate.Year()
	if referenceDate.Month() < fyStartMonth {
		year--
	}
	return time.Date(year, fyStartMonth, 1, 0, 0, 0, 0, referenceDate.Location())
}

// fiscalQuarterStartDate returns the date when the fiscal quarter containing referenceDate starts.
func fiscalQuarterStartDate(referenceDate time.Time, fiscalYearStart int) time.Time {
	fyStart := fiscalYearStartDate(referenceDate, fiscalYearStart)

	// Calculate how many months into the fiscal year we are.
	monthsIntoFY := (int(referenceDate.Month()) - fiscalYearStart + 12) % 12
	// Quarter number (0-indexed).
	fqIndex := monthsIntoFY / 3
	// Start date of that fiscal quarter.
	return fyStart.AddDate(0, fqIndex*3, 0)
}
