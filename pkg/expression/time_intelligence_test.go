package expression

import (
	"strings"
	"testing"
	"time"
)

func refDate(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// ---------------------------------------------------------------------------
// Period-to-Date
// ---------------------------------------------------------------------------

func TestYTD(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 4)
	ref := refDate(2026, time.January, 15)
	sql := ti.YTD(ref)

	assertContains(t, sql, "order_date >= '2026-01-01'")
	assertContains(t, sql, "order_date <= '2026-01-15'")
}

func TestYTD_MidYear(t *testing.T) {
	ti := NewTimeIntelligence("created_at", 1)
	ref := refDate(2026, time.July, 20)
	sql := ti.YTD(ref)

	assertContains(t, sql, "created_at >= '2026-01-01'")
	assertContains(t, sql, "created_at <= '2026-07-20'")
}

func TestQTD(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 4)

	tests := []struct {
		name      string
		ref       time.Time
		wantStart string
	}{
		{"Q1", refDate(2026, time.February, 15), "'2026-01-01'"},
		{"Q2", refDate(2026, time.May, 10), "'2026-04-01'"},
		{"Q3", refDate(2026, time.August, 1), "'2026-07-01'"},
		{"Q4", refDate(2026, time.November, 30), "'2026-10-01'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql := ti.QTD(tt.ref)
			assertContains(t, sql, "order_date >= "+tt.wantStart)
		})
	}
}

func TestMTD(t *testing.T) {
	ti := NewTimeIntelligence("sale_date", 4)
	ref := refDate(2026, time.March, 13)
	sql := ti.MTD(ref)

	assertContains(t, sql, "sale_date >= '2026-03-01'")
	assertContains(t, sql, "sale_date <= '2026-03-13'")
}

func TestWTD(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 4)
	// 2026-03-13 is a Friday
	ref := refDate(2026, time.March, 13)
	sql := ti.WTD(ref)

	// Monday of that week is March 9
	assertContains(t, sql, "order_date >= '2026-03-09'")
	assertContains(t, sql, "order_date <= '2026-03-13'")
}

func TestWTD_Monday(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 4)
	// 2026-03-09 is a Monday
	ref := refDate(2026, time.March, 9)
	sql := ti.WTD(ref)

	assertContains(t, sql, "order_date >= '2026-03-09'")
	assertContains(t, sql, "order_date <= '2026-03-09'")
}

func TestWTD_Sunday(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 4)
	// 2026-03-15 is a Sunday
	ref := refDate(2026, time.March, 15)
	sql := ti.WTD(ref)

	// Monday of that week is March 9
	assertContains(t, sql, "order_date >= '2026-03-09'")
	assertContains(t, sql, "order_date <= '2026-03-15'")
}

// ---------------------------------------------------------------------------
// Fiscal Year-to-Date
// ---------------------------------------------------------------------------

func TestFYTD_IndianFiscalYear(t *testing.T) {
	// Indian fiscal year starts April. Reference date June 15, 2026.
	ti := NewTimeIntelligence("invoice_date", 4)
	ref := refDate(2026, time.June, 15)
	sql := ti.FYTD(ref)

	assertContains(t, sql, "invoice_date >= '2026-04-01'")
	assertContains(t, sql, "invoice_date <= '2026-06-15'")
}

func TestFYTD_BeforeFiscalYearStart(t *testing.T) {
	// Indian fiscal year starts April. Reference date Feb 20, 2026.
	// FY started April 2025.
	ti := NewTimeIntelligence("invoice_date", 4)
	ref := refDate(2026, time.February, 20)
	sql := ti.FYTD(ref)

	assertContains(t, sql, "invoice_date >= '2025-04-01'")
	assertContains(t, sql, "invoice_date <= '2026-02-20'")
}

