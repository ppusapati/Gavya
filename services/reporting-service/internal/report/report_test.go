package report

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

type fakeCollections struct {
	rows []Collection
	// askedLimit records what the renderer asked for, so the test can show the
	// ceiling is applied at the source rather than after an unbounded read.
	askedLimit int
	askedFrom  time.Time
	askedTo    time.Time
	err        error
}

func (f *fakeCollections) Collections(_ context.Context, _ string, from, to time.Time, limit int) ([]Collection, error) {
	f.askedLimit, f.askedFrom, f.askedTo = limit, from, to
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

type fakePayables struct {
	rows  []Payable
	cycle Cycle
	err   error
}

func (f *fakePayables) Payables(context.Context, string, string) ([]Payable, Cycle, error) {
	if f.err != nil {
		return nil, Cycle{}, f.err
	}
	return f.rows, f.cycle, nil
}

type fakeDivergences struct{ rows []Divergence }

func (f *fakeDivergences) Divergences(context.Context, string, time.Time, time.Time, int) ([]Divergence, error) {
	return f.rows, nil
}

func parseCSV(t *testing.T, b []byte) [][]string {
	t.Helper()
	recs, err := csv.NewReader(strings.NewReader(string(b))).ReadAll()
	if err != nil {
		t.Fatalf("the report is not readable as CSV: %v", err)
	}
	return recs
}

// A type nobody wrote is refused by name, not rendered as an empty file.
//
// The empty file is the defect this catalogue exists to prevent: a report
// marked complete, zero rows, and nothing anywhere saying that the type was
// never one this platform produces.
func TestAnUnknownTypeIsRefusedAndNamesWhatThereIs(t *testing.T) {
	_, err := Render(context.Background(), &Sources{}, "daily_yield", "TEN", Params{})
	if err == nil {
		t.Fatal("an unknown report type rendered successfully")
	}
	if !errors.Is(err, ErrUnknownKind) {
		t.Errorf("failed with %v, which a caller cannot tell from a source being down", err)
	}
	for _, name := range []string{"collections", "settlement_summary", "divergences"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the refusal does not say that %s is available: %v", name, err)
		}
	}
}

// A missing source fails with the setting named, and does not render.
//
// An empty collections report and a collections report whose source could not
// be reached look identical on a screen, and only one of them means nobody
// delivered any milk.
func TestAMissingSourceFailsAndNamesTheSetting(t *testing.T) {
	for _, tc := range []struct{ kind, setting string }{
		{"collections", "PROCUREMENT_URL"},
		{"settlement_summary", "SETTLEMENT_URL"},
		{"divergences", "SHADOW_SETTLEMENT_URL"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			params := Params{"from": "2026-09-01", "to": "2026-09-15", "cycle_id": "CYC_1"}
			out, err := Render(context.Background(), &Sources{}, tc.kind, "TEN", params)
			if err == nil {
				t.Fatalf("%s rendered %d rows with no source configured", tc.kind, out.Rows)
			}
			if !errors.Is(err, ErrNoSource) {
				t.Errorf("failed with %v rather than ErrNoSource", err)
			}
			if !strings.Contains(err.Error(), tc.setting) {
				t.Errorf("the refusal does not name %s, so nobody knows what to set: %v",
					tc.setting, err)
			}
		})
	}
}

// A type that needs a parameter refuses without it rather than rendering
// whatever the empty value happens to mean.
func TestAMissingRequiredParameterIsRefused(t *testing.T) {
	src := &Sources{Payables: &fakePayables{}}
	_, err := Render(context.Background(), src, "settlement_summary", "TEN", Params{})
	if err == nil {
		t.Fatal("a settlement summary rendered with no cycle named")
	}
	if !errors.Is(err, ErrBadParameters) {
		t.Errorf("failed with %v rather than ErrBadParameters", err)
	}
	if !strings.Contains(err.Error(), "cycle_id") {
		t.Errorf("the refusal does not name the missing parameter: %v", err)
	}
}

