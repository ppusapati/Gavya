package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"p9e.in/samavaya/packages/p9log"

	"github.com/ppusapati/gavya/libs/integrity/ports"

	"github.com/ppusapati/gavya/services/gateway-service/internal/config"
)

func newHandler(t *testing.T) (*Handler, *http.ServeMux) {
	t.Helper()
	h := New(config.Load(), p9log.NewHelper(p9log.DefaultLogger))
	mux := http.NewServeMux()
	h.Register(mux)
	return h, mux
}

// Register once registered "/" for every upstream, and ServeMux panics on a
// duplicate pattern — so the gateway could not start at all.
func TestRegisterDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Register panicked, so the gateway cannot start: %v", r)
		}
	}()
	newHandler(t)
}

// Connect addresses a procedure as /<package>.<Service>/<Method>. A pattern that
// does not end in "/" matches only that exact path, so routing must be done by
// prefix rather than by registering the prefix as a ServeMux pattern.
func TestEveryUpstreamRoutesARealProcedurePath(t *testing.T) {
	h, _ := newHandler(t)

	procedures := []string{
		"/cattle.v1.CattleService/GetCattle",
		"/milk.v1.MilkService/RecordMilk",
		"/breeding.v1.BreedingService/GetCycle",
		"/health.v1.HealthService/GetTreatment",
		"/feed.v1.FeedService/GetPlan",
		"/farm.v1.FarmService/GetFarm",
		"/cattlemarket.v1.CattleMarketService/GetListing",
		"/productcatalog.v1.ProductCatalogService/GetProduct",
		"/inventory.v1.InventoryService/GetStock",
		"/order.v1.OrderService/GetOrder",
		"/billing.v1.BillingService/GetInvoice",
		"/tenant.v1.TenantService/GetTenant",
		"/notification.v1.NotificationService/Send",
		"/reporting.v1.ReportingService/RequestReport",
		"/audit.v1.AuditService/ListAuditLogs",
		"/file.v1.FileService/GetFile",
		"/ingestion.v1.IngestionService/DeliverRecord",
		"/canonical.v1.CanonicalService/ResolveIdentity",
		"/observation.v1.ObservationService/RecordObservation",
		"/pooling.v1.PoolingService/ValuePool",
		"/balance.v1.BalanceService/Reconcile",
		"/shadowsettlement.v1.ShadowSettlementService/Adjudicate",
	}

	for _, path := range procedures {
		if !routeExists(h, path) {
			t.Errorf("no upstream is registered for %s", path)
		}
	}
}

// routeExists reports whether dispatch would find an upstream, without making a
// network call to one.
func routeExists(h *Handler, path string) bool {
	for _, rt := range h.routes {
		if len(path) >= len(rt.prefix) && path[:len(rt.prefix)] == rt.prefix {
			return true
		}
	}
	return false
}

// An unauthenticated caller is refused before the routing table is consulted,
// so a path that exists and a path that does not are the same reply.
//
// This test used to expect 404 and the change is deliberate: answering 404
// before authenticating lets anybody with a network route enumerate which
// services this platform runs, one guess at a time, without an account.
func TestAnUnauthenticatedCallerCannotTellWhichPathsExist(t *testing.T) {
	_, mux := newHandler(t)

	var codes []int
	for _, path := range []string{"/nosuch.v1.Service/Method", "/milk.v1.MilkService/RecordMilk"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		codes = append(codes, rec.Code)
	}
	if codes[0] != http.StatusUnauthorized || codes[1] != http.StatusUnauthorized {
		t.Errorf("statuses %v, want both 401 — a real path and an invented one must look alike",
			codes)
	}
}

