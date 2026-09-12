// Package serve builds the HTTP server every service in this platform runs.
//
// It exists because three things had to be true of all twenty-nine and were
// true of none:
//
//   - Every procedure is behind an authorisation check. Wrapping the mux here
//     means a service cannot serve a route it forgot to guard, in the same way
//     that putting tenant scoping in connectjson means a handler cannot forget
//     that either.
//   - Every server has timeouts. None of them did. A handful of slow
//     connections will otherwise hold sockets until the process runs out, and
//     the process that runs out is the one taking the morning's collections.
//   - Every server speaks h2c, which they already did, one copy at a time.
//   - Every server bounds how fast one caller can ask. Not a defence against a
//     distributed attacker, and not meant to be: it stops one misbehaving client
//     from starving everyone else, which is the failure a co-operative actually
//     meets.
//   - Every server can be watched. /readyz asks the service's dependencies and
//     /metrics says what it has been doing. Both are registered here, so a
//     service cannot be deployed without them and a probe cannot be pointed at
//     an endpoint that does not exist.
//
// A service now names its address and its mux and gets a server with all three.
// The alternative — a note in a README asking twenty-nine main functions to
// remember — is the arrangement that produced the gap.
package serve

import (
	"net/http"
	"os"
	"strconv"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/ppusapati/gavya/libs/integrity/authz"
	"github.com/ppusapati/gavya/libs/integrity/observe"
	"github.com/ppusapati/gavya/libs/integrity/ratelimit"
)

// The general rate, per caller.
//
// Generous, because it is not the sign-in limit — that one is stricter and lives
// in identity-service, where failures rather than attempts are counted. This is
// the bound that stops one client hammering a service, and a booth recording a
// morning's collections must never come near it: a limit that catches ordinary
// work is a limit somebody turns off.
//
// The burst is what lets a page that fetches a dozen things load at once.
const (
	RequestsPerSecond = 50
	RequestBurst      = 200
	// How many callers to remember. An unbounded map keyed on an address is
	// itself the denial of service.
	TrackedClients = 8192
)

// The timeouts.
//
// ReadHeaderTimeout is the one that matters for the attack: it bounds how long a
// connection may dribble out its headers, which is how a few dozen sockets
// become all of them.
//
// WriteTimeout is generous because a settlement print or a recall trace over a
// large plant is genuinely slow, and a report that times out halfway is worse
// than one that takes a minute. It is a bound, not a target.
const (
	ReadHeaderTimeout = 10 * time.Second
	ReadTimeout       = 30 * time.Second
	WriteTimeout      = 2 * time.Minute
	IdleTimeout       = 2 * time.Minute
)

// The settings a deployment may move the general rate with.
//
// Because the right number is a property of the deployment and not of this
// repository: one address may be a phone, or a village office behind one NAT, or
// an integration pushing a day of collections in a batch. A default that catches
// the third is a default somebody removes entirely, and then there is no limit
// anywhere.
const (
	RateEnv  = "GAVYA_RATE_PER_SECOND"
	BurstEnv = "GAVYA_RATE_BURST"
)

// generalRate reads the configured rate, falling back to the constants.
//
// There is deliberately no way to switch the limit off. A value at or below zero
// is not read as "unlimited" — it is read as a mistake, and the default stands.
// The alternative is a control that a typo silently removes, which is the shape
// of failure this platform keeps finding: something that reports success while
// doing nothing.
func generalRate() (float64, int) {
	perSecond, burst := float64(RequestsPerSecond), RequestBurst
	if v, err := strconv.ParseFloat(os.Getenv(RateEnv), 64); err == nil && v > 0 {
		perSecond = v
	}
	if v, err := strconv.Atoi(os.Getenv(BurstEnv)); err == nil && v > 0 {
		burst = v
	}
	return perSecond, burst
}

// limit is the general per-caller limit, built once per server.
func limit() func(http.Handler) http.Handler {
	perSecond, burst := generalRate()
	return ratelimit.Middleware(
		ratelimit.New(perSecond, burst, TrackedClients),
		ratelimit.ExemptProbes(ratelimit.ByClient),
	)
}

// New returns the server a service should run: the mux, guarded and counted,
// over h2c, with timeouts — and with /readyz and /metrics on it.
//
// The checks are what /readyz asks. A service with none answers ready, which is
// a real state; a service that has them and does not pass them here answers
// ready while its database is gone, which is the state this replaced.
func New(addr string, mux *http.ServeMux, checks ...observe.Check) *http.Server {
	metrics := observe.NewMetrics()

	// Registered here rather than asked of every service, for the same reason
	// the guard is: an endpoint every deployment needs and each service has to
	// remember is an endpoint some service does not have.
	// /healthz is here rather than in each service's Register, and that is not
	// tidiness. Every service registered its own, and an http.ServeMux panics on
	// a duplicate pattern — so the modulith, which mounts twenty-eight services
	// on one mux, would have panicked at startup on the second one. It was not
	// caught by the check that exists for exactly this, because that check
	// counted module names instead of registering them.
	//
	// Liveness, and deliberately unconditional: a failing liveness probe gets
	// the pod killed, so tying it to the database would turn a recoverable
	// outage into a crash loop across the platform. Readiness is the one that
	// asks.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/readyz", observe.Ready(checks...))
	mux.HandleFunc("/metrics", metrics.Handler())

	limited := limit()

	// Order matters. Counting is outermost so a refused request is counted —
	// a service rejecting everything must not look like one serving everything.
	// The limit comes before the authorisation check, so an exhausted caller is
	// turned away without the work of deciding what they may do.
	return &http.Server{
		Addr:              addr,
		Handler:           h2c.NewHandler(metrics.Middleware(limited(authz.Guard(mux))), &http2.Server{}),
		ReadHeaderTimeout: ReadHeaderTimeout,
		ReadTimeout:       ReadTimeout,
		WriteTimeout:      WriteTimeout,
		IdleTimeout:       IdleTimeout,
	}
}

// Unguarded returns the same server without the authorisation check.
//
// For the gateway, which is not a service: it authorises against the procedure
// it is about to proxy, before it knows which upstream will serve it, and then
// forwards. Running Guard over its own mux as well would decide the same
// question twice from the same inputs.
//
// Nothing else should use this. The test in this package names the one caller
// that may, so a second one is a failing build rather than a quiet decision.
func Unguarded(addr string, mux http.Handler, metrics *observe.Metrics) *http.Server {
	if metrics == nil {
		metrics = observe.NewMetrics()
	}
	// Limited here too. The modulith runs this one, and the modulith is the shape
	// this platform ships — a bound present only in the deployment nobody runs is
	// not a weaker bound, it is none.
	return &http.Server{
		Addr:              addr,
		Handler:           h2c.NewHandler(metrics.Middleware(limit()(mux)), &http2.Server{}),
		ReadHeaderTimeout: ReadHeaderTimeout,
		ReadTimeout:       ReadTimeout,
		WriteTimeout:      WriteTimeout,
		IdleTimeout:       IdleTimeout,
	}
}
