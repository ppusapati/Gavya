package expression

// TimeGranularity for time intelligence operations.
type TimeGranularity string

const (
	GranularityDay           TimeGranularity = "day"
	GranularityWeek          TimeGranularity = "week"
	GranularityMonth         TimeGranularity = "month"
	GranularityQuarter       TimeGranularity = "quarter"
	GranularityYear          TimeGranularity = "year"
	GranularityFiscalMonth   TimeGranularity = "fiscal_month"
	GranularityFiscalQuarter TimeGranularity = "fiscal_quarter"
	GranularityFiscalYear    TimeGranularity = "fiscal_year"
)

// PeriodComparison holds SQL fragments for comparing two time periods.
type PeriodComparison struct {
	CurrentPeriodSQL  string
	PreviousPeriodSQL string
	GrowthSQL         string // percentage growth expression
}

// TimeIntelligenceFunction represents a named time function that can be referenced in reports.
type TimeIntelligenceFunction struct {
	Name        string
	Description string
	Category    string // "period_to_date", "same_period", "rolling", "growth", "moving_average", "fiscal"
	Parameters  []TimeIntelligenceParam
}

// TimeIntelligenceParam describes a parameter for a time intelligence function.
type TimeIntelligenceParam struct {
	Name     string
	Type     string // "date", "integer", "string"
	Required bool
	Default  string
}

