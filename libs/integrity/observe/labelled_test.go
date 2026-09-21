package observe

import (
	"strconv"
	"strings"
	"testing"
)

// What a labelled counter puts on the wire.
//
// The shape matters more here than for the plain kinds. Prometheus rejects a
// whole scrape on one malformed line, and rejecting it loses every other metric
// in the same document — so a counter that renders one bad series blinds the
// service, not itself.

// scraped is this file's shorthand for the helper in observe_test.go: these
// tests never record a request, so the Metrics they scrape is always a fresh
// one.
func scraped(t *testing.T) string {
	t.Helper()
	return scrape(t, NewMetrics())
}

func TestALabelledCounterRendersItsLabels(t *testing.T) {
	forgetGauges()
	t.Cleanup(forgetGauges)

	PublishLabelledCounter(LabelledCounter{
		Name:   "gavya_test_rows_total",
		Help:   "Rows, by table and operation.",
		Labels: []string{"table", "operation"},
		Read: func() []Series {
			return []Series{
				{Labels: []string{"collections", "SELECT"}, Value: 12},
				{Labels: []string{"animals", "INSERT"}, Value: 3},
			}
		},
	})

	body := scraped(t)
	for _, want := range []string{
		"# HELP gavya_test_rows_total Rows, by table and operation.",
		"# TYPE gavya_test_rows_total counter",
		`gavya_test_rows_total{table="animals",operation="INSERT"} 3`,
		`gavya_test_rows_total{table="collections",operation="SELECT"} 12`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the scrape does not contain:\n  %s\n\ngot:\n%s", want, body)
		}
	}
}

// Sorted, so two scrapes of an unchanged process are identical bytes.
//
// The same property every other block in the handler has. Without it a diff
// between two scrapes is noise and nobody can use one.
func TestLabelledSeriesComeOutSorted(t *testing.T) {
	forgetGauges()
	t.Cleanup(forgetGauges)

	PublishLabelledCounter(LabelledCounter{
		Name:   "gavya_test_sorted_total",
		Help:   "n",
		Labels: []string{"table", "operation"},
		Read: func() []Series {
			return []Series{
				{Labels: []string{"zebra", "SELECT"}, Value: 1},
				{Labels: []string{"animals", "UPDATE"}, Value: 1},
				{Labels: []string{"animals", "INSERT"}, Value: 1},
			}
		},
	})

	body := scraped(t)
	order := []string{
		`{table="animals",operation="INSERT"}`,
		`{table="animals",operation="UPDATE"}`,
		`{table="zebra",operation="SELECT"}`,
	}
	at := -1
	for _, want := range order {
		i := strings.Index(body, want)
		if i < 0 {
			t.Fatalf("%s is missing from the scrape", want)
		}
		if i < at {
			t.Errorf("%s came out before the series that should precede it", want)
		}
		at = i
	}
}

// Above the ceiling, the rest are left out and the number left out is published.
//
// A truncated metric that does not say it was truncated is a number somebody
// reads as the whole picture. That is the entire reason the ceiling is safe to
// have.
func TestTooManySeriesAreBoundedAndCounted(t *testing.T) {
	forgetGauges()
	t.Cleanup(forgetGauges)

	const over = 37
	PublishLabelledCounter(LabelledCounter{
		Name:   "gavya_test_many_total",
		Help:   "n",
		Labels: []string{"table", "operation"},
		Read: func() []Series {
			out := make([]Series, 0, MaxSeries+over)
			for i := 0; i < MaxSeries+over; i++ {
				out = append(out, Series{
					Labels: []string{string(rune('a'+i%26)) + strings.Repeat("x", i%7) + strconv.Itoa(i), "SELECT"},
					Value:  1,
				})
			}
			return out
		},
	})

	body := scraped(t)
	if got := strings.Count(body, "gavya_test_many_total{"); got != MaxSeries {
		t.Errorf("the scrape carries %d series and the ceiling is %d", got, MaxSeries)
	}
	if !strings.Contains(body, "gavya_test_many_total_series_dropped "+strconv.Itoa(over)) {
		t.Errorf("the scrape does not say %d series were left out:\n%s", over,
			tail(body, 400))
	}
}

