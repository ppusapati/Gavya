//go:build e2e && loadtest

// The morning collection.
//
// This platform has exactly one predictable load spike and it is the moment it
// must not fail: every booth in a society records its collections inside the
// same half hour, twice a day, and a producer who cannot be recorded has to be
// written on paper and typed in later — which is the failure the platform exists
// to remove.
//
// Nothing measured it. There were no benchmarks of any kind, so "it is fast
// enough" was a belief about code nobody had run under load, and the metrics
// package refused to keep a latency histogram on the grounds that choosing
// buckets before anybody had watched it run would be inventing a latency profile
// rather than measuring one. That was right, and this is the watching.
//
// It is behind its own build tag rather than in the gate. The gate runs on every
// push and this takes minutes and wants a quiet machine; a load figure taken on
// a runner that is building something else is a number that will be argued with
// rather than acted on.
//
//	TEST_DATABASE_DSN="postgres://user@host:port/%s?sslmode=disable" \
//	  go test -count=1 -tags 'e2e loadtest' -run TestTheMorningCollection -v -timeout 30m ./...
//
// -count=1 is not optional and this is the reason it is written here rather than
// assumed: the first attempt to check whether a result reproduced ran the test
// twice and got figures identical to the tenth of a millisecond, because the
// second run was Go's test cache handing back the first one's output. A load
// test that reports a cached number is worse than no load test.
//
// # WHAT IT MEASURED
//
// On a four-core machine running all twenty-nine services and their PostgreSQL
// side by side, recording one thousand collections:
//
//	booths   p50      p99      max      throughput
//	10       3.4ms    7.3ms    11.4ms   2787/s
//	25       8.6ms    16.4ms   20.3ms   2796/s
//	50       17.2ms   27.7ms   83.9ms   2334-2822/s
//
// Throughput is flat from ten booths upward and latency rises in proportion to
// concurrency, which is the signature of a server at its service rate rather
// than one falling over: the box does about 2,800 collections a second and each
// booth waits its turn. Nothing was refused and nothing accepted was lost at any
// of these.
//
// One thing worth recording because it wasted an hour. The very first run showed
// a bimodal distribution — 95% under 25ms and 5% at 2.5 seconds — which looked
// like a fixed timer somewhere. It was not reproducible: it was cold start, with
// the databases being created and every cache empty. The lesson is the one this
// repository keeps relearning, that a single measurement is an anecdote; the
// second and third runs are what made this a baseline.
//
// Two things it does not measure, and both are deliberate. The rate limiter is
// raised out of the way by the harness, because the question here is where the
// platform saturates rather than where the limiter stops it — the limiter's own
// behaviour is tested elsewhere. And every service runs on this one machine
// beside its own PostgreSQL, so the figures are a shape rather than a capacity:
// they say how latency behaves as concurrency rises, not how many booths a given
// server will hold.
package e2e

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

// The shape of a morning, tunable because the interesting runs are the ones at
// numbers nobody chose in advance.
//
// The defaults are one middling society: fifty collection points, twenty
// producers each. A thousand collections is enough for a hundredth percentile to
// mean something and short enough that somebody will actually run it.
func loadShape() (booths, perBooth int) {
	booths = envInt("GAVYA_LOAD_BOOTHS", 50)
	perBooth = envInt("GAVYA_LOAD_PER_BOOTH", 20)
	return booths, perBooth
}

