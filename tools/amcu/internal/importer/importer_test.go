package importer

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ppusapati/gavya/tools/amcu/internal/mapping"
	"github.com/ppusapati/gavya/tools/amcu/internal/profile"
	"github.com/ppusapati/gavya/tools/amcu/internal/source"
)

// A file in the shape a real collection export arrives in: a header, a society
// code, member numbers, dates written the way an Indian AMCU writes them, a
// shift column of M and E, and the measurements.
const goodFile = `SOCIETY,MEMBER,DATE,SHIFT,QTY,FAT,SNF,RATE,AMOUNT
0142,101,01/02/2026,M,12.500,4.15,8.60,42.5000,531.25
0142,102,01/02/2026,M,8.250,3.90,8.55,40.1000,330.83
0142,101,01/02/2026,E,11.000,4.20,8.62,42.8000,470.80
0142,103,01/02/2026,M,15.750,4.05,8.51,41.2000,648.90
0142,102,01/02/2026,E,9.500,3.95,8.58,40.4000,383.80
`

func read(t *testing.T, text string) *source.Table {
	t.Helper()
	tbl, err := source.Read(strings.NewReader(text))
	if err != nil {
		t.Fatalf("read the file: %v", err)
	}
	return tbl
}

// settled builds a profile from a file and answers the questions a person would
// have to answer, so the result is usable rather than a draft.
func settled(t *testing.T, text string) (*mapping.Profile, *source.Table) {
	t.Helper()
	tbl := read(t, text)
	p := mapping.Propose("TestVendor", tbl, profile.Profile(tbl))

	p.Format.DateLayout = "02/01/2006"
	p.Format.QuantityUnit = "L"
	p.Format.ShiftValues = map[string]string{"M": "MORNING", "E": "EVENING"}
	p.Unresolved = nil
	for i := range p.Fields {
		p.Fields[i].Confidence = 1
	}
	return p, tbl
}

func TestAGoodFileImports(t *testing.T) {
	p, tbl := settled(t, goodFile)
	res, err := Read(p, tbl)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(res.Collections) != 5 {
		t.Fatalf("%d collections, want 5 (%d rejected)", len(res.Collections), len(res.Rejected))
	}
	if len(res.Rejected) != 0 {
		t.Errorf("rejections from a clean file: %+v", res.Rejected)
	}

	c := res.Collections[0]
	if c.ProducerCode != "101" || c.Quantity != "12.500" || c.Fat != "4.15" {
		t.Errorf("first collection reads %+v", c)
	}
	if c.QuantityUnit != "L" {
		t.Errorf("quantity unit = %q, want the profile's declared L", c.QuantityUnit)
	}
	if c.Shift != "MORNING" {
		t.Errorf("shift = %q, want it mapped through the vendor's vocabulary", c.Shift)
	}
	if got := c.CollectedOn.Format("2006-01-02"); got != "2026-02-01" {
		t.Errorf("collected on %s, want the first of February read day-first", got)
	}
	// The line is the line of the file, counting the header, because "row 4,812
	// is wrong" is actionable and "one of the rows is wrong" is not.
	if c.Line != 2 {
		t.Errorf("first data row reports line %d, want 2", c.Line)
	}
}

// A quantity is a decimal in the file and must be a decimal in the platform.
// Through a float, 12.500 becomes the nearest binary value to it, and that
// difference multiplies into a rate.
func TestMeasurementsSurviveExactly(t *testing.T) {
	p, tbl := settled(t, goodFile)
	res, err := Read(p, tbl)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res.Collections {
		for name, v := range map[string]string{
			"quantity": c.Quantity, "fat": c.Fat, "snf": c.SNF,
			"rate": c.Rate, "amount": c.Amount,
		} {
			if strings.ContainsAny(v, "eE") {
				t.Errorf("line %d: %s reads %q, which is what a float renders as", c.Line, name, v)
			}
		}
	}
	if got := res.Collections[0].Rate; got != "42.5000" {
		t.Errorf("rate = %q, want the four decimal places the file wrote", got)
	}
}

// A draft profile is one where somebody has not yet said which way round the
// dates read or whether the quantity is litres. Importing through it would make
// those decisions silently, about somebody's milk payment.
func TestADraftProfileIsRefused(t *testing.T) {
	tbl := read(t, goodFile)
	draft := mapping.Propose("TestVendor", tbl, profile.Profile(tbl))
	if len(draft.Unresolved) == 0 {
		t.Fatal("the proposed profile has nothing unresolved, so this test proves nothing")
	}

	_, err := Read(draft, tbl)
	var e *ErrDraftProfile
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want a refusal naming what is unsettled", err)
	}
	if len(e.Unresolved) == 0 {
		t.Error("the refusal does not say what still has to be decided")
	}
}

