// Package ratelimit bounds how fast one caller can ask.
//
// There was nothing. The identity service locks an account after repeated
// failures, which protects one account against many passwords and does nothing
// at all against the attack that is actually used: one password tried against
// many accounts. Every failure lands on a different account, no account reaches
// its threshold, and the platform notices nothing.
//
// # WHAT THIS IS NOT
//
// It is per process. Two copies of a service each keep their own buckets, so a
// deployment of N processes allows N times the configured rate. That is a real
// limitation and it is stated rather than papered over: a shared counter needs
// somewhere shared to keep it, and adding a Redis to this platform to hold one
// integer is a larger decision than it looks. In the modulith — one process —
// the configured rate is the rate.
//
// It is also not a defence against a distributed attacker, who has more
// addresses than this has buckets. What it is: a bound on how fast one source
// can work, which turns an overnight credential-stuffing run into something that
// takes years, and stops one misbehaving client from starving everyone else.
package ratelimit

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Limiter is a token bucket per key.
type Limiter struct {
	rate  float64 // tokens added per second
	burst float64 // bucket size, and so the most that can arrive at once
	max   int     // how many keys to remember

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	seen   time.Time
}

// New builds a limiter.
//
// max bounds the memory. An unbounded map keyed on the client's address is
// itself the denial of service: an attacker with a range of addresses fills it
// until the process dies, which is a cheaper attack than the one being defended
// against.
func New(perSecond float64, burst, max int) *Limiter {
	if max < 1 {
		max = 1
	}
	return &Limiter{
		rate:    perSecond,
		burst:   float64(burst),
		max:     max,
		buckets: make(map[string]*bucket, max),
	}
}

// Allow reports whether this key may proceed, and takes a token if so.
func (l *Limiter) Allow(key string) bool {
	return l.allowAt(key, time.Now())
}

func (l *Limiter) allowAt(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	b, known := l.buckets[key]
	if !known {
		l.makeRoom(now)
		b = &bucket{tokens: l.burst, seen: now}
		l.buckets[key] = b
	} else {
		// Refill for the time that passed, capped at the bucket size.
		b.tokens += now.Sub(b.seen).Seconds() * l.rate
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
		b.seen = now
	}

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Retry is roughly how long this key should wait, for the Retry-After header.
func (l *Limiter) Retry(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, known := l.buckets[key]
	if !known || b.tokens >= 1 || l.rate <= 0 {
		return time.Second
	}
	need := 1 - b.tokens
	return time.Duration(need/l.rate*float64(time.Second)) + time.Second
}

// makeRoom evicts when the map is full. Caller holds the lock.
//
// Full buckets go first, because a bucket at its ceiling carries no information:
// forgetting it and recreating it later gives the same answer. Only if none are
// full does this evict by age, which is the case where every remembered key is
// mid-flight and something has to give.
func (l *Limiter) makeRoom(now time.Time) {
	if len(l.buckets) < l.max {
		return
	}
	for key, b := range l.buckets {
		refilled := b.tokens + now.Sub(b.seen).Seconds()*l.rate
		if refilled >= l.burst {
			delete(l.buckets, key)
		}
	}
	if len(l.buckets) < l.max {
		return
	}
	var oldestKey string
	var oldest time.Time
	for key, b := range l.buckets {
		if oldestKey == "" || b.seen.Before(oldest) {
			oldestKey, oldest = key, b.seen
		}
	}
	delete(l.buckets, oldestKey)
}

// KeyFunc decides what a request is limited by.
//
// Returning "" exempts the request, which is how the probes stay reachable: a
// deployment cannot be asked to slow down its liveness checks.
type KeyFunc func(*http.Request) string

// Middleware refuses a request whose key has run out of tokens.
func Middleware(l *Limiter, key KeyFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			k := key(r)
			if k == "" || l.Allow(k) {
				next.ServeHTTP(w, r)
				return
			}
			TooMany(w, l.Retry(k))
		})
	}
}

// TooMany writes the refusal.
//
// With Retry-After, because a client told only "too many" retries immediately
// and makes it worse. The body is the platform's usual error shape so one
// decoder handles it.
func TooMany(w http.ResponseWriter, retry time.Duration) {
	seconds := int(retry.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte(`{"code":"resource_exhausted","message":"too many requests; retry after ` +
		strconv.Itoa(seconds) + ` seconds"}`))
}

// OnFailure counts only the requests that failed.
//
// For sign-in. Limiting attempts would throttle a busy co-operative signing its
// staff in each morning; limiting failures leaves that untouched and bounds the
// thing that is actually an attack. A caller who knows the password is never
// slowed down.
func OnFailure(l *Limiter, key KeyFunc, failed func(status int) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			k := key(r)
			if k == "" {
				next.ServeHTTP(w, r)
				return
			}
			// Checked before the work, so an exhausted key is refused without
			// another password comparison — the expensive part, and the part an
			// attacker is trying to make this platform do.
			if !l.Allow(k) {
				TooMany(w, l.Retry(k))
				return
			}
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			if !failed(rec.status) {
				// It succeeded, so give the token back: a person signing in
				// correctly should not be spending an attacker's budget.
				l.refund(k)
			}
		})
	}
}

func (l *Limiter) refund(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if b, known := l.buckets[key]; known {
		b.tokens++
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.written {
		s.status, s.written = code, true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.written = true
	return s.ResponseWriter.Write(b)
}
