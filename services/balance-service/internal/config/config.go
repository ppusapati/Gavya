package config

import (
	"github.com/ppusapati/gavya/libs/integrity/ports"

	"os"
	"strconv"
	"time"
)

type Config struct {
	ServiceName string
	ServerAddr  string
	DatabaseURL string

	// ReconciliationMLURL points at the Rust reconciler. Empty disables the call
	// entirely, which is a supported deployment: windows are still validated and
	// their imbalance still computed and recorded, with no adjustments.
	ReconciliationMLURL     string
	ReconciliationMLTimeout time.Duration
	// ReconciliationModelPin, when set, refuses any model version but this one,
	// so a re-examined window cannot pick up a refitted model and reach a
	// different verdict about which leg was at fault.
	ReconciliationModelPin string
}

func Load() *Config {
	return &Config{
		ServiceName:             getEnv("SERVICE_NAME", "balance-service"),
		ServerAddr:              getEnv("SERVER_ADDR", ports.Addr(ports.Balance)),
		DatabaseURL:             getEnv("DATABASE_URL", "postgres://postgres:secret@localhost:5432/dairy?sslmode=disable"),
		ReconciliationMLURL:     getEnv("RECONCILIATION_ML_URL", ""),
		ReconciliationMLTimeout: getDuration("RECONCILIATION_ML_TIMEOUT", 5*time.Second),
		ReconciliationModelPin:  getEnv("RECONCILIATION_MODEL_VERSION", ""),
	}
}

func getEnv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func getDuration(k string, d time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return d
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second
	}
	if parsed, err := time.ParseDuration(v); err == nil {
		return parsed
	}
	return d
}