// The failure this exists for. A vendor changes their export and two columns
// swap; both still hold plausible numbers, so every row imports and every
// payment is computed from the wrong measurement.
func TestAFileWhoseColumnsHaveMovedIsRefused(t *testing.T) {
	p, _ := settled(t, goodFile)

	// The same file with fat and SNF exchanged — which is exactly the pair a
	// vendor is most likely to reorder, and the pair whose values overlap.
	moved := `SOCIETY,MEMBER,DATE,SHIFT,QTY,SNF,FAT,RATE,AMOUNT
0142,101,01/02/2026,M,12.500,8.60,4.15,42.5000,531.25
0142,102,01/02/2026,M,8.250,8.55,3.90,40.1000,330.83
0142,101,01/02/2026,E,11.000,8.62,4.20,42.8000,470.80
0142,103,01/02/2026,M,15.750,8.51,4.05,41.2000,648.90
0142,102,01/02/2026,E,9.500,8.58,3.95,40.4000,383.80
`
	_, err := Read(p, read(t, moved))
	var e *ErrShapeChanged
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want a refusal; the columns moved", err)
	}
	if len(e.Differences) == 0 {
		t.Error("the refusal does not say what moved")
	}
	if !strings.Contains(e.Error(), "fat") && !strings.Contains(e.Error(), "snf") {
		t.Errorf("the refusal does not name the fields that moved: %v", e.Differences)
	}
}

// A row that cannot be read is held with its reason. Dropped, it leaves a file
// where nine rows in ten import cleanly and one vanishes — and nobody counts the
// rows.
func TestAnUnreadableRowIsHeldAndNotDropped(t *testing.T) {
	p, _ := settled(t, goodFile)

	withBadRows := `SOCIETY,MEMBER,DATE,SHIFT,QTY,FAT,SNF,RATE,AMOUNT
0142,101,01/02/2026,M,12.500,4.15,8.60,42.5000,531.25
0142,,01/02/2026,M,8.250,3.90,8.55,40.1000,330.83
0142,103,not-a-date,M,15.750,4.05,8.51,41.2000,648.90
0142,104,01/02/2026,M,,4.05,8.51,41.2000,648.90
0142,105,01/02/2026,X,9.500,3.95,8.58,40.4000,383.80
0142,106,01/02/2026,E,9.500,3.95,8.58,40.4000,383.80
`
	res, err := Read(p, read(t, withBadRows))
	if err != nil {
		t.Fatalf("the whole file was refused for four bad rows: %v", err)
	}
	if len(res.Collections) != 2 {
		t.Errorf("%d collections imported, want the 2 good rows", len(res.Collections))
	}
	if len(res.Rejected) != 4 {
		t.Fatalf("%d rejections, want 4: %+v", len(res.Rejected), res.Rejected)
	}

	reasons := map[string]bool{}
	for _, r := range res.Rejected {
		reasons[r.Reason] = true
		if r.Line == 0 {
			t.Error("a rejection does not say which line it was on")
		}
		if r.Detail == "" {
			t.Errorf("line %d was rejected with no explanation", r.Line)
		}
		if r.Raw == "" {
			t.Errorf("line %d was rejected without keeping what it said", r.Line)
		}
	}
	for _, want := range []string{"no producer", "unreadable date", "no quantity", "unknown shift"} {
		if !reasons[want] {
			t.Errorf("nothing was rejected for %q; reasons were %v", want, keys(reasons))
		}
	}
}

// A shift value the vendor's profile does not declare is a refusal, not a guess.
// Guessing which of M, 1, AM and MORNING means the morning is how a whole
// evening's milk lands in the wrong session.
func TestAnUndeclaredShiftValueIsNotGuessedAt(t *testing.T) {
	p, _ := settled(t, goodFile)
	odd := `SOCIETY,MEMBER,DATE,SHIFT,QTY,FAT,SNF,RATE,AMOUNT
0142,101,01/02/2026,AM,12.500,4.15,8.60,42.5000,531.25
0142,102,01/02/2026,PM,8.250,3.90,8.55,40.1000,330.83
0142,103,01/02/2026,AM,15.750,4.05,8.51,41.2000,648.90
0142,104,01/02/2026,PM,11.000,4.20,8.62,42.8000,470.80
0142,105,01/02/2026,AM,9.500,3.95,8.58,40.4000,383.80
`
	res, err := Read(p, read(t, odd))
	if err != nil {
		// A refusal at the shape check is also acceptable — either way nothing
		// was guessed.
		return
	}
	if len(res.Collections) != 0 {
		t.Errorf("%d rows imported with a shift vocabulary the profile does not declare",
			len(res.Collections))
	}
}

