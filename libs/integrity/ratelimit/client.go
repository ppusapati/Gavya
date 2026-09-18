package ratelimit

import (
	"net"
	"net/http"
	"strings"
)

// ClientHeader carries the address a request actually arrived from, set by the
// gateway and stripped from anything inbound.
//
// It exists because the alternative does not work. X-Forwarded-For is written by
// whoever is calling, so limiting on it limits nobody: an attacker sends a
// different one each time and every per-address bucket is fresh. The peer
// address of the connection is the one property of a caller that the caller
// cannot choose, and only whatever terminates that connection knows it.
const ClientHeader = "X-Gavya-Client"

// PeerAddress is the address this request is connected from, without the port.
//
// Without the port deliberately: a client gets a new source port for every
// connection, so keying on host:port would give one bucket per request and limit
// nothing at all — the same failure as trusting the header, arrived at by a
// different route.
func PeerAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}

// ByClient limits on the address the gateway asserted, falling back to the peer.
//
// The fallback is for a service reached directly — the e2e harness, and a
// deployment where something other than this gateway is in front. It is the
// weaker of the two and it is still better than no key at all.
func ByClient(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get(ClientHeader)); v != "" {
		return v
	}
	return PeerAddress(r)
}

// ExemptProbes wraps a key function so the platform's own plumbing is never
// limited.
//
// The health endpoints, because a deployment cannot be asked to slow down its
// liveness checks and a readiness probe that gets a 429 takes the pod out of
// rotation — turning a rate limit into an outage.
//
// And VerifySession, for the same reason arrived at the long way round.
//
// # WHAT IT COST TO LEAVE IT IN
//
// The gateway calls VerifySession on every authenticated request. That call is
// the platform talking to itself, and nothing about it belongs to the client who
// caused it — but it arrives at a limiter like any other request, and is keyed on
// whatever address it came from: the gateway's, in the separate-process shape, or
// the loopback in the modulith, where the gateway and identity-service are one
// process.
//
// Either way it is one bucket, shared by every authenticated request in the
// platform. So the general rate limit — meant to bound one client — became the
// platform's total ceiling on authenticated traffic, at fifty requests a second
// against a collection path measured at 2,800.
//
// Measured in e2e/modulith_test.go: against a burst of twenty, one caller got
// nine reads. About two tokens a request, the second being this.
//
// And the way it failed is worse than the ceiling. The verifier reads the 429 as
// the identity service being unreachable and answers 503, "the identity service
// could not be reached" — about a service that is running, and in the modulith is
// the very process printing the message. An operator is sent to look at the one
// thing that is fine.
//
// # WHY EXEMPTING IT IS NOT A HOLE
//
// Signing in is still limited, and more strictly: identity-service has its own
// limiter on that path, counting failures rather than attempts, and it is the
// step that turns a password into a session. This exempts the step that reads a
// session back — a single indexed lookup, cheaper than the readiness probe
// already exempt above, and useless to anybody who does not already hold a
// session id.
func ExemptProbes(key KeyFunc) KeyFunc {
	return func(r *http.Request) string {
		switch r.URL.Path {
		case "/healthz", "/readyz", "/metrics",
			"/gavya.identity.v1.IdentityService/VerifySession":
			return ""
		}
		return key(r)
	}
}
