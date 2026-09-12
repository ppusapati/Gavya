package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ppusapati/gavya/libs/integrity/authz"
	"github.com/ppusapati/gavya/services/gateway-service/internal/config"

	"p9e.in/samavaya/packages/p9log"
)

// The gateway refuses a procedure the session's role may not call.
//
// This is the edge half of a check the services make again for themselves. Two
// layers deciding the same question sounds like waste until you ask what each
// one is for: this one stops a request before it crosses the network to an
// upstream that would only refuse it, and the one inside the service is the one
// that still holds if anything ever reaches it another way.
func TestTheGatewayRefusesAProcedureTheRoleMayNotCall(t *testing.T) {
	u := newUpstream(t)
	// Both services routed at the one upstream, so a refusal below is this
	// gateway declining and not this gateway having nowhere to send it. Routing
	// only milk would make the settlement call a 404 and the assertion vacuous.
	mux := gatewayToBoth(t, u, stubVerifier{
		tenant: "T_A", user: "US_1",
		perms: authz.Roles()["collector"].Permissions,
	})

	// A collector records milk.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, milkPath, strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer SE_1")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("a collector could not record milk: %d %s", rec.Code, rec.Body)
	}

	// And does not approve settlement cycles.
	u.got = nil
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost,
		"/settlement.v1.SettlementService/ApproveCycle", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer SE_1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("a collector approving a settlement cycle got %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "settlement.approve") {
		t.Errorf("the refusal is %q and does not name the permission somebody would "+
			"have to be given", rec.Body.String())
	}
	if u.got != nil {
		t.Error("the request was refused at the gateway and reached the upstream anyway")
	}
}

// The permissions reach the upstream, and the client's own are discarded.
//
// The services decide for themselves from this header, so a client that could
// set it would be choosing its own permissions — the same hole the tenant header
// would be without strip.
func TestTheGatewayForwardsItsOwnPermissionsAndNotTheClientsClaim(t *testing.T) {
	u := newUpstream(t)
	mux := gatewayToBoth(t, u, stubVerifier{
		tenant: "T_A", user: "US_1",
		perms: authz.NewSet(authz.MilkRead, authz.MilkWrite),
	})

	req := httptest.NewRequest(http.MethodPost, milkPath, strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer SE_1")
	req.Header.Set(authz.PermissionsHeader, "settlement.approve,tenant.admin")
	req.Header.Set(authz.RoleHeader, "admin")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	got := authz.ParseSet(u.got.Get(authz.PermissionsHeader))
	if got.Has("settlement.approve") || got.Has("tenant.admin") {
		t.Errorf("the client's own permission claim reached the upstream: %q",
			u.got.Get(authz.PermissionsHeader))
	}
	if !got.Has(authz.MilkWrite) || len(got) != 2 {
		t.Errorf("the upstream was given %q, want exactly the session's two",
			u.got.Get(authz.PermissionsHeader))
	}
	if role := u.got.Get(authz.RoleHeader); role == "admin" {
		t.Error("the client's own role claim reached the upstream")
	}
}

// gatewayToBoth routes milk and settlement at one upstream, so a test can put a
// procedure the role holds and one it does not through the same gateway.
func gatewayToBoth(t *testing.T, u *upstream, v Verifier) *http.ServeMux {
	t.Helper()
	cfg := config.Load()
	cfg.MilkServiceURL = u.srv.URL
	cfg.SettlementServiceURL = u.srv.URL
	h := New(cfg, p9log.NewHelper(p9log.DefaultLogger)).WithVerifier(v)
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}
