package config

import (
	"os"

	"github.com/ppusapati/gavya/libs/integrity/ports"
)

type Config struct {
	ServiceName, ServerAddr, DatabaseURL string
	StorageProvider, StorageBucket       string
}

func Load() *Config {
	return &Config{
		ServiceName:     getEnv("SERVICE_NAME", "file-service"),
		ServerAddr:      getEnv("SERVER_ADDR", ports.Addr(ports.File)),
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://postgres:secret@localhost:5432/dairy?sslmode=disable"),
		StorageProvider: getEnv("STORAGE_PROVIDER", "local"),
		StorageBucket:   getEnv("STORAGE_BUCKET", "/data/uploads"),
	}
}

func getEnv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