func TestFYTD_CalendarYear(t *testing.T) {
	// When fiscal year starts in January, FYTD == YTD.
	ti := NewTimeIntelligence("order_date", 1)
	ref := refDate(2026, time.June, 15)
	fytd := ti.FYTD(ref)
	ytd := ti.YTD(ref)

	if fytd != ytd {
		t.Errorf("with fiscal year start = January, FYTD should equal YTD\nFYTD: %s\nYTD:  %s", fytd, ytd)
	}
}

func TestFQTD(t *testing.T) {
	// Indian fiscal year starts April. Reference date August 15, 2026.
	// Fiscal Q2 starts July 1, 2026.
	ti := NewTimeIntelligence("order_date", 4)
	ref := refDate(2026, time.August, 15)
	sql := ti.FQTD(ref)

	assertContains(t, sql, "order_date >= '2026-07-01'")
	assertContains(t, sql, "order_date <= '2026-08-15'")
}

func TestFQTD_FirstFiscalQuarter(t *testing.T) {
	// Indian fiscal year starts April. Reference May 10, 2026 -> FQ1 starts April 1.
	ti := NewTimeIntelligence("order_date", 4)
	ref := refDate(2026, time.May, 10)
	sql := ti.FQTD(ref)

	assertContains(t, sql, "order_date >= '2026-04-01'")
	assertContains(t, sql, "order_date <= '2026-05-10'")
}

// ---------------------------------------------------------------------------
// Same Period Last Year/Quarter/Month
// ---------------------------------------------------------------------------

func TestSPLY(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 4)
	ref := refDate(2026, time.March, 13)
	sql := ti.SPLY(ref)

	assertContains(t, sql, "order_date >= '2025-01-01'")
	assertContains(t, sql, "order_date <= '2025-03-13'")
}

func TestSPLQ(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 4)
	ref := refDate(2026, time.May, 15)
	sql := ti.SPLQ(ref)

	// Current Q starts April 1. Previous Q starts January 1.
	assertContains(t, sql, "order_date >= '2026-01-01'")
	assertContains(t, sql, "order_date <= '2026-02-15'")
}

func TestSPLM(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 4)
	ref := refDate(2026, time.March, 13)
	sql := ti.SPLM(ref)

	assertContains(t, sql, "order_date >= '2026-02-01'")
	assertContains(t, sql, "order_date <= '2026-02-13'")
}

// ---------------------------------------------------------------------------
// Rolling Windows
// ---------------------------------------------------------------------------

func TestRollingDays(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 4)
	ref := refDate(2026, time.March, 13)
	sql := ti.RollingDays(30, ref)

	assertContains(t, sql, "order_date > '2026-02-11'")
	assertContains(t, sql, "order_date <= '2026-03-13'")
}

func TestRollingWeeks(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 4)
	ref := refDate(2026, time.March, 13)
	sql := ti.RollingWeeks(4, ref)

	// 4 weeks = 28 days before March 13 = Feb 13
	assertContains(t, sql, "order_date > '2026-02-13'")
	assertContains(t, sql, "order_date <= '2026-03-13'")
}

func TestRollingMonths(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 4)
	ref := refDate(2026, time.March, 13)
	sql := ti.RollingMonths(6, ref)

	assertContains(t, sql, "order_date > '2025-09-13'")
	assertContains(t, sql, "order_date <= '2026-03-13'")
}

func TestRollingQuarters(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 4)
	ref := refDate(2026, time.March, 13)
	sql := ti.RollingQuarters(2, ref)

	// 2 quarters = 6 months before March 13 = Sep 13 2025
	assertContains(t, sql, "order_date > '2025-09-13'")
	assertContains(t, sql, "order_date <= '2026-03-13'")
}

// ---------------------------------------------------------------------------
// Period Comparisons
// ---------------------------------------------------------------------------