// The date layout comes from the profile, never from the value. 01/02/2026 is
// the first of February or the second of January and the file does not say
// which; reading it either way silently moves a month of collections.
func TestTheDateLayoutComesFromTheProfileAndNotTheValue(t *testing.T) {
	p, tbl := settled(t, goodFile)

	// The same file read month-first, which is what an American export would be.
	p.Format.DateLayout = "01/02/2006"
	res, err := Read(p, tbl)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Collections[0].CollectedOn.Format("2006-01-02"); got != "2026-01-02" {
		t.Errorf("with a month-first layout the date reads %s, want 2026-01-02 — the layout is "+
			"being taken from somewhere other than the profile", got)
	}
}

// -------------------------------------------------------------------------
// What reaches the platform
// -------------------------------------------------------------------------

func TestEachRecordKeepsWhereItCameFrom(t *testing.T) {
	p, tbl := settled(t, goodFile)
	res, err := Read(p, tbl)
	if err != nil {
		t.Fatal(err)
	}
	batch := BatchID(res.Vendor, res.Collections)
	records := res.Records("T_ALPHA", "D_IMPORT", batch, 1)

	if len(records) != len(res.Collections) {
		t.Fatalf("%d records for %d collections", len(records), len(res.Collections))
	}
	var got collectionPayload
	if err := json.Unmarshal(records[0].Payload, &got); err != nil {
		t.Fatal(err)
	}
	if got.SourceLine != 2 {
		t.Errorf("source line = %d, want 2", got.SourceLine)
	}
	if got.SourceHash == "" {
		t.Error("no hash of the source row, so a re-import cannot be recognised as one")
	}
	if got.ImportBatch != batch {
		t.Errorf("import batch = %q", got.ImportBatch)
	}
	if got.Vendor != "TestVendor" {
		t.Errorf("vendor = %q", got.Vendor)
	}
}

// The sequence is the line the row was on, not a counter over the rows that
// happened to import. A counter renumbers everything after a row that was later
// fixed, and the platform detects duplicates on (session, sequence) — so a
// corrected re-import would read as a different set of records rather than the
// same ones.
func TestTheSequenceIsTheLineAndNotACounter(t *testing.T) {
	p, _ := settled(t, goodFile)
	withGap := `SOCIETY,MEMBER,DATE,SHIFT,QTY,FAT,SNF,RATE,AMOUNT
0142,101,01/02/2026,M,12.500,4.15,8.60,42.5000,531.25
0142,,01/02/2026,M,8.250,3.90,8.55,40.1000,330.83
0142,103,01/02/2026,E,11.000,4.20,8.62,42.8000,470.80
0142,104,01/02/2026,M,15.750,4.05,8.51,41.2000,648.90
0142,105,01/02/2026,E,9.500,3.95,8.58,40.4000,383.80
`
	res, err := Read(p, read(t, withGap))
	if err != nil {
		t.Fatal(err)
	}
	records := res.Records("T_ALPHA", "D_IMPORT", "IB_1", 1)
	if len(records) != 4 {
		t.Fatalf("%d records, want 4", len(records))
	}
	// Line 3 was rejected, so the sequences skip it rather than closing up.
	want := []int64{2, 4, 5, 6}
	for i, r := range records {
		if r.Sequence != want[i] {
			t.Errorf("record %d has sequence %d, want %d — the sequence has been renumbered "+
				"around the rejected row", i, r.Sequence, want[i])
		}
	}
}

// Importing the same file twice must produce the same batch, so the platform's
// replay detection sees a replay. A timestamp or a random id would make every
// re-import look like new milk and pay for it again.
func TestTheSameFileTwiceProducesTheSameBatch(t *testing.T) {
	p, tbl := settled(t, goodFile)
	first, err := Read(p, tbl)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Read(p, read(t, goodFile))
	if err != nil {
		t.Fatal(err)
	}
	a := BatchID(first.Vendor, first.Collections)
	b := BatchID(second.Vendor, second.Collections)
	if a != b {
		t.Errorf("the same file produced batches %s and %s", a, b)
	}
	if len(a) != 26 {
		t.Errorf("batch id is %d characters, want the 26 an id column holds", len(a))
	}
}

// And a different file must produce a different one, or two days' collections
// would collide and the second would be discarded as a replay.
func TestADifferentFileProducesADifferentBatch(t *testing.T) {
	p, tbl := settled(t, goodFile)
	first, _ := Read(p, tbl)

	nextDay := strings.ReplaceAll(goodFile, "01/02/2026", "02/02/2026")
	second, err := Read(p, read(t, nextDay))
	if err != nil {
		t.Fatal(err)
	}
	if BatchID(first.Vendor, first.Collections) == BatchID(second.Vendor, second.Collections) {
		t.Error("two different days produced the same batch, so the second would be discarded " +
			"as a replay of the first")
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