// The period a report covers includes the last day named.
//
// 1 to 15 September means the whole of the 15th. An instant-to-instant reading
// gives midnight on the 15th and silently drops a day's milk — which, for a
// fortnightly settlement, is one shift of every producer's money.
func TestThePeriodIncludesTheLastDayNamed(t *testing.T) {
	f := &fakeCollections{}
	src := &Sources{Collections: f}

	_, err := Render(context.Background(), src, "collections", "TEN",
		Params{"from": "2026-09-01", "to": "2026-09-15"})
	if err != nil {
		t.Fatal(err)
	}

	wantFrom := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	wantTo := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	if !f.askedFrom.Equal(wantFrom) {
		t.Errorf("asked from %s, want %s", f.askedFrom, wantFrom)
	}
	if !f.askedTo.Equal(wantTo) {
		t.Errorf("asked to %s, want %s — the 15th has to be included in full",
			f.askedTo, wantTo)
	}
}

func TestABackwardsPeriodIsRefused(t *testing.T) {
	src := &Sources{Collections: &fakeCollections{}}
	_, err := Render(context.Background(), src, "collections", "TEN",
		Params{"from": "2026-09-15", "to": "2026-09-01"})
	if !errors.Is(err, ErrBadParameters) {
		t.Fatalf("a backwards period gave %v", err)
	}
}

// A report that stopped short says so.
//
// A truncated total somebody acts on is short by an amount nothing on the page
// discloses, so this is the flag the runner writes to its own column.
func TestATruncatedReportSaysSo(t *testing.T) {
	rows := make([]Collection, MaxRows+1)
	for i := range rows {
		rows[i] = Collection{ProducerRef: fmt.Sprintf("P%05d", i), CollectedOn: "2026-09-01"}
	}
	f := &fakeCollections{rows: rows}

	out, err := Render(context.Background(), &Sources{Collections: f}, "collections", "TEN",
		Params{"from": "2026-09-01", "to": "2026-09-01"})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Truncated {
		t.Error("more rows came back than the ceiling and the report does not say it stopped short")
	}
	if out.Rows != MaxRows {
		t.Errorf("kept %d rows, want %d", out.Rows, MaxRows)
	}
	if f.askedLimit <= MaxRows {
		t.Errorf("asked the source for %d rows; asking for exactly the ceiling makes a full "+
			"result ambiguous, so it has to ask for one more", f.askedLimit)
	}

	// And the rows it kept are really there, header included.
	recs := parseCSV(t, out.Content)
	if len(recs) != MaxRows+1 {
		t.Errorf("the file has %d lines; %d rows plus a header is %d", len(recs), MaxRows, MaxRows+1)
	}
}