// And once past authentication, an unrouted path is a plain 404.
func TestAnUnknownPathIsNotFoundOnceAuthenticated(t *testing.T) {
	h, mux := newHandler(t)
	h.WithVerifier(stubVerifier{tenant: "T_A", user: "US_1"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/nosuch.v1.Service/Method", nil)
	req.Header.Set("Authorization", "Bearer SE_1")
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHealthzIsServedByTheGatewayItself(t *testing.T) {
	_, mux := newHandler(t)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); body != `{"status":"ok"}` {
		t.Errorf("body = %q", body)
	}
}

// A longer package must win over one that is its prefix, or a new service whose
// name extends an existing one would be silently swallowed.
func TestLongestPrefixWins(t *testing.T) {
	_, _ = newHandler(t)
	h := New(config.Load(), p9log.NewHelper(p9log.DefaultLogger))
	h.Register(http.NewServeMux())

	for i := 1; i < len(h.routes); i++ {
		if len(h.routes[i-1].prefix) < len(h.routes[i].prefix) {
			t.Fatalf("routes are not ordered longest first: %q before %q",
				h.routes[i-1].prefix, h.routes[i].prefix)
		}
	}
}

// One malformed upstream must disable only its own routes, never the gateway.
func TestAMalformedUpstreamDoesNotDisableTheGateway(t *testing.T) {
	cfg := config.Load()
	cfg.CattleServiceURL = "://not a url"

	h := New(cfg, p9log.NewHelper(p9log.DefaultLogger))
	mux := http.NewServeMux()
	h.Register(mux)

	if routeExists(h, "/cattle.v1.CattleService/GetCattle") {
		t.Error("a malformed upstream was registered as a route")
	}
	if !routeExists(h, "/milk.v1.MilkService/RecordMilk") {
		t.Error("a malformed upstream took an unrelated route offline")
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("gateway health = %d, want 200", rec.Code)
	}
}

// The gateway's upstream defaults and the services' own defaults were kept in
// two places and drifted: fourteen of the pairs disagreed, so on a developer's
// machine the gateway could not reach most of the platform. Both sides read one
// table now, and this is what says so — a future edit that hardcodes a number
// back into either side fails here.
func TestUpstreamDefaultsComeFromTheSharedTable(t *testing.T) {
	cfg := config.Load()

	for _, c := range []struct {
		name string
		url  string
		port int
	}{
		{"cattle", cfg.CattleServiceURL, ports.Cattle},
		{"milk", cfg.MilkServiceURL, ports.Milk},
		{"breeding", cfg.BreedingServiceURL, ports.Breeding},
		{"health", cfg.HealthServiceURL, ports.Health},
		{"feed", cfg.FeedServiceURL, ports.Feed},
		{"farm", cfg.FarmServiceURL, ports.Farm},
		{"cattle-market", cfg.CattleMarketServiceURL, ports.CattleMarket},
		{"product-catalog", cfg.ProductCatalogServiceURL, ports.ProductCatalog},
		{"inventory", cfg.InventoryServiceURL, ports.Inventory},
		{"order", cfg.OrderServiceURL, ports.Order},
		{"billing", cfg.BillingServiceURL, ports.Billing},
		{"tenant", cfg.TenantServiceURL, ports.Tenant},
		{"notification", cfg.NotificationServiceURL, ports.Notification},
		{"reporting", cfg.ReportingServiceURL, ports.Reporting},
		{"audit", cfg.AuditServiceURL, ports.Audit},
		{"file", cfg.FileServiceURL, ports.File},
		{"ingestion", cfg.IngestionServiceURL, ports.Ingestion},
		{"canonical", cfg.CanonicalServiceURL, ports.Canonical},
		{"observation", cfg.ObservationServiceURL, ports.Observation},
		{"pooling", cfg.PoolingServiceURL, ports.Pooling},
		{"balance", cfg.BalanceServiceURL, ports.Balance},
		{"shadow-settlement", cfg.ShadowSettlementServiceURL, ports.ShadowSettlement},
	} {
		if want := ports.LocalURL(c.port); c.url != want {
			t.Errorf("the gateway dials %s at %s, but %s listens on %s",
				c.name, c.url, c.name, want)
		}
	}
}

// Every upstream the gateway routes must have somewhere to dial. A service
// added to the routing table without a port is a route to nothing.
func TestEveryRoutedUpstreamHasAPort(t *testing.T) {
	h, _ := newHandler(t)
	for _, rt := range h.routes {
		if rt.proxy == nil {
			t.Errorf("route %s has no upstream", rt.prefix)
		}
	}
	// Counted against the port table rather than a number written here, so
	// adding a service updates both sides at once or neither.
	if len(h.routes) != len(ports.All)-1 {
		t.Errorf("%d upstreams are routed and %d services have ports (the gateway itself is "+
			"not an upstream); a service with a port and no route is unreachable, and a route "+
			"with no port dials nothing", len(h.routes), len(ports.All))
	}
}

// stubVerifier stands in for the identity service.
type stubVerifier struct {
	tenant  string
	user    string
	service string
	err     error
}

func (s stubVerifier) Verify(context.Context, string) (Identity, error) {
	if s.err != nil {
		return Identity{}, s.err
	}
	return Identity{TenantID: s.tenant, UserID: s.user, ServiceIdentityID: s.service}, nil
}
