// Package ratelimit provides HTTP middleware that throttles abusive
// clients on a per-source-IP basis. The auth-failure variant in this
// file specifically protects expensive auth paths (JWT signature verify,
// session lookup against identity.auth_sessions) from a flood of
// malformed credentials.
//
// Strategy: sliding-window counter per IP. Each failed auth attempt
// pushes a timestamp into a small ring buffer. When the buffer holds
// >= threshold entries within the configured window, the next request
// from that IP returns HTTP 429 with a Retry-After header proportional
// to how recent the most-recent failure was. A SUCCESSFUL auth resets
// the counter — legitimate users who briefly fat-finger a token aren't
// punished.
//
// What this protects against:
//   - Brute-force token guessing
//   - Pile of malformed tokens hammering signature-verify CPU
//   - Brute-force session_id probing against identity.auth_sessions
//
// What this DOES NOT protect against:
//   - Distributed attacks from many IPs (use a WAF/CDN for that)
//   - Burst legitimate traffic from one IP (consider per-user quotas
//     at a higher layer)
//
// The middleware is self-contained: no external counter store, no
// dependency on Redis or anything stateful. The trade-off is per-process
// state — a deployment with N replicas behind a load balancer gives
// each replica its own counter, so the effective threshold is N×
// configured. For monolith-dev this is irrelevant; for prod scale-out,
// move to a shared store (separate package, not this one).

package ratelimit

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// AuthFailureLimiter is the sliding-window per-IP failure counter.
// Construction: NewAuthFailureLimiter(threshold, window).
//
// Wire into the HTTP chain via Limiter.Wrap. The wrapped middleware
// inspects every authenticated response: status 401 / 403 increments
// the counter; 2xx resets it; everything else (5xx, 200 health, etc.)
// neither increments nor resets so non-auth traffic doesn't pollute
// the signal.
type AuthFailureLimiter struct {
	threshold int
	window    time.Duration

	mu     sync.RWMutex
	counts map[string]*ipBucket // keyed by source IP

	// Soft cap on the counts map size. When exceeded, the next prune
	// drops every bucket whose latest failure is older than 5×window.
	maxIPs int
}

// ipBucket holds the failure timestamps for one source IP. We keep
// timestamps not just a count so we can age-out entries past the
// window without scheduling a separate cleanup pass.
type ipBucket struct {
	mu         sync.Mutex
	failures   []time.Time // sorted ascending, trimmed on each access
	lastAccess time.Time
}

// NewAuthFailureLimiter constructs a limiter. threshold = how many
// failures within window before throttling kicks in. Both must be > 0.
//
// Default config (DEPLOY_AUTH_RATELIMIT_THRESHOLD=10, _WINDOW=60s)
// means: 11th bad token from one IP within 60s gets 429.
func NewAuthFailureLimiter(threshold int, window time.Duration) *AuthFailureLimiter {
	if threshold <= 0 {
		threshold = 10
	}
	if window <= 0 {
		window = 60 * time.Second
	}
	return &AuthFailureLimiter{
		threshold: threshold,
		window:    window,
		counts:    make(map[string]*ipBucket),
		maxIPs:    10000,
	}
}

// Wrap returns an http.Handler that enforces the limit BEFORE next runs.
// If the source IP is over the limit, responds 429 immediately and
// next is not called. Otherwise next runs, and the response status
// drives the failure counter (401/403 → increment; 2xx → reset).
//
// Order matters in the middleware chain: this MUST run before the
// auth middleware so that signature verification doesn't burn CPU for
// already-throttled IPs. Place between corsMiddleware and the auth
// middleware in app/cmd/main.go.
// skipPaths are URL paths the rate limiter never throttles. They are
// either public meta-endpoints (health/ready/JWKS) or unauth'd
// endpoints whose throttling would be a UX foot-gun (JWKS fetches by
// legitimate clients should never be blocked just because some other
// client on the same IP burned the auth-failure budget).
var skipPaths = map[string]struct{}{
	"/":                      {},
	"/health":                {},
	"/ready":                 {},
	"/.well-known/jwks.json": {},
}

