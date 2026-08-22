package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"p9e.in/samavaya/packages/p9log"

	"github.com/ppusapati/gavya/services/gateway-service/internal/config"
)

const workspace = "https://workspace.example"

func wrapped(t *testing.T, origins ...string) http.Handler {
	t.Helper()
	cfg := config.Load()
	cfg.CORSAllowedOrigins = origins
	h := New(cfg, p9log.NewHelper(p9log.DefaultLogger))

	reached := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Reached-Upstream", "yes")
		w.WriteHeader(http.StatusOK)
	})
	return h.CORS(reached)
}

func preflight(origin string) *http.Request {
	r := httptest.NewRequest(http.MethodOptions, "/tenant.v1.TenantService/GetTenant", nil)
	r.Header.Set("Origin", origin)
	r.Header.Set("Access-Control-Request-Method", "POST")
	r.Header.Set("Access-Control-Request-Headers", "content-type, x-tenant-id")
	return r
}

func TestPreflightFromAnAllowedOriginIsAnswered(t *testing.T) {
	rec := httptest.NewRecorder()
	wrapped(t, workspace).ServeHTTP(rec, preflight(workspace))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != workspace {
		t.Errorf("allow-origin = %q, want %q", got, workspace)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "POST") {
		t.Errorf("allow-methods = %q, want it to permit POST", got)
	}
	// Without these two the browser drops the call before it is ever sent.
	for _, want := range []string{"Content-Type", "X-Tenant-ID"} {
		if got := rec.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, want) {
			t.Errorf("allow-headers = %q, want it to include %s", got, want)
		}
	}
	if rec.Header().Get("X-Reached-Upstream") != "" {
		t.Error("a preflight was proxied to an upstream instead of being answered here")
	}
}

func TestPreflightFromAnUnknownOriginIsRefusedAndNotProxied(t *testing.T) {
	rec := httptest.NewRecorder()
	wrapped(t, workspace).ServeHTTP(rec, preflight("https://elsewhere.example"))

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("allow-origin = %q, want it absent for an unapproved origin", got)
	}
	if rec.Header().Get("X-Reached-Upstream") != "" {
		t.Error("an unapproved preflight reached an upstream")
	}
}

// The origin is matched exactly. A prefix match would let evil-workspace.example
// through when workspace.example is configured.
func TestOriginIsMatchedExactly(t *testing.T) {
	for _, origin := range []string{
		workspace + ".attacker.example",
		"https://evil-workspace.example",
		"http://workspace.example", // a different scheme is a different origin
		workspace + ":8443",        // so is a different port
	} {
		rec := httptest.NewRecorder()
		wrapped(t, workspace).ServeHTTP(rec, preflight(origin))
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("%s was allowed (allow-origin = %q)", origin, got)
		}
	}
}

func TestActualCallFromAnAllowedOriginIsProxiedAndLabelled(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/tenant.v1.TenantService/GetTenant", nil)
	r.Header.Set("Origin", workspace)

	rec := httptest.NewRecorder()
	wrapped(t, workspace).ServeHTTP(rec, r)

	if rec.Header().Get("X-Reached-Upstream") != "yes" {
		t.Error("an allowed cross-origin call did not reach the upstream")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != workspace {
		t.Errorf("allow-origin = %q, want %q", got, workspace)
	}
	if got := rec.Header().Get("Vary"); !strings.Contains(got, "Origin") {
		t.Errorf("Vary = %q, want it to include Origin so caches do not cross origins", got)
	}
}

// Phase-1 carries no cookies or credentials, so granting them would widen what a
// browser will attach to these calls for no benefit.
func TestCredentialsAreNeverGranted(t *testing.T) {
	rec := httptest.NewRecorder()
	wrapped(t, workspace).ServeHTTP(rec, preflight(workspace))

	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("allow-credentials = %q, want it absent", got)
	}
}

// With nothing configured the gateway must behave exactly as it did before CORS
// existed, so an existing same-origin deployment is unaffected.
func TestWithNoOriginsConfiguredNothingChanges(t *testing.T) {
	rec := httptest.NewRecorder()
	wrapped(t).ServeHTTP(rec, preflight(workspace))

	if rec.Header().Get("X-Reached-Upstream") != "yes" {
		t.Error("the wrapper intercepted a request although no origin is configured")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("allow-origin = %q, want it absent", got)
	}
}

// A same-origin call carries no Origin header at all and must be untouched.
func TestSameOriginCallsAreUntouched(t *testing.T) {
	rec := httptest.NewRecorder()
	wrapped(t, workspace).ServeHTTP(rec,
		httptest.NewRequest(http.MethodPost, "/tenant.v1.TenantService/GetTenant", nil))

	if rec.Header().Get("X-Reached-Upstream") != "yes" {
		t.Error("a same-origin call was blocked")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("allow-origin = %q, want it absent when no Origin was offered", got)
	}
}
