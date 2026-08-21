package handler

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"

	"p9e.in/samavaya/packages/p9log"

	"github.com/ppusapati/gavya/services/gateway-service/internal/config"
)

// route maps a Connect path prefix onto an upstream service.
//
// Connect addresses a procedure as /<fully.qualified.Service>/<Method>, so the
// package prefix is what identifies the owning service.
type route struct {
	prefix string
	proxy  *httputil.ReverseProxy
}

type Handler struct {
	cfg    *config.Config
	log    *p9log.Helper
	routes []route
}

func New(cfg *config.Config, log *p9log.Helper) *Handler {
	return &Handler{cfg: cfg, log: log}
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
		{"/shadowsettlement.v1.", h.cfg.ShadowSettlementServiceURL},
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
		h.routes = append(h.routes, route{prefix: u.prefix, proxy: httputil.NewSingleHostReverseProxy(target)})
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
	for _, rt := range h.routes {
		if strings.HasPrefix(r.URL.Path, rt.prefix) {
			// Headers, including X-Tenant-ID, are forwarded by the reverse proxy
			// as received.
			rt.proxy.ServeHTTP(w, r)
			return
		}
	}
	http.Error(w, "no service is registered for "+r.URL.Path, http.StatusNotFound)
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}
