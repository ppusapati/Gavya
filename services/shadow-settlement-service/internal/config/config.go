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

	// DivergenceMLURL points at the Rust divergence service. Empty disables the
	// advisory call entirely, which is a supported deployment: the platform
	// still classifies every divergence deterministically without it.
	DivergenceMLURL     string
	DivergenceMLTimeout time.Duration
	// DivergenceModelPin, when set, refuses any model version but this one, so
	// a replayed adjudication cannot silently pick up a retrained model.
	DivergenceModelPin string
}

func Load() *Config {
	return &Config{
		ServiceName:         getEnv("SERVICE_NAME", "shadow-settlement-service"),
		ServerAddr:          getEnv("SERVER_ADDR", ports.Addr(ports.ShadowSettlement)),
		DatabaseURL:         getEnv("DATABASE_URL", "postgres://postgres:secret@localhost:5432/dairy?sslmode=disable"),
		DivergenceMLURL:     getEnv("DIVERGENCE_ML_URL", ""),
		DivergenceMLTimeout: getDuration("DIVERGENCE_ML_TIMEOUT", 3*time.Second),
		DivergenceModelPin:  getEnv("DIVERGENCE_MODEL_VERSION", ""),
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
