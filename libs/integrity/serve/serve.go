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
//
// A service now names its address and its mux and gets a server with all three.
// The alternative — a note in a README asking twenty-nine main functions to
// remember — is the arrangement that produced the gap.
package serve

import (
	"net/http"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/ppusapati/gavya/libs/integrity/authz"
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

// New returns the server a service should run: the mux, guarded, over h2c, with
// timeouts.
func New(addr string, mux http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h2c.NewHandler(authz.Guard(mux), &http2.Server{}),
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
func Unguarded(addr string, mux http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h2c.NewHandler(mux, &http2.Server{}),
		ReadHeaderTimeout: ReadHeaderTimeout,
		ReadTimeout:       ReadTimeout,
		WriteTimeout:      WriteTimeout,
		IdleTimeout:       IdleTimeout,
	}
}