// A series whose labels do not match the names is dropped, and counted.
//
// Rendered short it would be a malformed line, and one of those loses the whole
// document — so this is the case where dropping is the only safe answer and
// saying so is the only honest one.
func TestAMalformedSeriesIsDroppedRatherThanRendered(t *testing.T) {
	forgetGauges()
	t.Cleanup(forgetGauges)

	PublishLabelledCounter(LabelledCounter{
		Name:   "gavya_test_ragged_total",
		Help:   "n",
		Labels: []string{"table", "operation"},
		Read: func() []Series {
			return []Series{
				{Labels: []string{"collections", "SELECT"}, Value: 1},
				{Labels: []string{"only-one"}, Value: 2},
				{Labels: []string{"a", "b", "c"}, Value: 3},
			}
		},
	})

	body := scraped(t)
	if strings.Contains(body, "only-one") {
		t.Error("a series with too few labels was rendered; Prometheus rejects the " +
			"whole document on one malformed line")
	}
	if !strings.Contains(body, "gavya_test_ragged_total_series_dropped 2") {
		t.Errorf("the two malformed series were not counted as dropped:\n%s", tail(body, 300))
	}
	if !strings.Contains(body, `gavya_test_ragged_total{table="collections",operation="SELECT"} 1`) {
		t.Error("the well-formed series was lost along with the bad ones")
	}
}

// The dropped count is always there, including as zero.
//
// A line that appears only when something is wrong is a line nobody has a graph
// of at the moment it appears.
func TestTheDroppedCountIsAlwaysPublished(t *testing.T) {
	forgetGauges()
	t.Cleanup(forgetGauges)

	PublishLabelledCounter(LabelledCounter{
		Name:   "gavya_test_fine_total",
		Help:   "n",
		Labels: []string{"table"},
		Read:   func() []Series { return []Series{{Labels: []string{"x"}, Value: 1}} },
	})

	if !strings.Contains(scraped(t), "gavya_test_fine_total_series_dropped 0") {
		t.Error("a counter with nothing dropped does not publish a zero")
	}
}

// A counter naming no labels is refused.
//
// It is a Counter written in the wrong shape, and publishing it would produce
// `name{} 1` — which reads as the plain counter it should have been and is one
// more thing for a query to be wrong about.
func TestALabelledCounterWithNoLabelsIsRefused(t *testing.T) {
	forgetGauges()
	t.Cleanup(forgetGauges)

	PublishLabelledCounter(LabelledCounter{
		Name: "gavya_test_unlabelled_total",
		Help: "n",
		Read: func() []Series { return []Series{{Value: 1}} },
	})

	if strings.Contains(scraped(t), "gavya_test_unlabelled_total") {
		t.Error("a labelled counter with no label names was published")
	}
}

// Publishing the same name twice replaces rather than duplicates.
//
// Two lines with one name is a document Prometheus rejects, and rejecting it
// loses every other metric in the scrape — the same rule Publish follows.
func TestPublishingALabelledCounterTwiceReplacesIt(t *testing.T) {
	forgetGauges()
	t.Cleanup(forgetGauges)

	for _, v := range []float64{1, 2} {
		PublishLabelledCounter(LabelledCounter{
			Name:   "gavya_test_once_total",
			Help:   "n",
			Labels: []string{"table"},
			Read:   func() []Series { return []Series{{Labels: []string{"x"}, Value: v}} },
		})
	}

	body := scraped(t)
	if got := strings.Count(body, "# TYPE gavya_test_once_total counter"); got != 1 {
		t.Errorf("the counter was declared %d times", got)
	}
	if !strings.Contains(body, `gavya_test_once_total{table="x"} 2`) {
		t.Error("the second registration did not replace the first")
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
