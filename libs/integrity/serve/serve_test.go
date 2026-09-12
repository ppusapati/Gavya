package serve

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Every service runs the server this package builds.
//
// A service that assembles its own http.Server is a service with no
// authorisation check and no timeouts, and neither absence shows up in any test
// of that service's own behaviour — it answers exactly as it did before, to
// anybody who asks. The only place that is visible is here, comparing what the
// services do against what they are all supposed to do.
//
// This is the same check, one layer down, as the gateway's test that every
// upstream it needs is in docker-compose: two lists of the same thing, compared,
// rather than a convention everybody is asked to remember.
//
// RUN WITH -count=1. It reads Go files in other modules, which the test cache
// does not track.
func TestEveryServiceRunsTheGuardedServer(t *testing.T) {
	root := repoRoot(t)
	mains, err := filepath.Glob(filepath.Join(root, "services", "*-service", "cmd", "server", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(mains) < 20 {
		t.Fatalf("found only %d service main functions; the glob has probably stopped "+
			"matching, and a check that finds nothing passes", len(mains))
	}

	// The gateway is the one caller allowed to skip the guard, because it
	// authorises the procedure it is about to proxy before it knows which
	// upstream serves it. Named here so a second exception is a failing build
	// rather than somebody's quiet decision.
	const mayBeUnguarded = "gateway-service"

	var unguarded, handRolled []string
	for _, path := range mains {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		service := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(path))))
		text := string(src)

		switch {
		case strings.Contains(text, "serve.New("):
			// Guarded, with timeouts. What everything should be.
		case strings.Contains(text, "serve.Unguarded("):
			if service != mayBeUnguarded {
				unguarded = append(unguarded, service)
			}
		default:
			handRolled = append(handRolled, service)
		}
	}

	sort.Strings(handRolled)
	sort.Strings(unguarded)
	if len(handRolled) > 0 {
		t.Errorf("%d services build their own http.Server instead of calling serve.New:\n  %s\n"+
			"Those serve every procedure they have with no permission check and no "+
			"timeouts, and answer exactly as they did before to anybody who asks.",
			len(handRolled), strings.Join(handRolled, "\n  "))
	}
	if len(unguarded) > 0 {
		t.Errorf("%d services run the unguarded server, and only %s may:\n  %s",
			len(unguarded), mayBeUnguarded, strings.Join(unguarded, "\n  "))
	}
	t.Logf("checked %d service main functions", len(mains))
}

// The timeouts are actually set on what New returns.
//
// Stated separately from the constants because a constant nothing reads is a
// number in a file, and this package exists precisely because that was the
// situation across twenty-eight main functions.
func TestTheServerCarriesItsTimeouts(t *testing.T) {
	// A real mux, because New registers /healthz, /readyz and /metrics on it —
	// which is the point of it taking one.
	srv := New(":0", http.NewServeMux())
	for _, c := range []struct {
		name string
		got  any
		want any
	}{
		{"ReadHeaderTimeout", srv.ReadHeaderTimeout, ReadHeaderTimeout},
		{"ReadTimeout", srv.ReadTimeout, ReadTimeout},
		{"WriteTimeout", srv.WriteTimeout, WriteTimeout},
		{"IdleTimeout", srv.IdleTimeout, IdleTimeout},
	} {
		if c.got != c.want {
			t.Errorf("%s is %v, want %v", c.name, c.got, c.want)
		}
	}
	if srv.Handler == nil {
		t.Error("the server has no handler")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// .../libs/integrity/serve -> repository root
	return filepath.Dir(filepath.Dir(filepath.Dir(wd)))
}

// The general rate can be moved by a deployment and cannot be switched off.
//
// Moved, because the right number is a property of the deployment: one address
// may be a phone, a village office behind one NAT, or an integration pushing a
// day of collections in a batch. Not switched off, because the way a control
// disappears is never a decision to remove it — it is a zero, an empty string or
// a typo in a compose file, after which everything still works and nothing is
// bounded.
func TestTheGeneralRateIsConfigurableAndCannotBeDisabled(t *testing.T) {
	for _, c := range []struct {
		name        string
		rate, burst string
		wantRate    float64
		wantBurst   int
	}{
		{"unset", "", "", RequestsPerSecond, RequestBurst},
		{"raised", "5000", "20000", 5000, 20000},
		{"lowered", "2", "3", 2, 3},
		// Each of these is somebody trying, or failing, to remove the limit.
		{"zero", "0", "0", RequestsPerSecond, RequestBurst},
		{"negative", "-1", "-1", RequestsPerSecond, RequestBurst},
		{"nonsense", "unlimited", "none", RequestsPerSecond, RequestBurst},
		{"blank", "   ", "", RequestsPerSecond, RequestBurst},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(RateEnv, c.rate)
			t.Setenv(BurstEnv, c.burst)
			rate, burst := generalRate()
			if rate != c.wantRate {
				t.Errorf("%s=%q gives %v requests a second, want %v", RateEnv, c.rate, rate, c.wantRate)
			}
			if burst != c.wantBurst {
				t.Errorf("%s=%q gives a burst of %d, want %d", BurstEnv, c.burst, burst, c.wantBurst)
			}
		})
	}
}

// The server the modulith runs is limited too.
//
// It is built by Unguarded, which for a while had no limit on it at all: the
// bound existed in the twenty-nine-process deployment and was absent from the one
// this platform ships. Exercised rather than read, because a limiter that is
// constructed and not wired in reads exactly the same.
func TestTheUnguardedServerIsStillLimited(t *testing.T) {
	t.Setenv(RateEnv, "1")
	t.Setenv(BurstEnv, "2")

	srv := Unguarded(":0", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), nil)

	var refused bool
	for i := 0; i < 10; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/milk.v1.MilkService/RecordMilk", nil)
		req.RemoteAddr = "198.51.100.4:40000"
		srv.Handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			refused = true
			break
		}
	}
	if !refused {
		t.Error("ten requests a second from one address through the modulith's server " +
			"were all served, with a configured burst of two")
	}

	// And the probes are still reachable, because a readiness probe that gets a
	// 429 takes the pod out of rotation — a rate limit turned into an outage.
	for i := 0; i < 10; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.RemoteAddr = "198.51.100.4:40000"
		srv.Handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("/healthz was refused on request %d", i+1)
		}
	}
}
