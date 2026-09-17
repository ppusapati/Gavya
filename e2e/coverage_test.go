//go:build e2e

// Whether this suite still calls every route the platform serves.
//
// "261 of 261, covered end to end" was true on the afternoon somebody counted
// it, and nothing has recounted it since. A route added tomorrow gets an entry
// in the permission table — that much is gated — and is covered by nothing else,
// and the difference between a procedure nobody has exercised and one that has
// never worked is invisible until a society telephones about it.
//
// # COUNTED AT RUNTIME, NOT READ OUT OF THE SOURCE
//
// The first version of this scanned the suite's own source for
// `<serviceConst>+"/Method"`, which is how most of these calls are written. It
// reported twenty-one routes uncovered and nineteen of them were covered — by a
// constant with a digit on the end that the pattern did not match, by a method
// name that arrives in a loop variable, and by identity-service's tests, which
// post to a URL they build themselves. A coverage check that cannot read the
// call is a coverage check that reports gaps where there are none, and the usual
// end of that is an exception list that grows until the check means nothing.
//
// So it asks the services. Every one of them counts what it served, per
// procedure, because observe.Metrics has done that since the platform got
// readiness probes — and a procedure with a non-zero count is one this suite
// actually reached, whatever shape the call was written in. The measurement is
// taken from the same /metrics endpoint Prometheus scrapes, which means the
// thing being trusted here is a thing the platform relies on anyway.
//
// # AND THEN IT ASKED THE WRONG SERVICES
//
// The counting version reported three routes uncovered and two of them were
// covered, by tests that run against the ML-enabled platform: a second copy of
// three Go services, started by mlharness_test.go, which this swept up nowhere.
// Accepting a reconciliation run is one of them — it needs a run that converged,
// and converging needs the reconciler.
//
// Worth writing down rather than quietly fixing, because it is the failure this
// file is about, arriving inside the check itself: a coverage count is only as
// good as the set of things it asks, and a thing it does not know to ask reads
// exactly like a thing that never ran. It now asks both platforms.
//
// The third was real. ingestion-service's ListSessions had never been called by
// anything, and e2e/uncalled_test.go calls it now.
//
// # A SURVIVING MUTANT, RECORDED
//
// Taking the ML sweep back out no longer fails anything, and that is worth
// saying plainly rather than leaving as an unexplained passing test.
//
// It survives because uncalled_test.go went on to cover all three routes from
// the plain platform — a run that did not converge is what balance-service
// produces when the reconciler is absent, and a flagged observation can be made
// with SQL. So today every route is reached without the Rust tier, and both the
// sweep below and the leniency in reportRouteCoverage are insurance rather than
// load-bearing.
//
// Both are kept. The measurement's question is "what did this suite call", and
// answering it from one of the two platforms is wrong even on a day when the
// wrong answer happens to match — which is the same argument as everywhere else
// in this repository, and the reason the sweep exists at all is that on the
// first run it did not match.
package e2e

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/authz"
)

var served = struct {
	sync.Mutex
	procedures map[string]bool
}{procedures: map[string]bool{}}

// servedLine matches one counter in Prometheus text exposition.
var servedLine = regexp.MustCompile(
	`^gavya_requests_total\{procedure="([^"]+)",code="\d+"\} (\d+)$`)

// recordServedProcedures reads one service's counters before it goes away.
//
// Called for the shared platform's services in TestMain, and from
// identity-service's own cleanup — it starts a binary of its own per test and is
// dead by the time TestMain runs, so it has to be asked while it is up.
func recordServedProcedures(baseURL string) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(strings.TrimRight(baseURL, "/") + "/metrics")
	if err != nil {
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	served.Lock()
	defer served.Unlock()
	for _, line := range strings.Split(string(body), "\n") {
		m := servedLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		count, err := strconv.ParseUint(m[2], 10, 64)
		if err != nil || count == 0 {
			continue
		}
		// A procedure counted under any status. A refusal is a route reached:
		// several of these are only ever called to be refused, which is what the
		// authorisation and argument tests are for.
		if strings.Contains(m[1], "/") {
			served.procedures[m[1]] = true
		}
	}
}

