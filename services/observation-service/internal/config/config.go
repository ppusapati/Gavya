package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	ServiceName string
	ServerAddr  string
	DatabaseURL string

	// UncertaintyMLURL points at the Rust uncertainty service. Empty disables
	// the estimate entirely, which is a supported deployment: observations are
	// still recorded, marked as carrying no stated confidence.
	UncertaintyMLURL     string
	UncertaintyMLTimeout time.Duration
	// UncertaintyModelPin, when set, refuses any model version but this one, so
	// a recomputed budget cannot silently pick up a refitted model.
	UncertaintyModelPin string

	// AnomalyMLURL points at the Rust anomaly service. Empty disables scoring;
	// nothing downstream depends on it, because a flag never rejects anything.
	AnomalyMLURL     string
	AnomalyMLTimeout time.Duration
	AnomalyModelPin  string
}

func Load() *Config {
	return &Config{
		ServiceName:          getEnv("SERVICE_NAME", "observation-service"),
		ServerAddr:           getEnv("SERVER_ADDR", ":8092"),
		DatabaseURL:          getEnv("DATABASE_URL", "postgres://postgres:secret@localhost:5432/dairy?sslmode=disable"),
		UncertaintyMLURL:     getEnv("UNCERTAINTY_ML_URL", ""),
		UncertaintyMLTimeout: getDuration("UNCERTAINTY_ML_TIMEOUT", 3*time.Second),
		UncertaintyModelPin:  getEnv("UNCERTAINTY_MODEL_VERSION", ""),
		AnomalyMLURL:         getEnv("ANOMALY_ML_URL", ""),
		AnomalyMLTimeout:     getDuration("ANOMALY_ML_TIMEOUT", 3*time.Second),
		AnomalyModelPin:      getEnv("ANOMALY_MODEL_VERSION", ""),
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