// An answer that fits is not marked truncated.
func TestAWholeAnswerIsNotMarkedTruncated(t *testing.T) {
	f := &fakeCollections{rows: make([]Collection, 3)}
	out, err := Render(context.Background(), &Sources{Collections: f}, "collections", "TEN",
		Params{"from": "2026-09-01", "to": "2026-09-01"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Truncated {
		t.Error("three rows was reported as a truncated answer")
	}
	if out.Rows != 3 {
		t.Errorf("rows = %d, want 3", out.Rows)
	}
}

// Every figure reaches the file as the string the service sent.
//
// The whole platform holds money and quantities as exact decimals and marshals
// them as strings for that reason. A report is the last place they travel, and
// a renderer that parsed one to format it would undo the care at the final step
// — invisibly, because the number would still look plausible.
func TestFiguresReachTheFileUnaltered(t *testing.T) {
	f := &fakeCollections{rows: []Collection{{
		CollectedOn: "2026-09-01", Shift: "MORNING", ProducerRef: "PRD_1",
		Quantity: "6.250", QuantityUnit: "LITRES", Fat: "4.10", SNF: "8.500",
		Rate: "38.7500", Amount: "242.19", Currency: "INR",
	}}}

	out, err := Render(context.Background(), &Sources{Collections: f}, "collections", "TEN",
		Params{"from": "2026-09-01", "to": "2026-09-01"})
	if err != nil {
		t.Fatal(err)
	}
	recs := parseCSV(t, out.Content)
	if len(recs) != 2 {
		t.Fatalf("want a header and one row, got %d lines", len(recs))
	}
	row := strings.Join(recs[1], ",")
	for _, exact := range []string{"6.250", "4.10", "8.500", "38.7500", "242.19"} {
		if !strings.Contains(row, exact) {
			t.Errorf("%q is not in the row as written: %s", exact, row)
		}
	}
}

// A field holding a comma or a newline does not break the file.
func TestAwkwardTextIsQuotedRatherThanBreakingTheFile(t *testing.T) {
	f := &fakeCollections{rows: []Collection{{
		ProducerRef: "PRD_1",
		Explanation: `priced at 38.75, less a 2% deduction
and a note on a second line`,
	}}}
	out, err := Render(context.Background(), &Sources{Collections: f}, "collections", "TEN",
		Params{"from": "2026-09-01", "to": "2026-09-01"})
	if err != nil {
		t.Fatal(err)
	}
	recs := parseCSV(t, out.Content)
	if len(recs) != 2 {
		t.Fatalf("a comma and a newline in one field produced %d rows, want 2", len(recs))
	}
	if !strings.Contains(recs[1][13], "second line") {
		t.Errorf("the explanation did not survive: %q", recs[1][13])
	}
}

// A source that fails fails the report; nothing half-rendered is returned.
func TestASourceFailureFailsTheReport(t *testing.T) {
	f := &fakeCollections{err: errors.New("procurement is unreachable")}
	out, err := Render(context.Background(), &Sources{Collections: f}, "collections", "TEN",
		Params{"from": "2026-09-01", "to": "2026-09-01"})
	if err == nil {
		t.Fatalf("a failing source produced a report of %d rows", out.Rows)
	}
	if len(out.Content) != 0 {
		t.Error("a failing source produced content")
	}
}

func TestParseParams(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want Params
	}{
		{"", Params{}},
		{"{}", Params{}},
		{"null", Params{}},
		{`{"from":"2026-09-01"}`, Params{"from": "2026-09-01"}},
		{`{"limit":100}`, Params{"limit": "100"}},
		{`{"all":true}`, Params{"all": "true"}},
	} {
		got, err := ParseParams(tc.raw)
		if err != nil {
			t.Errorf("ParseParams(%q): %v", tc.raw, err)
			continue
		}
		for k, v := range tc.want {
			if got[k] != v {
				t.Errorf("ParseParams(%q)[%q] = %q, want %q", tc.raw, k, got[k], v)
			}
		}
	}

	for _, bad := range []string{`not json`, `[1,2]`, `{"a":{"b":1}}`, `{"a":[1]}`} {
		if _, err := ParseParams(bad); !errors.Is(err, ErrBadParameters) {
			t.Errorf("ParseParams(%q) was accepted or failed wrongly: %v", bad, err)
		}
	}
}

// A schedule's window is resolved in the schedule's own zone.
//
// "Yesterday" is a local day. A schedule firing at 02:00 in Asia/Kolkata is
// firing at 20:30 the previous day in UTC, so a runner resolving "yesterday" in
// UTC would produce the day before the one the society means — every time, and
// consistently enough that nobody would spot it from one report.
func TestAWindowIsResolvedInTheSchedulesZone(t *testing.T) {
	india, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	// 02:00 on 23 September in India is 20:30 on the 22nd in UTC.
	fired := time.Date(2026, 9, 23, 2, 0, 0, 0, india)

	inIndia, err := ResolveWindow(WindowYesterday, fired, india)
	if err != nil {
		t.Fatal(err)
	}
	if inIndia["from"] != "2026-09-22" || inIndia["to"] != "2026-09-22" {
		t.Errorf("yesterday in India on the 23rd is the 22nd; got %s to %s",
			inIndia["from"], inIndia["to"])
	}

	inUTC, err := ResolveWindow(WindowYesterday, fired, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if inUTC["from"] == inIndia["from"] {
		t.Error("the same firing resolved to the same day in two zones, so the zone was ignored")
	}
}

func TestScheduleWindows(t *testing.T) {
	loc := time.UTC
	fired := time.Date(2026, 9, 23, 7, 0, 0, 0, loc)

	for _, tc := range []struct{ keyword, from, to string }{
		{WindowYesterday, "2026-09-22", "2026-09-22"},
		{WindowLast7Days, "2026-09-16", "2026-09-22"},
		{WindowLastMonth, "2026-08-01", "2026-08-31"},
	} {
		got, err := ResolveWindow(tc.keyword, fired, loc)
		if err != nil {
			t.Errorf("%s: %v", tc.keyword, err)
			continue
		}
		if got["from"] != tc.from || got["to"] != tc.to {
			t.Errorf("%s gave %s to %s, want %s to %s",
				tc.keyword, got["from"], got["to"], tc.from, tc.to)
		}
	}

	// A window nobody wrote, and no window at all, are both refused with the
	// three that exist named.
	for _, keyword := range []string{"", "last_fortnight", "since_forever"} {
		_, err := ResolveWindow(keyword, fired, loc)
		if !errors.Is(err, ErrBadParameters) {
			t.Errorf("window %q gave %v", keyword, err)
		}
		if !strings.Contains(err.Error(), WindowLast7Days) {
			t.Errorf("the refusal for %q does not name the windows that exist: %v", keyword, err)
		}
	}
}

// A scheduled report has to be one a schedule can actually ask for.
//
// A settlement summary names a cycle. A schedule firing at two in the morning
// has no way to know which cycle is meant, so this is a property the catalogue
// declares rather than something discovered at two in the morning.
func TestOnlyPeriodBoundedTypesAreSchedulable(t *testing.T) {
	for _, k := range Catalogue() {
		if k.Schedulable && !k.Window {
			t.Errorf("%s is schedulable and is not bounded by a period, so a schedule has "+
				"nothing to give it", k.Name)
		}
		for _, need := range k.Needs {
			if k.Schedulable && need != "from" && need != "to" {
				t.Errorf("%s is schedulable and needs %q, which a schedule cannot supply",
					k.Name, need)
			}
		}
	}

	summary, err := Lookup("settlement_summary")
	if err != nil {
		t.Fatal(err)
	}
	if summary.Schedulable {
		t.Error("a settlement summary names a cycle and is marked schedulable")
	}
}

// Every type in the catalogue is described and renderable.
func TestTheCatalogueIsComplete(t *testing.T) {
	cat := Catalogue()
	if len(cat) == 0 {
		t.Fatal("the catalogue is empty")
	}
	for _, k := range cat {
		if k.Name == "" || k.Summary == "" {
			t.Errorf("%+v has no name or no summary", k)
		}
		if k.render == nil {
			t.Errorf("%s has no renderer, so requesting it would panic", k.Name)
		}
		if _, err := Lookup(k.Name); err != nil {
			t.Errorf("%s is in the catalogue and Lookup does not find it: %v", k.Name, err)
		}
	}
}

func TestSettlementSummaryAndDivergencesRender(t *testing.T) {
	src := &Sources{
		Payables: &fakePayables{
			cycle: Cycle{ID: "CYC_1", Name: "September, first fortnight",
				PeriodStart: "2026-09-01", PeriodEnd: "2026-09-15"},
			rows: []Payable{{ProducerRef: "PRD_1", Gross: "1200.00", Deducted: "200.00",
				Net: "1000.00", Currency: "INR", Status: "APPROVED"}},
		},
		Divergences: &fakeDivergences{rows: []Divergence{{
			ProducerRef: "PRD_1", Classification: "UNEXPLAINED", Delta: "-12.50",
			Currency: "INR", Status: "OPEN", NeedsReview: true, CreatedAt: "2026-09-16",
		}}},
	}

	summary, err := Render(context.Background(), src, "settlement_summary", "TEN",
		Params{"cycle_id": "CYC_1"})
	if err != nil {
		t.Fatal(err)
	}
	if recs := parseCSV(t, summary.Content); len(recs) != 2 || recs[1][8] != "1000.00" {
		t.Errorf("settlement summary row is wrong: %v", recs)
	}

	div, err := Render(context.Background(), src, "divergences", "TEN",
		Params{"from": "2026-09-01", "to": "2026-09-30"})
	if err != nil {
		t.Fatal(err)
	}
	recs := parseCSV(t, div.Content)
	if len(recs) != 2 {
		t.Fatalf("divergence report has %d lines, want 2", len(recs))
	}
	if recs[1][6] != "yes" {
		t.Errorf("needs_review was written as %q, want \"yes\"", recs[1][6])
	}
}