func TestPeriodOverPeriod(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 4)
	cmp := ti.PeriodOverPeriod(
		refDate(2026, time.January, 1), refDate(2026, time.March, 31),
		refDate(2025, time.January, 1), refDate(2025, time.March, 31),
	)

	assertContains(t, cmp.CurrentPeriodSQL, "order_date >= '2026-01-01'")
	assertContains(t, cmp.CurrentPeriodSQL, "order_date <= '2026-03-31'")
	assertContains(t, cmp.PreviousPeriodSQL, "order_date >= '2025-01-01'")
	assertContains(t, cmp.PreviousPeriodSQL, "order_date <= '2025-03-31'")
	assertContains(t, cmp.GrowthSQL, "NULLIF(previous_period_total, 0)")
	assertContains(t, cmp.GrowthSQL, "100.0")
}

// ---------------------------------------------------------------------------
// Growth SQL
// ---------------------------------------------------------------------------

func TestYoYGrowthSQL(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 4)
	sql := ti.YoYGrowthSQL("total_revenue")

	assertContains(t, sql, "LAG(total_revenue, 12)")
	assertContains(t, sql, "OVER (ORDER BY order_date)")
	assertContains(t, sql, "NULLIF(LAG(total_revenue, 12)")
	assertContains(t, sql, "100.0")
}

func TestQoQGrowthSQL(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 4)
	sql := ti.QoQGrowthSQL("total_revenue")

	assertContains(t, sql, "LAG(total_revenue, 4)")
	assertContains(t, sql, "OVER (ORDER BY order_date)")
}

func TestMoMGrowthSQL(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 4)
	sql := ti.MoMGrowthSQL("total_revenue")

	assertContains(t, sql, "LAG(total_revenue, 1)")
	assertContains(t, sql, "OVER (ORDER BY order_date)")
}

// ---------------------------------------------------------------------------
// Moving Averages
// ---------------------------------------------------------------------------

func TestMovingAverageSQL(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 4)
	sql := ti.MovingAverageSQL("amount", 3)

	assertContains(t, sql, "AVG(amount)")
	assertContains(t, sql, "OVER (ORDER BY order_date ROWS BETWEEN 2 PRECEDING AND CURRENT ROW)")
}

func TestMovingAverageSQL_SinglePeriod(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 4)
	sql := ti.MovingAverageSQL("amount", 1)

	assertContains(t, sql, "ROWS BETWEEN 0 PRECEDING AND CURRENT ROW")
}

func TestMovingAverageSQL_InvalidPeriod(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 4)
	sql := ti.MovingAverageSQL("amount", 0)

	// Should default to 1 period (0 preceding)
	assertContains(t, sql, "ROWS BETWEEN 0 PRECEDING AND CURRENT ROW")
}

func TestCumulativeSumSQL(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 4)
	sql := ti.CumulativeSumSQL("amount")

	assertContains(t, sql, "SUM(amount)")
	assertContains(t, sql, "OVER (ORDER BY order_date ROWS UNBOUNDED PRECEDING)")
}

// ---------------------------------------------------------------------------
// Fiscal Calendar Helpers
// ---------------------------------------------------------------------------

func TestFiscalYear_India(t *testing.T) {
	// Indian fiscal year starts April.
	ti := NewTimeIntelligence("order_date", 4)
	sql := ti.FiscalYear("order_date")

	assertContains(t, sql, "EXTRACT(MONTH FROM order_date) >= 4")
	assertContains(t, sql, "EXTRACT(YEAR FROM order_date)")
	assertContains(t, sql, "EXTRACT(YEAR FROM order_date) - 1")
}

func TestFiscalYear_Australia(t *testing.T) {
	// Australian fiscal year starts July.
	ti := NewTimeIntelligence("txn_date", 7)
	sql := ti.FiscalYear("txn_date")

	assertContains(t, sql, "EXTRACT(MONTH FROM txn_date) >= 7")
	assertContains(t, sql, "EXTRACT(YEAR FROM txn_date) - 1")
}

