package handler

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"

	"p9e.in/samavaya/packages/p9log"

	"github.com/ppusapati/gavya/services/gateway-service/config"
)

// route maps a path onto an upstream service.
//
// Two kinds, and the difference is what `exact` records.
//
// A procedure route is a Connect package prefix. Connect addresses a procedure
// as /<fully.qualified.Service>/<Method>, so the package identifies the owning
// service and everything under it belongs to that service.
//
// A download route is one whole path. It is matched exactly because there is
// nothing underneath it to match — the resource travels in a signed token in
// the query string — and because it is one of the two paths this gateway lets
// through without a session. A prefix there would route, and exempt, anything
// beginning with it.
type route struct {
	prefix string
	exact  bool
	proxy  *httputil.ReverseProxy
}

type Handler struct {
	cfg    *config.Config
	log    *p9log.Helper
	routes []route
	// verifier turns a session into a tenant. Nil disables every authenticated
	// route rather than letting them through unauthenticated.
	verifier Verifier
}

func New(cfg *config.Config, log *p9log.Helper) *Handler {
	h := &Handler{cfg: cfg, log: log}
	if cfg.IdentityServiceURL != "" {
		h.verifier = newIdentityVerifier(cfg.IdentityServiceURL)
	}
	return h
}

// WithVerifier replaces how sessions are checked, so a test can drive the
// gateway without an identity service behind it.
func (h *Handler) WithVerifier(v Verifier) *Handler {
	h.verifier = v
	return h
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.healthz)

	upstreams := []struct {
		prefix string
		url    string
	}{
		// Dairy domain.
		{"/cattle.v1.", h.cfg.CattleServiceURL},
		{"/milk.v1.", h.cfg.MilkServiceURL},
		{"/breeding.v1.", h.cfg.BreedingServiceURL},
		{"/health.v1.", h.cfg.HealthServiceURL},
		{"/feed.v1.", h.cfg.FeedServiceURL},
		{"/farm.v1.", h.cfg.FarmServiceURL},

		// Commerce.
		{"/cattlemarket.v1.", h.cfg.CattleMarketServiceURL},
		{"/productcatalog.v1.", h.cfg.ProductCatalogServiceURL},
		{"/inventory.v1.", h.cfg.InventoryServiceURL},
		{"/order.v1.", h.cfg.OrderServiceURL},
		{"/billing.v1.", h.cfg.BillingServiceURL},

		// Platform.
		{"/gavya.identity.v1.", h.cfg.IdentityServiceURL},
		{"/procurement.v1.", h.cfg.ProcurementServiceURL},
		{"/settlement.v1.", h.cfg.SettlementServiceURL},
		{"/tenant.v1.", h.cfg.TenantServiceURL},
		{"/notification.v1.", h.cfg.NotificationServiceURL},
		{"/reporting.v1.", h.cfg.ReportingServiceURL},
		{"/audit.v1.", h.cfg.AuditServiceURL},
		{"/file.v1.", h.cfg.FileServiceURL},

		// Integrity layer.
		{"/ingestion.v1.", h.cfg.IngestionServiceURL},
		{"/canonical.v1.", h.cfg.CanonicalServiceURL},
		{"/observation.v1.", h.cfg.ObservationServiceURL},
		{"/pooling.v1.", h.cfg.PoolingServiceURL},
		{"/balance.v1.", h.cfg.BalanceServiceURL},
		{"/material.v1.", h.cfg.MaterialServiceURL},
		{"/laboratory.v1.", h.cfg.LaboratoryServiceURL},
		{"/production.v1.", h.cfg.ProductionServiceURL},
		{"/shadowsettlement.v1.", h.cfg.ShadowSettlementServiceURL},
	}

	// Signed download links.
	//
	// Not Connect procedures, and the only paths this gateway routes that are
	// not. A browser following an <a href> sends no Authorization header, so
	// these carry their own authority in a signed token and are exempt from the
	// session check — see signedDownloads in authentication.go for what that
	// exemption is and is not.
	//
	// The path names the service, so a token one service issued comes back to
	// the service that can check it. Listed apart from the packages above
	// because they are a different kind of route and every check about routing
	// has to be able to tell them apart.
	downloads := []struct {
		path string
		url  string
	}{
		{"/download/report", h.cfg.ReportingServiceURL},
		{"/download/file", h.cfg.FileServiceURL},
	}

	for _, u := range upstreams {
		target, err := url.Parse(u.url)
		if err != nil {
			// A malformed upstream disables its own routes rather than the whole
			// gateway: one bad environment variable must not take every service
			// offline.
			h.log.Errorf("route %s disabled, invalid upstream %q: %v", u.prefix, u.url, err)
			continue
		}
		h.routes = append(h.routes, route{
			prefix: u.prefix,
			proxy:  httputil.NewSingleHostReverseProxy(target),
		})
	}

	for _, d := range downloads {
		target, err := url.Parse(d.url)
		if err != nil {
			h.log.Errorf("download %s disabled, invalid upstream %q: %v", d.path, d.url, err)
			continue
		}
		h.routes = append(h.routes, route{
			prefix: d.path, exact: true,
			proxy: httputil.NewSingleHostReverseProxy(target),
		})
	}

	// Longest prefix first, so a more specific package always wins over one that
	// happens to be its prefix.
	sort.Slice(h.routes, func(i, j int) bool {
		return len(h.routes[i].prefix) > len(h.routes[j].prefix)
	})

	// A single catch-all. ServeMux panics on a duplicate pattern, and a pattern
	// that does not end in "/" matches only that exact path — so one dispatcher
	// is both the safe and the correct way to route Connect procedures.
	mux.HandleFunc("/", h.dispatch)

	h.log.Infof("gateway routing %d upstreams", len(h.routes))
}

func (h *Handler) dispatch(w http.ResponseWriter, r *http.Request) {
	// Before routing, not after. A request that is going to be refused for
	// having no session must be refused whether or not an upstream exists for
	// it, and a request that is going to be forwarded must have had the
	// client's own tenant header removed first.
	if !h.authenticate(w, r) {
		return
	}
	for _, rt := range h.routes {
		if !rt.matches(r.URL.Path) {
			continue
		}
		{
			// Authorised here rather than before the loop, so a path this
			// gateway has no upstream for is still a 404. Asked earlier, every
			// unknown path would answer "no permission is declared for it",
			// which is true and useless: it says the platform has a gap where
			// the caller has a typo.
			if !h.authorise(w, r) {
				return
			}
			// The reverse proxy forwards headers as they now stand, which
			// includes the tenant this gateway just established and excludes
			// anything the client sent under that name.
			rt.proxy.ServeHTTP(w, r)
			return
		}
	}
	http.Error(w, "no service is registered for "+r.URL.Path, http.StatusNotFound)
}

// matches says whether this route serves a path.
func (rt route) matches(path string) bool {
	if rt.exact {
		return path == rt.prefix
	}
	return strings.HasPrefix(path, rt.prefix)
}

// procedureRoutes are the Connect packages, one per service.
//
// Separated from the download routes because the two answer different
// questions: how many services are reachable, and which paths skip
// authentication. A check that counted both together would report a service
// twice and a download not at all.
func (h *Handler) procedureRoutes() []route {
	var out []route
	for _, rt := range h.routes {
		if !rt.exact {
			out = append(out, rt)
		}
	}
	return out
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}
