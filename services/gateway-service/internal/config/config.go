package config

import "os"

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
}

// The localhost defaults are for running a service outside compose. Under
// compose every Go ERP service listens on 8080 inside its own container and the
// URLs come from the environment, so these are only a development convenience.
func Load() *Config {
	return &Config{
		ServiceName: getEnv("SERVICE_NAME", "gateway-service"),
		ServerAddr:  getEnv("SERVER_ADDR", ":8000"),

		CattleServiceURL:   getEnv("CATTLE_SERVICE_URL", "http://localhost:8079"),
		MilkServiceURL:     getEnv("MILK_SERVICE_URL", "http://localhost:8078"),
		BreedingServiceURL: getEnv("BREEDING_SERVICE_URL", "http://localhost:8082"),
		HealthServiceURL:   getEnv("HEALTH_SERVICE_URL", "http://localhost:8083"),
		FeedServiceURL:     getEnv("FEED_SERVICE_URL", "http://localhost:8084"),
		FarmServiceURL:     getEnv("FARM_SERVICE_URL", "http://localhost:8081"),

		CattleMarketServiceURL:   getEnv("CATTLE_MARKET_SERVICE_URL", "http://localhost:8077"),
		ProductCatalogServiceURL: getEnv("PRODUCT_CATALOG_SERVICE_URL", "http://localhost:8085"),
		InventoryServiceURL:      getEnv("INVENTORY_SERVICE_URL", "http://localhost:8086"),
		OrderServiceURL:          getEnv("ORDER_SERVICE_URL", "http://localhost:8087"),
		BillingServiceURL:        getEnv("BILLING_SERVICE_URL", "http://localhost:8088"),

		TenantServiceURL:       getEnv("TENANT_SERVICE_URL", "http://localhost:8089"),
		NotificationServiceURL: getEnv("NOTIFICATION_SERVICE_URL", "http://localhost:8098"),
		ReportingServiceURL:    getEnv("REPORTING_SERVICE_URL", "http://localhost:8096"),
		AuditServiceURL:        getEnv("AUDIT_SERVICE_URL", "http://localhost:8097"),
		FileServiceURL:         getEnv("FILE_SERVICE_URL", "http://localhost:8099"),

		IngestionServiceURL:        getEnv("INGESTION_SERVICE_URL", "http://localhost:8091"),
		CanonicalServiceURL:        getEnv("CANONICAL_SERVICE_URL", "http://localhost:8093"),
		ObservationServiceURL:      getEnv("OBSERVATION_SERVICE_URL", "http://localhost:8092"),
		PoolingServiceURL:          getEnv("POOLING_SERVICE_URL", "http://localhost:8094"),
		BalanceServiceURL:          getEnv("BALANCE_SERVICE_URL", "http://localhost:8095"),
		ShadowSettlementServiceURL: getEnv("SHADOW_SETTLEMENT_SERVICE_URL", "http://localhost:8090"),
	}
}

func getEnv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
