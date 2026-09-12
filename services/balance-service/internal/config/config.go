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
		ServiceName: getEnv("SERVICE_NAME", "balance-service"),
		ServerAddr:  getEnv("SERVER_ADDR", ports.Addr(ports.Balance)),
		// No default, deliberately.
		//
		// This was postgres://postgres:secret@localhost:5432/dairy?sslmode=disable
		// in twenty-eight services: a password in a compiled binary, and a
		// connection to whatever happened to be on localhost. A service started
		// without DATABASE_URL did not fail — it connected somewhere, and which
		// somewhere depended on the machine.
		//
		// Empty is refused by libs/integrity/tenantdb, which every service opens
		// its pool through, so a missing setting is a refusal to start that names
		// itself rather than a connection to the wrong database.
		DatabaseURL:             os.Getenv("DATABASE_URL"),
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