func envInt(name string, fallback int) int {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func TestTheMorningCollection(t *testing.T) {
	p := startPlatform(t)
	booths, perBooth := loadShape()
	total := booths * perBooth

	// Each booth opens its session first. Session creation is not part of what
	// is measured: a booth opens one and then records against it all morning, so
	// counting it would flatter the figures with work that happens once.
	type booth struct {
		session string
		cattle  string
	}
	opened := make([]booth, booths)
	for i := range opened {
		cattle := newID("cow")
		sess, err := svcclient.Call[createMilkSessionReq, milkSessionResp](
			context.Background(), p.milk(), milkSvc+"/CreateSession",
			createMilkSessionReq{TenantID: p.tenant, CattleID: cattle,
				ShiftType: "morning", Timezone: tenantTimezone, CreatedBy: "load"}, p.opts())
		if err != nil {
			t.Fatalf("open booth %d: %v", i, err)
		}
		opened[i] = booth{session: sess.Session.ID, cattle: cattle}
	}

	latencies := make([][]time.Duration, booths)
	failures := make([]error, booths)
	var wg sync.WaitGroup

	started := time.Now()
	for i := range opened {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b := opened[i]
			mine := make([]time.Duration, 0, perBooth)
			for j := range perBooth {
				at := time.Now()
				_, err := svcclient.Call[recordMilkReq, milkRecordResp](
					context.Background(), p.milk(), milkSvc+"/RecordMilk",
					recordMilkReq{TenantID: p.tenant, SessionID: b.session,
						CattleID: b.cattle, QuantityLiters: 6.25, CreatedBy: "load"},
					p.opts())
				took := time.Since(at)
				if err != nil {
					failures[i] = fmt.Errorf("booth %d collection %d: %w", i, j, err)
					return
				}
				mine = append(mine, took)
			}
			latencies[i] = mine
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(started)

	// Errors first, because a fast platform that refused half the morning is not
	// a fast platform.
	for _, err := range failures {
		if err != nil {
			t.Fatalf("a collection was refused under load, which on a real morning is a "+
				"producer sent away to be typed in later: %v", err)
		}
	}

	all := make([]time.Duration, 0, total)
	for _, l := range latencies {
		all = append(all, l...)
	}
	if len(all) != total {
		t.Fatalf("recorded %d collections and expected %d", len(all), total)
	}

	// Nothing was lost. This is the assertion that matters more than any
	// latency figure: concurrency that drops a write produces a platform that is
	// fast and wrong, and the producer whose collection vanished is the one who
	// finds out.
	for i, b := range opened {
		list, err := svcclient.Call[listRecordsReq, listRecordsResp](
			context.Background(), p.milk(), milkSvc+"/ListSessionRecords",
			listRecordsReq{SessionID: b.session, TenantID: p.tenant}, p.opts())
		if err != nil {
			t.Fatalf("read booth %d back: %v", i, err)
		}
		if len(list.Records) != perBooth {
			t.Errorf("booth %d recorded %d collections and %d are readable; the rest were "+
				"accepted and lost", i, perBooth, len(list.Records))
		}
	}

	report(t, all, elapsed, booths)
}

// report prints the distribution, and prints it as buckets because the point of
// running this is to choose them.
//
// libs/integrity/observe keeps no latency histogram, deliberately: buckets
// chosen before anybody had watched this run would have been an invented latency
// profile. These are the measurements that let somebody choose.
func report(t *testing.T, all []time.Duration, elapsed time.Duration, booths int) {
	t.Helper()
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })

	var sum time.Duration
	for _, d := range all {
		sum += d
	}

	t.Logf("%d collections from %d booths at once, in %s (%.0f/s)",
		len(all), booths, elapsed.Round(time.Millisecond),
		float64(len(all))/elapsed.Seconds())
	t.Logf("  mean %s", (sum / time.Duration(len(all))).Round(100*time.Microsecond))
	for _, q := range []float64{50, 90, 95, 99, 99.9} {
		t.Logf("  p%-5s %s", strconv.FormatFloat(q, 'f', -1, 64), percentile(all, q).Round(100*time.Microsecond))
	}
	t.Logf("  max   %s", all[len(all)-1].Round(100*time.Microsecond))

	// The distribution across candidate buckets. Prometheus histogram buckets are
	// cumulative upper bounds, and the useful set is the one where the interesting
	// part of this curve is not all in one bucket.
	t.Log("  distribution, cumulative:")
	for _, bound := range candidateBuckets {
		n := sort.Search(len(all), func(i int) bool { return all[i] > bound })
		t.Logf("    <= %-8s %5d  %5.1f%%", bound, n, 100*float64(n)/float64(len(all)))
	}
}

// candidateBuckets are the bounds this run reports against, and the ones a
// histogram would use if the numbers say they fit.
var candidateBuckets = []time.Duration{
	1 * time.Millisecond,
	2500 * time.Microsecond,
	5 * time.Millisecond,
	10 * time.Millisecond,
	25 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	250 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	2500 * time.Millisecond,
	5 * time.Second,
}

func percentile(sorted []time.Duration, q float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := int(q / 100 * float64(len(sorted)))
	if i >= len(sorted) {
		i = len(sorted) - 1
	}
	return sorted[i]
}