func (l *AuthFailureLimiter) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, skip := skipPaths[r.URL.Path]; skip {
			next.ServeHTTP(w, r)
			return
		}
		ip := clientIP(r)

		// Pre-check: is this IP already throttled?
		if retryAfter, throttled := l.shouldThrottle(ip); throttled {
			seconds := int(retryAfter.Seconds())
			if seconds < 1 {
				seconds = 1
			}
			w.Header().Set("Retry-After", fmt.Sprintf("%d", seconds))
			http.Error(w,
				fmt.Sprintf("too many failed auth attempts from %s; retry after %ds", ip, seconds),
				http.StatusTooManyRequests,
			)
			return
		}

		// Wrap ResponseWriter so we can observe the status code.
		sw := &statusCapture{ResponseWriter: w}
		next.ServeHTTP(sw, r)

		// Drive the counter from the response.
		switch {
		case sw.status == http.StatusUnauthorized || sw.status == http.StatusForbidden:
			l.recordFailure(ip)
		case sw.status >= 200 && sw.status < 300:
			l.reset(ip)
		}
	})
}

// shouldThrottle reports whether the IP is over the threshold within
// the window. retryAfter is the suggested wait until the oldest
// in-window failure ages out (so the IP drops back under threshold).
func (l *AuthFailureLimiter) shouldThrottle(ip string) (time.Duration, bool) {
	l.mu.RLock()
	bucket, ok := l.counts[ip]
	l.mu.RUnlock()
	if !ok {
		return 0, false
	}

	bucket.mu.Lock()
	defer bucket.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-l.window)
	bucket.failures = trim(bucket.failures, cutoff)

	if len(bucket.failures) < l.threshold {
		return 0, false
	}
	// Suggest retry-after = how long until the oldest in-window failure
	// expires (1 slot opens up).
	retry := bucket.failures[0].Sub(cutoff)
	if retry < time.Second {
		retry = time.Second
	}
	return retry, true
}

// recordFailure pushes a timestamp into the IP's bucket and prunes
// stale entries.
func (l *AuthFailureLimiter) recordFailure(ip string) {
	now := time.Now()

	l.mu.RLock()
	bucket, ok := l.counts[ip]
	l.mu.RUnlock()

	if !ok {
		l.mu.Lock()
		// Re-check under write lock (double-checked locking).
		bucket, ok = l.counts[ip]
		if !ok {
			bucket = &ipBucket{lastAccess: now}
			l.counts[ip] = bucket
		}
		// Cheap eviction: if map oversized, prune buckets idle past
		// 5×window. Bounded so eviction is amortized O(map size) only
		// when overflow happens.
		if len(l.counts) > l.maxIPs {
			cutoff := now.Add(-5 * l.window)
			for k, v := range l.counts {
				if v.lastAccess.Before(cutoff) {
					delete(l.counts, k)
				}
			}
		}
		l.mu.Unlock()
	}

	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	bucket.lastAccess = now
	cutoff := now.Add(-l.window)
	bucket.failures = trim(bucket.failures, cutoff)
	bucket.failures = append(bucket.failures, now)
}

// reset clears the IP's failure history. Called on successful auth so
// a legitimate user who briefly mis-typed isn't punished.
func (l *AuthFailureLimiter) reset(ip string) {
	l.mu.RLock()
	bucket, ok := l.counts[ip]
	l.mu.RUnlock()
	if !ok {
		return
	}
	bucket.mu.Lock()
	bucket.failures = nil
	bucket.lastAccess = time.Now()
	bucket.mu.Unlock()
}

// trim returns the suffix of ts whose first element is >= cutoff. ts
// MUST be sorted ascending.
func trim(ts []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(ts) && ts[i].Before(cutoff) {
		i++
	}
	return ts[i:]
}

// statusCapture wraps http.ResponseWriter and remembers the status
// code so the post-handler hook can inspect it.
type statusCapture struct {
	http.ResponseWriter
	status int
}

func (s *statusCapture) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusCapture) Write(b []byte) (int, error) {
	if s.status == 0 {
		// http.ResponseWriter implicitly writes 200 on first Write
		// without a prior WriteHeader. Mirror that.
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// clientIP extracts the source IP for rate-limit accounting.
// Order of preference:
//  1. X-Forwarded-For first hop (when behind a trusted proxy/CDN).
//  2. X-Real-IP (nginx convention).
//  3. RemoteAddr (direct connection — strip the port).
//
// When the gateway is untrusted, X-Forwarded-For can be spoofed by the
// client. Production deployments should set a trusted-proxy list at
// the gateway and reject inbound XFF from anyone else; this middleware
// trusts the headers because the monolith dev profile assumes a
// gateway that already validated them. For internet-direct exposure,
// this trust assumption needs revisiting.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// First hop is the original client. Strip whitespace + take
		// the first comma-separated entry.
		if i := strings.Index(xff, ","); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	if real := r.Header.Get("X-Real-IP"); real != "" {
		return strings.TrimSpace(real)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
