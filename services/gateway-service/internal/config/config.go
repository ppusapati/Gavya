package config

import (
	"github.com/ppusapati/gavya/libs/integrity/ports"

	"os"
	"strings"
)

type Config struct {
	ServiceName string
	ServerAddr  string

	// Dairy domain.
	CattleServiceURL   string
	MilkServiceURL     string
	BreedingServiceURL string
	HealthServiceURL   string
	FeedServiceURL     string
	FarmServiceURL     string

	// Commerce.
	CattleMarketServiceURL   string
	ProductCatalogServiceURL string
	InventoryServiceURL      string
	OrderServiceURL          string
	BillingServiceURL        string

	// Platform.
	TenantServiceURL       string
	NotificationServiceURL string
	ReportingServiceURL    string
	AuditServiceURL        string
	FileServiceURL         string

	// Integrity layer.
	IngestionServiceURL        string
	CanonicalServiceURL        string
	ObservationServiceURL      string
	PoolingServiceURL          string
	BalanceServiceURL          string
	ShadowSettlementServiceURL string

	// CORSAllowedOrigins lists the browser origins the workspaces are served
	// from. Empty means no cross-origin call is answered, which is the right
	// default for a deployment that serves the workspace from the gateway.
	CORSAllowedOrigins []string
}

// The localhost defaults are for running a service outside compose. Under
// compose every Go ERP service listens on 8080 inside its own container and the
// URLs come from the environment, so these are only a development convenience.
func Load() *Config {
	return &Config{
		ServiceName: getEnv("SERVICE_NAME", "gateway-service"),
		ServerAddr:  getEnv("SERVER_ADDR", ports.Addr(ports.Gateway)),

		CattleServiceURL:   getEnv("CATTLE_SERVICE_URL", ports.LocalURL(ports.Cattle)),
		MilkServiceURL:     getEnv("MILK_SERVICE_URL", ports.LocalURL(ports.Milk)),
		BreedingServiceURL: getEnv("BREEDING_SERVICE_URL", ports.LocalURL(ports.Breeding)),
		HealthServiceURL:   getEnv("HEALTH_SERVICE_URL", ports.LocalURL(ports.Health)),
		FeedServiceURL:     getEnv("FEED_SERVICE_URL", ports.LocalURL(ports.Feed)),
		FarmServiceURL:     getEnv("FARM_SERVICE_URL", ports.LocalURL(ports.Farm)),

		CattleMarketServiceURL:   getEnv("CATTLE_MARKET_SERVICE_URL", ports.LocalURL(ports.CattleMarket)),
		ProductCatalogServiceURL: getEnv("PRODUCT_CATALOG_SERVICE_URL", ports.LocalURL(ports.ProductCatalog)),
		InventoryServiceURL:      getEnv("INVENTORY_SERVICE_URL", ports.LocalURL(ports.Inventory)),
		OrderServiceURL:          getEnv("ORDER_SERVICE_URL", ports.LocalURL(ports.Order)),
		BillingServiceURL:        getEnv("BILLING_SERVICE_URL", ports.LocalURL(ports.Billing)),

		TenantServiceURL:       getEnv("TENANT_SERVICE_URL", ports.LocalURL(ports.Tenant)),
		NotificationServiceURL: getEnv("NOTIFICATION_SERVICE_URL", ports.LocalURL(ports.Notification)),
		ReportingServiceURL:    getEnv("REPORTING_SERVICE_URL", ports.LocalURL(ports.Reporting)),
		AuditServiceURL:        getEnv("AUDIT_SERVICE_URL", ports.LocalURL(ports.Audit)),
		FileServiceURL:         getEnv("FILE_SERVICE_URL", ports.LocalURL(ports.File)),

		IngestionServiceURL:        getEnv("INGESTION_SERVICE_URL", ports.LocalURL(ports.Ingestion)),
		CanonicalServiceURL:        getEnv("CANONICAL_SERVICE_URL", ports.LocalURL(ports.Canonical)),
		ObservationServiceURL:      getEnv("OBSERVATION_SERVICE_URL", ports.LocalURL(ports.Observation)),
		PoolingServiceURL:          getEnv("POOLING_SERVICE_URL", ports.LocalURL(ports.Pooling)),
		BalanceServiceURL:          getEnv("BALANCE_SERVICE_URL", ports.LocalURL(ports.Balance)),
		ShadowSettlementServiceURL: getEnv("SHADOW_SETTLEMENT_SERVICE_URL", ports.LocalURL(ports.ShadowSettlement)),

		CORSAllowedOrigins: splitList(getEnv("CORS_ALLOWED_ORIGINS", "")),
	}
}

// splitList reads a comma-separated environment value, dropping blanks so a
// trailing comma cannot introduce an empty origin that matches nothing.
func splitList(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func getEnv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
