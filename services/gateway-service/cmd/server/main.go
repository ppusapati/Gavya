package main

import (
	"context"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ppusapati/gavya/services/gateway-service/internal/config"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"p9e.in/samavaya/packages/p9log"
)

func main() {
	cfg := config.Load()
	log := p9log.NewHelper(p9log.DefaultLogger)

	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	routes := map[string]string{
		"/farm.v1.":           cfg.FarmServiceURL,
		"/breeding.v1.":       cfg.BreedingServiceURL,
		"/health.v1.":         cfg.HealthServiceURL,
		"/feed.v1.":           cfg.FeedServiceURL,
		"/productcatalog.v1.": cfg.ProductCatalogServiceURL,
		"/inventory.v1.":      cfg.InventoryServiceURL,
		"/order.v1.":          cfg.OrderServiceURL,
		"/billing.v1.":        cfg.BillingServiceURL,
		"/tenant.v1.":         cfg.TenantServiceURL,
		"/notification.v1.":   cfg.NotificationServiceURL,
		"/reporting.v1.":      cfg.ReportingServiceURL,
		"/audit.v1.":          cfg.AuditServiceURL,
		"/file.v1.":           cfg.FileServiceURL,
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		for prefix, targetURL := range routes {
			if strings.HasPrefix(r.URL.Path, prefix) {
				target, err := url.Parse(targetURL)
				if err != nil {
					http.Error(w, "bad gateway", http.StatusBadGateway)
					return
				}
				proxy := httputil.NewSingleHostReverseProxy(target)
				tenantID := r.Header.Get("X-Tenant-ID")
				if tenantID != "" {
					r.Header.Set("X-Tenant-ID", tenantID)
				}
				proxy.ServeHTTP(w, r)
				return
			}
		}
		http.NotFound(w, r)
	})

	srv := &http.Server{
		Addr:    cfg.ServerAddr,
		Handler: h2c.NewHandler(mux, &http2.Server{}),
	}

	go func() {
		log.Infof("gateway-service listening on %s", cfg.ServerAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
	log.Info("gateway-service stopped")
}
