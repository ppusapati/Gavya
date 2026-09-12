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

// ExemptProbes wraps a key function so the health endpoints are never limited.
//
// A deployment cannot be asked to slow down its liveness checks, and a readiness
// probe that gets a 429 takes the pod out of rotation — turning a rate limit
// into an outage.
func ExemptProbes(key KeyFunc) KeyFunc {
	return func(r *http.Request) string {
		switch r.URL.Path {
		case "/healthz", "/readyz", "/metrics":
			return ""
		}
		return key(r)
	}
}
