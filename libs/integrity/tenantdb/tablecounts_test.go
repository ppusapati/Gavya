package tenantdb

import (
	"strings"
	"sync"
	"testing"

	"github.com/ppusapati/gavya/libs/integrity/observe"
)

// The per-table, per-operation counts, and the parser they share with the span
// name.
//
// The last requirement owed from the inherited library's list. The breakdown
// already existed in the traces — a span is named `db SELECT collections` — and
// as a metric it needed labels, which observe did not have.
//
// The span names themselves are covered by TestSpanNamesAreBoundedAndRefuseToGuess
// in trace_test.go; what is here is the split those names are now composed from,
// and the counting built on it.
// The span name is composed from the same two parts the labels are.
//
// Not a restatement of the case table above: this is the property that would
// break silently if somebody gave operationOf its own parser back, and it is
// checked against statements the table does not list.
func TestTheSpanNameAndTheLabelsCannotDisagree(t *testing.T) {
	for _, sql := range []string{
		"SELECT 1",
		"SELECT * FROM payables",
		"INSERT INTO audit_logs (id) VALUES ($1)",
		"WITH x AS (SELECT 1) SELECT * FROM x",
		"nonsense",
		"ROLLBACK TO SAVEPOINT s",
	} {
		verb, table := verbAndTable(sql)
		want := "db"
		switch {
		case verb != "" && table != "":
			want = "db " + verb + " " + table
		case verb != "":
			want = "db " + verb
		}
		if got := operationOf(sql); got != want {
			t.Errorf("operationOf(%q) = %q and its parts compose to %q", sql, got, want)
		}
	}
}

// What the per-table counters carry.
func TestTheTableCountsRecordWhatRan(t *testing.T) {
	forgetTableQueries()
	t.Cleanup(forgetTableQueries)

	recordTableQuery("SELECT", "collections", 0.010, false)
	recordTableQuery("SELECT", "collections", 0.020, false)
	recordTableQuery("INSERT", "collections", 0.005, true)
	recordTableQuery("BEGIN", "", 0.001, false)

	queries, seconds, failures := tableSeries()
	if got := valueFor(queries, "collections", "SELECT"); got != 2 {
		t.Errorf("two selects on collections were counted as %v", got)
	}
	if got := valueFor(seconds, "collections", "SELECT"); got != 0.030 {
		t.Errorf("their time came to %v, want 0.03", got)
	}
	if got := valueFor(failures, "collections", "INSERT"); got != 1 {
		t.Errorf("the failed insert was counted as %v failures", got)
	}
	if got := valueFor(failures, "collections", "SELECT"); got != 0 {
		t.Errorf("the selects succeeded and were counted as %v failures", got)
	}

	// A verb with no table goes somewhere that cannot be mistaken for one. The
	// transaction overhead is a real answer to "where is the time going".
	if got := valueFor(queries, "(none)", "BEGIN"); got != 1 {
		t.Errorf("the BEGIN was counted as %v under (none)", got)
	}
}

// A statement the parser does not recognise is not counted here.
//
// It is still in the three totals beside these. What it must not be is a row
// labelled "unknown" on a metric whose whole purpose is to say which table is
// slow — that answers the question wrongly rather than not at all.
func TestAnUnrecognisedStatementIsNotGivenATable(t *testing.T) {
	forgetTableQueries()
	t.Cleanup(forgetTableQueries)

	recordTableQuery("", "", 0.5, false)

	queries, _, _ := tableSeries()
	if len(queries) != 0 {
		t.Errorf("an unrecognised statement produced %d series: %v", len(queries), queries)
	}
}

// The map stops growing, and the combinations already in it go on counting.
//
// Preferred to evicting: a counter that forgets is a counter whose rate() is
// wrong in a way nobody can see.
func TestTheTableCountsAreBounded(t *testing.T) {
	forgetTableQueries()
	t.Cleanup(forgetTableQueries)

	recordTableQuery("SELECT", "first_table", 0.001, false)
	for i := 0; i < MaxTableSeries*2; i++ {
		recordTableQuery("SELECT", "table_"+strings.Repeat("x", i%5)+itoa(i), 0.001, false)
	}
	recordTableQuery("SELECT", "first_table", 0.001, false)

	queries, _, _ := tableSeries()
	if len(queries) > MaxTableSeries {
		t.Errorf("the map grew to %d combinations and the ceiling is %d",
			len(queries), MaxTableSeries)
	}
	if got := valueFor(queries, "first_table", "SELECT"); got != 2 {
		t.Errorf("a combination already being counted stopped counting once the "+
			"ceiling was reached: first_table/SELECT is %v, want 2", got)
	}
}

// Several connections record at once, and nothing is lost.
//
// pgx calls the tracer from whichever goroutine ran the query, and a pool of
// eight has eight of them. Run with -race.
func TestConcurrentRecordingKeepsEveryQuery(t *testing.T) {
	forgetTableQueries()
	t.Cleanup(forgetTableQueries)

	const writers, each = 8, 500
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < each; i++ {
				recordTableQuery("SELECT", "collections", 0.001, false)
			}
		}()
	}
	wg.Wait()

	queries, seconds, _ := tableSeries()
	if got := valueFor(queries, "collections", "SELECT"); got != writers*each {
		t.Errorf("%d queries were recorded and %v were counted", writers*each, got)
	}
	// Floating point, so a tolerance rather than an equality — what is being
	// checked is that no increment was lost, not the last bit of the sum.
	want := float64(writers*each) * 0.001
	if got := valueFor(seconds, "collections", "SELECT"); got < want*0.999 || got > want*1.001 {
		t.Errorf("their time came to %v, want about %v", got, want)
	}
}

// valueFor is the sample for one table and operation, or -1 when there is none.
func valueFor(series []observe.Series, table, operation string) float64 {
	for _, s := range series {
		if len(s.Labels) == 2 && s.Labels[0] == table && s.Labels[1] == operation {
			return s.Value
		}
	}
	return -1
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
