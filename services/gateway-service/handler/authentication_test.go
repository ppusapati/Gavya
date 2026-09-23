package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"p9e.in/samavaya/packages/p9log"

	"github.com/ppusapati/gavya/libs/integrity/ratelimit"
	"github.com/ppusapati/gavya/services/gateway-service/config"
)

// upstream stands in for a service behind the gateway and records the headers it
// was handed, which is the only way to see what the gateway actually forwarded.
type upstream struct {
	got http.Header
	srv *httptest.Server
}

func newUpstream(t *testing.T) *upstream {
	t.Helper()
	u := &upstream{}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.got = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(u.srv.Close)
	return u
}

// gatewayTo builds a gateway that routes the milk package at one upstream.
func gatewayTo(t *testing.T, u *upstream, v Verifier) *http.ServeMux {
	t.Helper()
	cfg := config.Load()
	cfg.MilkServiceURL = u.srv.URL
	h := New(cfg, p9log.NewHelper(p9log.DefaultLogger)).WithVerifier(v)
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

const milkPath = "/milk.v1.MilkService/RecordMilk"

// The single most important assertion about this gateway.
//
// Downstream services trust the tenant header because it is set here from a
// verified session. If a client could send it themselves, the header would be
// exactly as trustworthy as the request body it replaced, and sessions,
// policies and membership would all be decoration around a value the caller
// chose.
func TestAClientCannotSupplyItsOwnTenantHeader(t *testing.T) {
	u := newUpstream(t)
	mux := gatewayTo(t, u, stubVerifier{tenant: "T_REAL", user: "US_1"})

	req := httptest.NewRequest(http.MethodPost, milkPath, nil)
	req.Header.Set("Authorization", "Bearer SE_1")
	req.Header.Set(tenantHeader, "T_SOMEBODY_ELSE")
	req.Header.Set("X-Gavya-User", "US_SOMEBODY_ELSE")
	req.Header.Set("X-Gavya-Service-Identity", "SI_SOMEBODY_ELSE")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got := u.got.Get(tenantHeader); got != "T_REAL" {
		t.Errorf("the upstream was told tenant %q; the client asked for T_SOMEBODY_ELSE and the "+
			"session says T_REAL", got)
	}
	if got := u.got.Get("X-Gavya-User"); got != "US_1" {
		t.Errorf("the upstream was told user %q", got)
	}
	if got := u.got.Get("X-Gavya-Service-Identity"); got != "" {
		t.Errorf("the upstream was told service identity %q, which the client supplied and the "+
			"session did not", got)
	}
}

// The same, on a path that needs no session. There must be no route through this
// gateway on which a client-supplied value survives.
func TestAClientCannotSupplyATenantOnAnUnauthenticatedPathEither(t *testing.T) {
	u := newUpstream(t)
	cfg := config.Load()
	cfg.IdentityServiceURL = u.srv.URL
	h := New(cfg, p9log.NewHelper(p9log.DefaultLogger)).WithVerifier(stubVerifier{err: errNotSignedIn})
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, "/gavya.identity.v1.IdentityService/SignIn", nil)
	req.Header.Set(tenantHeader, "T_SOMEBODY_ELSE")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("sign-in was refused: %d %s", rec.Code, rec.Body)
	}
	if got := u.got.Get(tenantHeader); got != "" {
		t.Errorf("the identity service was told tenant %q by the client", got)
	}
}

func TestARequestWithNoSessionIsRefused(t *testing.T) {
	u := newUpstream(t)
	mux := gatewayTo(t, u, stubVerifier{tenant: "T_A"})

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, milkPath, nil))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if u.got != nil {
		t.Error("the request reached the upstream without a session")
	}
}

func TestASessionThatDoesNotVerifyIsRefused(t *testing.T) {
	u := newUpstream(t)
	mux := gatewayTo(t, u, stubVerifier{err: errNotSignedIn})

	req := httptest.NewRequest(http.MethodPost, milkPath, nil)
	req.Header.Set("Authorization", "Bearer SE_STALE")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if u.got != nil {
		t.Error("a request with an unusable session reached the upstream")
	}
}