// ListTimeIntelligenceFunctions returns all available time intelligence functions.
// Used by the expression builder UI to show available functions.
func ListTimeIntelligenceFunctions() []TimeIntelligenceFunction {
	return []TimeIntelligenceFunction{
		// Period-to-date
		{
			Name:        "YTD",
			Description: "Year-to-date: filters rows from the start of the calendar year to the reference date",
			Category:    "period_to_date",
			Parameters: []TimeIntelligenceParam{
				{Name: "reference_date", Type: "date", Required: true},
			},
		},
		{
			Name:        "QTD",
			Description: "Quarter-to-date: filters rows from the start of the calendar quarter to the reference date",
			Category:    "period_to_date",
			Parameters: []TimeIntelligenceParam{
				{Name: "reference_date", Type: "date", Required: true},
			},
		},
		{
			Name:        "MTD",
			Description: "Month-to-date: filters rows from the start of the month to the reference date",
			Category:    "period_to_date",
			Parameters: []TimeIntelligenceParam{
				{Name: "reference_date", Type: "date", Required: true},
			},
		},
		{
			Name:        "WTD",
			Description: "Week-to-date: filters rows from the start of the ISO week to the reference date",
			Category:    "period_to_date",
			Parameters: []TimeIntelligenceParam{
				{Name: "reference_date", Type: "date", Required: true},
			},
		},
		{
			Name:        "FYTD",
			Description: "Fiscal year-to-date: filters rows from the start of the fiscal year to the reference date",
			Category:    "period_to_date",
			Parameters: []TimeIntelligenceParam{
				{Name: "reference_date", Type: "date", Required: true},
			},
		},
		{
			Name:        "FQTD",
			Description: "Fiscal quarter-to-date: filters rows from the start of the fiscal quarter to the reference date",
			Category:    "period_to_date",
			Parameters: []TimeIntelligenceParam{
				{Name: "reference_date", Type: "date", Required: true},
			},
		},
		// Same period comparisons
		{
			Name:        "SPLY",
			Description: "Same period last year: shifts the current period range back by one year",
			Category:    "same_period",
			Parameters: []TimeIntelligenceParam{
				{Name: "reference_date", Type: "date", Required: true},
			},
		},
		{
			Name:        "SPLQ",
			Description: "Same period last quarter: shifts the current period range back by one quarter",
			Category:    "same_period",
			Parameters: []TimeIntelligenceParam{
				{Name: "reference_date", Type: "date", Required: true},
			},
		},
		{
			Name:        "SPLM",
			Description: "Same period last month: shifts the current period range back by one month",
			Category:    "same_period",
			Parameters: []TimeIntelligenceParam{
				{Name: "reference_date", Type: "date", Required: true},
			},
		},
		// Rolling windows
		{
			Name:        "RollingDays",
			Description: "Rolling window of the last N days ending at the reference date",
			Category:    "rolling",
			Parameters: []TimeIntelligenceParam{
				{Name: "n", Type: "integer", Required: true},
				{Name: "reference_date", Type: "date", Required: true},
			},
		},
		{
			Name:        "RollingWeeks",
			Description: "Rolling window of the last N weeks ending at the reference date",
			Category:    "rolling",
			Parameters: []TimeIntelligenceParam{
				{Name: "n", Type: "integer", Required: true},
				{Name: "reference_date", Type: "date", Required: true},
			},
		},
		{
			Name:        "RollingMonths",
			Description: "Rolling window of the last N months ending at the reference date",
			Category:    "rolling",
			Parameters: []TimeIntelligenceParam{
				{Name: "n", Type: "integer", Required: true},
				{Name: "reference_date", Type: "date", Required: true},
			},
		},
		{
			Name:        "RollingQuarters",
			Description: "Rolling window of the last N quarters ending at the reference date",
			Category:    "rolling",
			Parameters: []TimeIntelligenceParam{
				{Name: "n", Type: "integer", Required: true},
				{Name: "reference_date", Type: "date", Required: true},
			},
		},
		// Growth
		{
			Name:        "YoYGrowth",
			Description: "Year-over-year growth percentage using window functions (LAG over 12 monthly periods)",
			Category:    "growth",
			Parameters: []TimeIntelligenceParam{
				{Name: "measure_column", Type: "string", Required: true},
			},
		},
		{
			Name:        "QoQGrowth",
			Description: "Quarter-over-quarter growth percentage using window functions (LAG over 4 quarterly periods)",
			Category:    "growth",
			Parameters: []TimeIntelligenceParam{
				{Name: "measure_column", Type: "string", Required: true},
			},
		},
		{
			Name:        "MoMGrowth",
			Description: "Month-over-month growth percentage using window functions (LAG over 1 monthly period)",
			Category:    "growth",
			Parameters: []TimeIntelligenceParam{
				{Name: "measure_column", Type: "string", Required: true},
			},
		},
		// Moving averages
		{
			Name:        "MovingAverage",
			Description: "Moving average over the last N periods using a SQL window function",
			Category:    "moving_average",
			Parameters: []TimeIntelligenceParam{
				{Name: "measure_column", Type: "string", Required: true},
				{Name: "periods", Type: "integer", Required: true, Default: "3"},
			},
		},
		{
			Name:        "CumulativeSum",
			Description: "Cumulative (running) sum ordered by the date column",
			Category:    "moving_average",
			Parameters: []TimeIntelligenceParam{
				{Name: "measure_column", Type: "string", Required: true},
			},
		},
		// Fiscal calendar helpers
		{
			Name:        "FiscalYear",
			Description: "Returns the fiscal year number for a date expression, using the configured fiscal year start month",
			Category:    "fiscal",
			Parameters: []TimeIntelligenceParam{
				{Name: "date_expression", Type: "string", Required: true},
			},
		},
		{
			Name:        "FiscalQuarter",
			Description: "Returns the fiscal quarter (1-4) for a date expression, using the configured fiscal year start month",
			Category:    "fiscal",
			Parameters: []TimeIntelligenceParam{
				{Name: "date_expression", Type: "string", Required: true},
			},
		},
		{
			Name:        "FiscalMonth",
			Description: "Returns the fiscal month (1-12) for a date expression, using the configured fiscal year start month",
			Category:    "fiscal",
			Parameters: []TimeIntelligenceParam{
				{Name: "date_expression", Type: "string", Required: true},
			},
		},
		// Period comparison
		{
			Name:        "PeriodOverPeriod",
			Description: "Generates SQL for comparing a current period with a previous period, including growth calculation",
			Category:    "growth",
			Parameters: []TimeIntelligenceParam{
				{Name: "current_start", Type: "date", Required: true},
				{Name: "current_end", Type: "date", Required: true},
				{Name: "previous_start", Type: "date", Required: true},
				{Name: "previous_end", Type: "date", Required: true},
			},
		},
	}
}
