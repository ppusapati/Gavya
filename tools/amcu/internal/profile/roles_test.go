package profile

import "testing"

func col(header string, kind Kind, p5, p95 float64, decimals int) Column {
	return Column{Header: header, Kind: kind, P5: p5, P95: p95, Min: p5, Max: p95,
		Decimals: decimals, NonEmpty: 1000, Distinct: 400, DecimalHistogram: map[int]int{}}
}

// The defect this replaced: fat and SNF were tested in order against ranges that
// overlap, so a column sitting at 7.8–9.2 — squarely SNF — matched fat first and
// the two ended up swapped. Swapped, every payment is computed from the wrong
// measurement and nothing downstream can tell.
func TestFatAndSNFAreNotSwapped(t *testing.T) {
	fat := col("F1", KindDecimal, 3.4, 8.2, 2)
	snf := col("F2", KindDecimal, 7.9, 9.1, 2)

	if r, _, _ := roleFromValues(fat); r != RoleFat {
		t.Errorf("a column at 3.4–8.2 read as %s, want fat_percent", r)
	}
	if r, _, why := roleFromValues(snf); r != RoleSNF {
		t.Errorf("a column at 7.9–9.1 read as %s, want snf_percent (%s)", r, why)
	}
}

// One failed analyser reading must not redefine a column. Every real export has
// them, and using min/max meant a single 19.4 pushed a fat column out of its own
// band and into being read as a quantity.
func TestOneBadReadingDoesNotChangeWhatAColumnIs(t *testing.T) {
	c := col("FAT", KindDecimal, 3.4, 8.2, 2)
	c.Min, c.Max = 3.2, 19.4 // the outlier is in the column, outside the percentiles

	if r, _, why := roleFromValues(c); r != RoleFat {
		t.Errorf("a fat column containing one 19.4 read as %s (%s)", r, why)
	}
}

func TestALactometerReadingIsNotAPercentage(t *testing.T) {
	if r, _, _ := roleFromValues(col("LR", KindDecimal, 26.0, 30.5, 1)); r != RoleCLR {
		t.Errorf("a column at 26–30.5 read as %s, want clr", r)
	}
}

func TestAQuantityIsNotMistakenForAMeasurement(t *testing.T) {
	// Deliveries overlap the fat band numerically but spread far wider.
	c := col("QTY", KindDecimal, 2.8, 17.1, 2)
	c.Mean = 9.8
	if r, _, why := roleFromValues(c); r != RoleQuantity {
		t.Errorf("a delivery column read as %s (%s)", r, why)
	}
}

// The values decide, and the disagreement is reported rather than resolved
// silently — vendors do label a rate column FAT.
func TestValuesOutrankAMisleadingHeader(t *testing.T) {
	cols := []Column{col("FAT", KindDecimal, 7.9, 9.1, 2)}
	inferRoles(cols)
	if cols[0].Role != RoleSNF {
		t.Fatalf("role = %s, want the values to win over the header", cols[0].Role)
	}
	if cols[0].RoleReason == "" {
		t.Error("the disagreement with the header was not reported")
	}
}

func TestTwoColumnsCannotBothBeFat(t *testing.T) {
	cols := []Column{
		col("FAT", KindDecimal, 3.4, 8.2, 2),
		col("FAT2", KindDecimal, 3.5, 8.0, 2),
	}
	inferRoles(cols)
	n := 0
	for _, c := range cols {
		if c.Role == RoleFat {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d columns were assigned fat_percent; one of them is something else", n)
	}
}

// A value like 4.30 written as "4.3" is trailing-zero suppression, not a change
// in precision. About one in ten two-decimal readings ends in zero, so flagging
// it would bury every real finding under noise.
func TestTrailingZeroesAreNotAPrecisionChange(t *testing.T) {
	c := col("FAT", KindDecimal, 3.4, 8.2, 2)
	c.DecimalHistogram = map[int]int{2: 900, 1: 100} // exactly what suppression looks like

	if changed, detail := c.PrecisionChanged(); changed {
		t.Errorf("trailing zeroes were reported as a precision change: %s", detail)
	}
}

func TestARealPrecisionChangeIsReported(t *testing.T) {
	c := col("FAT", KindDecimal, 3.4, 8.2, 2)
	c.DecimalHistogram = map[int]int{2: 640, 1: 360} // far more than suppression explains

	changed, detail := c.PrecisionChanged()
	if !changed {
		t.Fatal("a third of the file at lower precision was not reported")
	}
	if detail == "" {
		t.Error("the finding does not say what the distribution was")
	}
}

// A date that parses both ways has to be flagged, because reading it wrongly
// moves a month of collections onto the wrong days.
func TestAnAmbiguousDateIsNotResolvedSilently(t *testing.T) {
	c := Column{Kind: KindDate, DateFormats: []string{"01/02/2006", "02/01/2006"}}
	amb, formats := c.Ambiguous()
	if !amb {
		t.Fatal("a date parsing both day-first and month-first was not flagged")
	}
	if len(formats) != 2 {
		t.Errorf("the finding does not name both readings: %v", formats)
	}

	// One layout is not ambiguous.
	if amb, _ := (Column{Kind: KindDate, DateFormats: []string{"2006-01-02"}}).Ambiguous(); amb {
		t.Error("an unambiguous date was flagged")
	}
}

func TestShiftColumnsAreRecognisedByTheirValues(t *testing.T) {
	c := Column{Kind: KindText, NonEmpty: 500, Distinct: 2, MaxLength: 1,
		TopValues: []ValueCount{{"M", 250}, {"E", 250}}}
	if r, _, _ := roleFromValues(c); r != RoleShift {
		t.Errorf("a column of M and E read as %s, want shift", r)
	}
}