// An identity service that is down is not the caller's fault, and reporting it
// as a failed sign-in would send everybody to reset a password during an outage.
func TestAnIdentityServiceThatIsDownIsNotReportedAsABadSession(t *testing.T) {
	u := newUpstream(t)
	mux := gatewayTo(t, u, stubVerifier{err: errors.New("connection refused")})

	req := httptest.NewRequest(http.MethodPost, milkPath, nil)
	req.Header.Set("Authorization", "Bearer SE_1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code == http.StatusUnauthorized {
		t.Error("an unreachable identity service was reported as a rejected session")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

// No identity service configured must mean nothing gets through, not everything.
// Falling back to unauthenticated would put every service back on taking the
// tenant from the request body, which is the arrangement this replaced.
func TestAGatewayWithNoIdentityServiceRefusesEverything(t *testing.T) {
	u := newUpstream(t)
	cfg := config.Load()
	cfg.MilkServiceURL = u.srv.URL
	cfg.IdentityServiceURL = ""
	h := New(cfg, p9log.NewHelper(p9log.DefaultLogger))
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest(http.MethodPost, milkPath, nil)
	req.Header.Set("Authorization", "Bearer SE_1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if u.got != nil {
		t.Error("a request was forwarded with no way to check who sent it")
	}
}

func TestSignInReachesTheIdentityServiceWithoutASession(t *testing.T) {
	u := newUpstream(t)
	cfg := config.Load()
	cfg.IdentityServiceURL = u.srv.URL
	h := New(cfg, p9log.NewHelper(p9log.DefaultLogger)).WithVerifier(stubVerifier{err: errNotSignedIn})
	mux := http.NewServeMux()
	h.Register(mux)

	for _, path := range []string{
		"/gavya.identity.v1.IdentityService/SignIn",
		"/gavya.identity.v1.IdentityService/SignInService",
	} {
		u.got = nil
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d, want it to reach the identity service", path, rec.Code)
		}
	}
}

// Everything else under the identity package needs a session like anything else.
// Signing somebody out everywhere is an administrative act, not a way in.
func TestTheOtherIdentityProceduresStillNeedASession(t *testing.T) {
	u := newUpstream(t)
	cfg := config.Load()
	cfg.IdentityServiceURL = u.srv.URL
	h := New(cfg, p9log.NewHelper(p9log.DefaultLogger)).WithVerifier(stubVerifier{err: errNotSignedIn})
	mux := http.NewServeMux()
	h.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost,
		"/gavya.identity.v1.IdentityService/SignOutEverywhere", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 — signing everybody out is not an unauthenticated act",
			rec.Code)
	}
}

func TestASessionIsAcceptedFromABearerTokenOrACookie(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(*http.Request)
	}{
		{"bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer SE_1") }},
		{"lowercase bearer", func(r *http.Request) { r.Header.Set("Authorization", "bearer SE_1") }},
		{"cookie", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "gavya_session", Value: "SE_1"}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := newUpstream(t)
			mux := gatewayTo(t, u, stubVerifier{tenant: "T_A", user: "US_1"})

			req := httptest.NewRequest(http.MethodPost, milkPath, nil)
			tc.set(req)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body)
			}
			if u.got.Get(tenantHeader) != "T_A" {
				t.Errorf("tenant = %q", u.got.Get(tenantHeader))
			}
		})
	}
}

// A verified session that names no tenant would be forwarded with an empty
// header, which downstream reads as an unscoped connection and the policies
// refuse — correct, and unreadable. Refused here, where the cause is visible.
func TestASessionThatNamesNoTenantIsRefusedAtTheGateway(t *testing.T) {
	u := newUpstream(t)
	mux := gatewayTo(t, u, stubVerifier{tenant: "", user: "US_1"})

	req := httptest.NewRequest(http.MethodPost, milkPath, nil)
	req.Header.Set("Authorization", "Bearer SE_1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatal("a session with no tenant was forwarded")
	}
	if u.got != nil {
		t.Error("the request reached the upstream unscoped")
	}
}

// A service calling another service gets its identity forwarded, so what it did
// can be attributed to it rather than to whoever owns the account it borrowed.
func TestAServiceIdentityIsForwardedAsItself(t *testing.T) {
	u := newUpstream(t)
	mux := gatewayTo(t, u, stubVerifier{tenant: "T_A", service: "SI_INGESTION"})

	req := httptest.NewRequest(http.MethodPost, milkPath, nil)
	req.Header.Set("Authorization", "Bearer SE_1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got := u.got.Get("X-Gavya-Service-Identity"); got != "SI_INGESTION" {
		t.Errorf("service identity = %q", got)
	}
	if got := u.got.Get("X-Gavya-User"); got != "" {
		t.Errorf("a service was forwarded as user %q", got)
	}
}