// reportRouteCoverage returns what to print and whether it should fail the run.
//
// The two are separate because of the ML tier. Several routes are reached only
// from the ML-enabled platform — accepting a reconciliation needs a run that
// converged, and converging needs the reconciler — and that platform skips,
// loudly, where cargo is not on PATH. On such a machine those routes are
// genuinely uncovered and it is not the code's fault, so the count is reported
// and not enforced. Enforcing it would make this check fail according to what is
// installed, which is the thing that gets a check deleted.
//
// mlSkipped is the reason the ML platform did not run, empty when it did.
func reportRouteCoverage(mlSkipped string) (string, bool) {
	// A filtered run exercises whatever was asked for, so the answer would be
	// "most routes are uncovered" and would be useless. Checked rather than
	// assumed: this is the one thing that would make the whole check evaporate
	// silently if the suite were ever run under a filter in the gate.
	if f := flag.Lookup("test.run"); f != nil && f.Value.String() != "" {
		return "", false
	}

	served.Lock()
	reached := make(map[string]bool, len(served.procedures))
	for p := range served.procedures {
		reached[p] = true
	}
	served.Unlock()

	// Nothing reached at all means no test started the platform — a run where
	// the database was absent and every test skipped. Saying "261 routes are
	// uncovered" there would be true and would tell nobody anything.
	if len(reached) == 0 {
		return "", false
	}

	missing := routesNotIn(reached)
	if len(missing) == 0 {
		return "", false
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\nROUTE COVERAGE: %d of %d routes were never called by this suite.\n\n",
		len(missing), len(authz.Table()))
	for _, p := range missing {
		fmt.Fprintf(&b, "    %s\n", p)
	}
	b.WriteString("\nEach one is a procedure that has a permission, is registered by its " +
		"service, and has never been shown to work. That is the state several " +
		"procedures in this repository were found in — registered, reachable, and " +
		"failing on their first real call.\n")

	if mlSkipped != "" {
		fmt.Fprintf(&b, "\nReported and not enforced: the ML-enabled platform did not "+
			"run (%s), and some of these are reached only from it.\n", mlSkipped)
		return b.String(), false
	}
	return b.String(), true
}

// routesNotIn is the comparison itself, kept apart from the reading of it so
// that it can be tested against a set somebody made up.
//
// Without this the checker had nothing checking it: a version that always
// answered "nothing missing" would have passed every run of the suite and every
// mutation of everything else, which is the shape of control this whole
// repository is written against.
func routesNotIn(reached map[string]bool) []string {
	var missing []string
	for procedure := range authz.Table() {
		if !reached[procedure] {
			missing = append(missing, procedure)
		}
	}
	sort.Strings(missing)
	return missing
}

// The comparison, against sets whose answer is known.
//
// Needs no database and no platform: it is the one part of this file that can be
// tested directly, and it is the part that decides.
func TestTheCoverageComparisonNoticesAMissingRoute(t *testing.T) {
	everything := map[string]bool{}
	for procedure := range authz.Table() {
		everything[procedure] = true
	}
	if missing := routesNotIn(everything); len(missing) != 0 {
		t.Fatalf("a set holding every route reports %d missing: %v", len(missing), missing)
	}

	// One taken away, chosen rather than arbitrary: this is the route the count
	// found uncovered on its first run.
	const dropped = "ingestion.v1.IngestionService/ListSessions"
	if !everything[dropped] {
		t.Fatalf("%s is no longer in the permission table, so this test is comparing "+
			"against something that has moved", dropped)
	}
	delete(everything, dropped)

	missing := routesNotIn(everything)
	if len(missing) != 1 || missing[0] != dropped {
		t.Fatalf("with %s taken away the comparison reports %v", dropped, missing)
	}

	// And nothing reached at all is every route missing, rather than none. A
	// comparison that answered "complete" for an empty set would report success
	// on a run where the platform never started.
	if got := len(routesNotIn(map[string]bool{})); got != len(authz.Table()) {
		t.Errorf("an empty set reports %d routes missing, want all %d",
			got, len(authz.Table()))
	}
}
