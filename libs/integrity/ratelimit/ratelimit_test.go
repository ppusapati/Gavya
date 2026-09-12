package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A caller is allowed its burst and then refused.
func TestABurstIsAllowedAndThenRefused(t *testing.T) {
	l := New(1, 3, 100)
	for i := 1; i <= 3; i++ {
		if !l.Allow("a") {
			t.Fatalf("refused request %d of a burst of 3", i)
		}
	}
	if l.Allow("a") {
		t.Error("a fourth request was allowed against a burst of 3")
	}
}

// The bucket refills with time, not with wishing.
func TestTheBucketRefillsOverTime(t *testing.T) {
	l := New(2, 2, 100) // two a second
	start := time.Now()
	l.allowAt("a", start)
	l.allowAt("a", start)
	if l.allowAt("a", start) {
		t.Fatal("a third request was allowed immediately against a burst of 2")
	}
	if !l.allowAt("a", start.Add(time.Second)) {
		t.Error("a second later, two tokens should have arrived and none had")
	}
}

// One caller running out does not affect another.
//
// The point of keying at all. A limiter that slowed everybody down when one
// client misbehaved would be the outage it exists to prevent.
func TestOneCallerRunningOutDoesNotAffectAnother(t *testing.T) {
	l := New(0.001, 2, 100)
	l.Allow("noisy")
	l.Allow("noisy")
	if l.Allow("noisy") {
		t.Fatal("the noisy caller was not limited")
	}
	if !l.Allow("quiet") {
		t.Error("a caller that had asked for nothing was refused because another " +
			"had asked for too much")
	}
}

// The map does not grow without bound.
//
// An unbounded map keyed on the client's address is a cheaper attack than the
// one being defended against: fill it until the process dies.
func TestTheMapIsBounded(t *testing.T) {
	const max = 64
	l := New(1000, 1, max)
	for i := 0; i < max*20; i++ {
		l.Allow("client-" + strconv.Itoa(i))
	}
	l.mu.Lock()
	size := len(l.buckets)
	l.mu.Unlock()
	if size > max {
		t.Errorf("the limiter remembers %d keys with a bound of %d", size, max)
	}
}

// Sign-in: failures count and successes do not.
//
// Counting attempts would throttle a co-operative signing thirty people in each
// morning to bound something a co-operative never does.
func TestOnlyFailedSignInsSpendTheBudget(t *testing.T) {
	l := New(0.001, 3, 100)
	var answer int
	handler := OnFailure(l, ByClient, func(s int) bool { return s == http.StatusUnauthorized })(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(answer)
		}))

	call := func() int {
		req := httptest.NewRequest(http.MethodPost, "/x", nil)
		req.Header.Set(ClientHeader, "10.0.0.1")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w.Code
	}

	// Twenty successes cost nothing.
	answer = http.StatusOK
	for i := 0; i < 20; i++ {
		if got := call(); got != http.StatusOK {
			t.Fatalf("a correct sign-in was refused after %d others: %d", i, got)
		}
	}

	// Three failures spend the budget; the fourth is refused.
	answer = http.StatusUnauthorized
	for i := 1; i <= 3; i++ {
		if got := call(); got != http.StatusUnauthorized {
			t.Fatalf("failure %d answered %d, want 401", i, got)
		}
	}
	if got := call(); got != http.StatusTooManyRequests {
		t.Errorf("the fourth failure answered %d, want 429", got)
	}
}

// The refusal says how long to wait.
func TestTheRefusalSaysHowLongToWait(t *testing.T) {
	l := New(0.5, 1, 100)
	handler := Middleware(l, ByClient)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	req := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/x", nil)
		r.Header.Set(ClientHeader, "10.0.0.2")
		return r
	}
	handler.ServeHTTP(httptest.NewRecorder(), req())

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req())
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("the second request answered %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("no Retry-After, so a client retries immediately and makes it worse")
	}
	if !strings.Contains(w.Body.String(), "resource_exhausted") {
		t.Errorf("the body is %q and is not the platform's error shape", w.Body)
	}
}

// The probes are never limited.
//
// A readiness probe that gets a 429 takes the pod out of rotation, turning a
// rate limit into an outage.
func TestTheProbesAreNeverLimited(t *testing.T) {
	l := New(0.001, 1, 100)
	handler := Middleware(l, ExemptProbes(ByClient))(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		for i := 0; i < 50; i++ {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, path, nil)
			r.Header.Set(ClientHeader, "10.0.0.3")
			handler.ServeHTTP(w, r)
			if w.Code == http.StatusTooManyRequests {
				t.Fatalf("%s was rate limited on request %d", path, i)
			}
		}
	}
}

// The key is the address without the port.
//
// A client gets a new source port per connection, so keying on host:port gives
// one bucket per request and limits nothing — the same failure as trusting a
// header, reached by a different route.
func TestTheKeyIgnoresTheSourcePort(t *testing.T) {
	a := httptest.NewRequest(http.MethodPost, "/x", nil)
	a.RemoteAddr = "203.0.113.9:51001"
	b := httptest.NewRequest(http.MethodPost, "/x", nil)
	b.RemoteAddr = "203.0.113.9:51002"

	if ByClient(a) != ByClient(b) {
		t.Errorf("two connections from one address key differently: %q and %q",
			ByClient(a), ByClient(b))
	}
	if strings.Contains(ByClient(a), ":") {
		t.Errorf("the key %q carries a port", ByClient(a))
	}
}

// The gateway's assertion outranks anything the client sent.
func TestTheAssertedAddressBeatsTheClaimedOne(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/x", nil)
	r.RemoteAddr = "203.0.113.9:51001"
	r.Header.Set(ClientHeader, "10.1.1.1")
	r.Header.Set("X-Forwarded-For", "1.2.3.4")

	if got := ByClient(r); got != "10.1.1.1" {
		t.Errorf("the key is %q; the gateway asserted 10.1.1.1 and the client "+
			"claimed 1.2.3.4", got)
	}
}
