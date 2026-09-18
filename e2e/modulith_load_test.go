//go:build e2e && loadtest

// The same morning, through the one door.
//
// e2e/load_test.go measures the twenty-nine process shape, calling milk-service
// directly — which is what a service does, and is not what anybody outside the
// platform does. The modulith is the shape that ships and had never been
// measured at all, so the figures the histogram buckets and two alert thresholds
// were chosen from describe a deployment nobody intends to run.
//
// It is the same work: a booth opens a session and records against it all
// morning. What differs is everything in front of the module, and that is the
// point of measuring it separately:
//
//   - a bearer token, verified on every single request, which in this shape
//     means a loopback call out of the process and back through the whole
//     middleware stack before the collection is even looked at;
//
//   - the gateway's own middleware, CORS and header stripping included;
//
//   - one process holding twenty-eight pools rather than twenty-nine processes
//     holding one each.
//
//     TEST_DATABASE_DSN="postgres://user@host:port/%s?sslmode=disable" \
//     go test -count=1 -tags 'e2e loadtest' -run TestTheMorningThroughTheModulith -v -timeout 30m ./...
//
// # WHAT IT MEASURED
//
// 18 September, same four-core machine, same hour as the re-measurement in
// load_test.go, so the two are comparable:
//
//	booths   p50      p99      throughput   (twenty-nine processes, same day)
//	10       5.1ms    41.7ms   1445/s       2.9-3.4ms  21-23ms  2134-2496/s
//	25       11.2ms   51.0ms   1860/s       7.4-7.6ms  26-68ms  2214-2907/s
//	50       22.5ms   73.8ms   2002/s       14.2-14.8  37-52ms  3130-3176/s
//
// The one door costs about half again on the median and about a third of the
// throughput. That is not a surprise and it is worth having the number: every
// request here is authenticated, and authenticating it means the gateway calling
// VerifySession — which in this shape leaves the process and comes back through
// the whole middleware stack, plus a session lookup, before the collection is
// looked at. Two milliseconds at the median is about one in-process round trip
// and one indexed read, which is what it should be.
//
// Nothing was refused and nothing accepted was lost at any of these, which
// matters more than the figures: the rate limiter is raised out of the way by the
// harness, so a refusal here would have been the platform and not the limit.
//
// It stays well inside what the alert rules and the histogram were tuned for:
// ProcedureSlow fires above a second at the ninety-ninth percentile, and the
// worst figure above is seventy-four milliseconds.
//
// Unlike load_test.go, this measurement includes the foreign key on
// milk_sessions.cattle_id — one of the twenty-two references that cross services
// — because this shape has one database, as both deployments do. That is the
// other reason to read this one for what a deployment does.
package e2e

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestTheMorningThroughTheModulith(t *testing.T) {
	caller := signedIn(t)
	booths, perBooth := loadShape()
	total := booths * perBooth

	// Each booth opens its session first, as in the other measurement: a booth
	// opens one and records against it all morning, so counting it would flatter
	// the figures with work that happens once.
	//
	// Real animals, not invented identifiers.
	//
	// milk_sessions.cattle_id is one of the twenty-two references that cross
	// services, and in one database it is a foreign key. The measurement in
	// load_test.go invents an id and gets away with it because that harness
	// gives each service a database of its own, so cattle-service's table is not
	// there and the key was never created — the same blind spot that hid the
	// invoices collision. Both deployments put everything in one database, so an
	// invented animal is refused there.
	type booth struct{ session, cattle string }
	opened := make([]booth, booths)
	for i := range opened {
		made := caller.call2xx(t, "cattle.v1.CattleService/CreateCattle", map[string]any{
			"tenant_id": modulithTenant, "tag_number": newID("tag"),
			"name": "Load", "gender": "F", "weight": 400.0, "created_by": "load",
		})
		animal, _ := made["cattle"].(map[string]any)
		if animal == nil {
			t.Fatalf("create the animal for booth %d: %v", i, made)
		}
		cattle := fmt.Sprint(animal["id"])
		out := caller.call2xx(t, "milk.v1.MilkService/CreateSession", map[string]any{
			"tenant_id": modulithTenant, "cattle_id": cattle,
			"shift_type": "morning", "timezone": tenantTimezone, "created_by": "load",
		})
		session, _ := out["session"].(map[string]any)
		if session == nil {
			t.Fatalf("open booth %d: %v", i, out)
		}
		opened[i] = booth{session: fmt.Sprint(session["id"]), cattle: cattle}
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
				code, body := caller.call(t, "milk.v1.MilkService/RecordMilk", map[string]any{
					"tenant_id": modulithTenant, "session_id": b.session,
					"cattle_id": b.cattle, "quantity_liters": 6.25, "created_by": "load",
				})
				took := time.Since(at)
				if code != http.StatusOK {
					failures[i] = fmt.Errorf("booth %d collection %d: %d %v", i, j, code, body)
					return
				}
				mine = append(mine, took)
			}
			latencies[i] = mine
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(started)

	// Errors first. A fast platform that refused half the morning is not a fast
	// platform, and through this shape a refusal can also mean the rate limiter
	// or a session that stopped verifying — both of which would read as latency
	// if only the timings were looked at.
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

	// Nothing was lost, which matters more than any figure here: concurrency
	// that drops a write produces a platform that is fast and wrong, and the
	// producer whose collection vanished is the one who finds out.
	for i, b := range opened {
		out := caller.call2xx(t, "milk.v1.MilkService/ListSessionRecords", map[string]any{
			"session_id": b.session, "tenant_id": modulithTenant,
		})
		records, _ := out["records"].([]any)
		if len(records) != perBooth {
			t.Errorf("booth %d recorded %d collections and %d are readable; the rest were "+
				"accepted and lost", i, perBooth, len(records))
		}
	}

	report(t, all, elapsed, booths)
}
