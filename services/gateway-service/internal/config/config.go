package config

import "os"

type Config struct {
	ServiceName string
	ServerAddr  string

	FarmServiceURL          string
	BreedingServiceURL      string
	HealthServiceURL        string
	FeedServiceURL          string
	ProductCatalogServiceURL string
	InventoryServiceURL     string
	OrderServiceURL         string
	BillingServiceURL       string
	TenantServiceURL        string
	NotificationServiceURL  string
	ReportingServiceURL     string
	AuditServiceURL         string
	FileServiceURL          string
}

func Load() *Config {
	return &Config{
		ServiceName:              getEnv("SERVICE_NAME", "gateway-service"),
		ServerAddr:               getEnv("SERVER_ADDR", ":8000"),
		FarmServiceURL:           getEnv("FARM_SERVICE_URL", "http://localhost:8081"),
		BreedingServiceURL:       getEnv("BREEDING_SERVICE_URL", "http://localhost:8082"),
		HealthServiceURL:         getEnv("HEALTH_SERVICE_URL", "http://localhost:8083"),
		FeedServiceURL:           getEnv("FEED_SERVICE_URL", "http://localhost:8084"),
		ProductCatalogServiceURL: getEnv("PRODUCT_CATALOG_SERVICE_URL", "http://localhost:8085"),
		InventoryServiceURL:      getEnv("INVENTORY_SERVICE_URL", "http://localhost:8086"),
		OrderServiceURL:          getEnv("ORDER_SERVICE_URL", "http://localhost:8087"),
		BillingServiceURL:        getEnv("BILLING_SERVICE_URL", "http://localhost:8088"),
		TenantServiceURL:         getEnv("TENANT_SERVICE_URL", "http://localhost:8089"),
		NotificationServiceURL:   getEnv("NOTIFICATION_SERVICE_URL", "http://localhost:8090"),
		ReportingServiceURL:      getEnv("REPORTING_SERVICE_URL", "http://localhost:8096"),
		AuditServiceURL:          getEnv("AUDIT_SERVICE_URL", "http://localhost:8097"),
		FileServiceURL:           getEnv("FILE_SERVICE_URL", "http://localhost:8095"),
	}
}

func getEnv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