func TestFiscalYear_USFederal(t *testing.T) {
	// US Federal fiscal year starts October.
	ti := NewTimeIntelligence("txn_date", 10)
	sql := ti.FiscalYear("txn_date")

	assertContains(t, sql, "EXTRACT(MONTH FROM txn_date) >= 10")
}

func TestFiscalYear_CalendarYear(t *testing.T) {
	// When fiscal year starts in January, it is just the calendar year.
	ti := NewTimeIntelligence("order_date", 1)
	sql := ti.FiscalYear("order_date")

	assertContains(t, sql, "EXTRACT(YEAR FROM order_date)")
	// Should NOT contain the CASE expression
	if strings.Contains(sql, "CASE") {
		t.Errorf("fiscal year with start=1 should not need CASE, got: %s", sql)
	}
}

func TestFiscalQuarter_India(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 4)
	sql := ti.FiscalQuarter("order_date")

	assertContains(t, sql, "CEIL")
	assertContains(t, sql, "EXTRACT(MONTH FROM order_date)")
}

func TestFiscalQuarter_CalendarYear(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 1)
	sql := ti.FiscalQuarter("order_date")

	assertContains(t, sql, "EXTRACT(QUARTER FROM order_date)")
}

func TestFiscalMonth_India(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 4)
	sql := ti.FiscalMonth("order_date")

	assertContains(t, sql, "EXTRACT(MONTH FROM order_date)")
	// April (month 4) should become fiscal month 1 via the formula
	// ((4 - 4 + 12) % 12 + 1) = (12 % 12 + 1) = 1
}

func TestFiscalMonth_CalendarYear(t *testing.T) {
	ti := NewTimeIntelligence("order_date", 1)
	sql := ti.FiscalMonth("order_date")

	assertContains(t, sql, "EXTRACT(MONTH FROM order_date)")
	if strings.Contains(sql, "%%") {
		t.Errorf("fiscal month with start=1 should not need modulo, got: %s", sql)
	}
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		col     string
		fy      int
		wantErr bool
	}{
		{"valid", "order_date", 4, false},
		{"empty column", "", 4, true},
		{"invalid month low", "order_date", 0, true},
		{"invalid month high", "order_date", 13, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use raw struct to bypass NewTimeIntelligence's normalization
			ti := &TimeIntelligence{dateColumn: tt.col, fiscalYearStart: tt.fy}
			err := ti.ValidateConfig()
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

func TestNewTimeIntelligence_DefaultsFiscalYear(t *testing.T) {
	ti := NewTimeIntelligence("d", 0)
	if ti.fiscalYearStart != 4 {
		t.Errorf("expected fiscal year start to default to 4, got %d", ti.fiscalYearStart)
	}

	ti = NewTimeIntelligence("d", 13)
	if ti.fiscalYearStart != 4 {
		t.Errorf("expected fiscal year start to default to 4 for out-of-range value, got %d", ti.fiscalYearStart)
	}
}

// ---------------------------------------------------------------------------
// ListTimeIntelligenceFunctions
// ---------------------------------------------------------------------------

func TestListTimeIntelligenceFunctions(t *testing.T) {
	fns := ListTimeIntelligenceFunctions()
	if len(fns) == 0 {
		t.Fatal("expected non-empty list of time intelligence functions")
	}

	// Check that all expected categories are present.
	categories := make(map[string]bool)
	for _, fn := range fns {
		categories[fn.Category] = true
		if fn.Name == "" {
			t.Error("found function with empty name")
		}
		if fn.Description == "" {
			t.Errorf("function %s has empty description", fn.Name)
		}
	}

	expectedCategories := []string{"period_to_date", "same_period", "rolling", "growth", "moving_average", "fiscal"}
	for _, cat := range expectedCategories {
		if !categories[cat] {
			t.Errorf("missing expected category: %s", cat)
		}
	}
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected SQL to contain %q\ngot: %s", needle, haystack)
	}
}