// A client cannot choose the address it is rate-limited by.
//
// This is the whole of the rate limit. Every per-address bucket downstream is
// keyed on X-Gavya-Client, and if a caller could send that header themselves,
// every request would arrive under a fresh key and the limit would hold nobody:
// the control would run, report success, and bound nothing. The forwarding
// headers go the same way, because a service reached without this gateway falls
// back to them — so a value that survived here would be a value the caller chose
// wearing a different name.
func TestAClientCannotChooseTheAddressItIsLimitedBy(t *testing.T) {
	u := newUpstream(t)
	mux := gatewayTo(t, u, stubVerifier{tenant: "T_A", user: "US_1"})

	req := httptest.NewRequest(http.MethodPost, milkPath, nil)
	req.Header.Set("Authorization", "Bearer SE_1")
	req.RemoteAddr = "198.51.100.9:41234"
	req.Header.Set(ratelimit.ClientHeader, "203.0.113.77")
	req.Header.Set("X-Forwarded-For", "203.0.113.78")
	req.Header.Set("X-Real-Ip", "203.0.113.79")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}

	// The peer of the connection, without the port: a new source port per
	// connection would give one bucket per request, which is the same nothing
	// arrived at by a different route.
	if got := u.got.Get(ratelimit.ClientHeader); got != "198.51.100.9" {
		t.Errorf("the upstream limits on %q; the client asked for 203.0.113.77 and the "+
			"connection came from 198.51.100.9", got)
	}
	// X-Forwarded-For is not empty at the upstream: the reverse proxy writes it
	// from the peer after the strip, which is the honest value. What must not
	// survive is the client's — so this asserts the claim is gone rather than
	// that the header is, which would fail on correct behaviour.
	for _, h := range []string{"X-Forwarded-For", "X-Real-Ip"} {
		if got := u.got.Get(h); strings.Contains(got, "203.0.113.") {
			t.Errorf("%s reached the upstream as %q, carrying what the client wrote", h, got)
		}
	}
}

func TestTheRefusalDoesNotSayWhyBeyondNotSignedIn(t *testing.T) {
	u := newUpstream(t)
	mux := gatewayTo(t, u, stubVerifier{err: errNotSignedIn})

	req := httptest.NewRequest(http.MethodPost, milkPath, nil)
	req.Header.Set("Authorization", "Bearer SE_1")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, leak := range []string{"expired", "revoked", "no such session"} {
		if strings.Contains(strings.ToLower(body), leak) {
			t.Errorf("the refusal says %q: %s", leak, body)
		}
	}
}

// The download exemption cannot be widened into a way past authentication.
//
// This is the one place a request may reach this platform with no session, so
// it is the one place worth being paranoid about. A prefix test would let
// `/download/../cattle.v1.CattleService/ListCattle` through — it has the
// prefix and it names a procedure — and this middleware runs in front of the
// whole mux in the modulith, where it can see a path the router has not
// cleaned.
func TestOnlyTheDownloadPathsThemselvesSkipAuthentication(t *testing.T) {
	for _, exempt := range signedDownloads {
		if !isUnauthenticated(exempt) {
			t.Errorf("%s is not exempt, so every signed link to it is refused as not "+
				"signed in", exempt)
		}
	}

	for _, path := range []string{
		"/download/",
		"/download",
		"/download/reports",
		"/download/report/extra",
		"/download/../cattle.v1.CattleService/ListCattle",
		"/download/..%2Fcattle.v1.CattleService/ListCattle",
		"/download/report/../../settlement.v1.SettlementService/ApprovePayable",
		"/download/report%00/x",
		"//download/report",
		"/Download/report",
		"/cattle.v1.CattleService/ListCattle",
	} {
		if isUnauthenticated(path) {
			t.Errorf("%q skips authentication; only the exact download paths may", path)
		}
	}
}

// And the exempt paths are the ones the gateway actually routes.
//
// A path exempted and not routed is a 404 that looks like a hole in the
// authentication list; a path routed and not exempted is a signed link refused
// as not signed in, which is the one thing a signed link exists to avoid.
func TestEveryExemptDownloadIsRouted(t *testing.T) {
	src, err := os.ReadFile("connect_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	routes := string(src)
	for _, exempt := range signedDownloads {
		if !strings.Contains(routes, `"`+exempt+`"`) {
			t.Errorf("%s skips authentication and the gateway routes it nowhere", exempt)
		}
	}
}
