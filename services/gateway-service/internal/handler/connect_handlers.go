package handler

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/ppusapati/gavya/services/gateway-service/internal/config"
	"p9e.in/samavaya/packages/p9log"
)

type Handler struct {
	cfg *config.Config
	log *p9log.Helper
}

func New(cfg *config.Config, log *p9log.Helper) *Handler {
	return &Handler{cfg: cfg, log: log}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.healthz)

	h.registerProxy(mux, "/farm.v1.", h.cfg.FarmServiceURL)
	h.registerProxy(mux, "/breeding.v1.", h.cfg.BreedingServiceURL)
	h.registerProxy(mux, "/health.v1.", h.cfg.HealthServiceURL)
	h.registerProxy(mux, "/feed.v1.", h.cfg.FeedServiceURL)
	h.registerProxy(mux, "/productcatalog.v1.", h.cfg.ProductCatalogServiceURL)
	h.registerProxy(mux, "/inventory.v1.", h.cfg.InventoryServiceURL)
	h.registerProxy(mux, "/order.v1.", h.cfg.OrderServiceURL)
	h.registerProxy(mux, "/billing.v1.", h.cfg.BillingServiceURL)
	h.registerProxy(mux, "/tenant.v1.", h.cfg.TenantServiceURL)
	h.registerProxy(mux, "/notification.v1.", h.cfg.NotificationServiceURL)
	h.registerProxy(mux, "/reporting.v1.", h.cfg.ReportingServiceURL)
	h.registerProxy(mux, "/audit.v1.", h.cfg.AuditServiceURL)
	h.registerProxy(mux, "/file.v1.", h.cfg.FileServiceURL)
}

func (h *Handler) registerProxy(mux *http.ServeMux, prefix, targetURL string) {
	target, err := url.Parse(targetURL)
	if err != nil {
		h.log.Errorf("invalid proxy target %s: %v", targetURL, err)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}
		tenantID := r.Header.Get("X-Tenant-ID")
		if tenantID != "" {
			r.Header.Set("X-Tenant-ID", tenantID)
		}
		proxy.ServeHTTP(w, r)
	})
}

func (h *Handler) newProxy(mux *http.ServeMux, prefix, targetURL string) {
	target, err := url.Parse(targetURL)
	if err != nil {
		h.log.Errorf("invalid proxy target %s: %v", targetURL, err)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	mux.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.Header.Get("X-Tenant-ID")
		if tenantID != "" {
			r.Header.Set("X-Tenant-ID", tenantID)
		}
		proxy.ServeHTTP(w, r)
	})
}

func (h *Handler) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func (h *Handler) ProxyHandler(targetURL string) http.HandlerFunc {
	target, err := url.Parse(targetURL)
	if err != nil {
		return func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "invalid upstream", http.StatusBadGateway)
		}
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := r.Header.Get("X-Tenant-ID")
		if tenantID != "" {
			r.Header.Set("X-Tenant-ID", tenantID)
		}
		proxy.ServeHTTP(w, r)
	}
}
