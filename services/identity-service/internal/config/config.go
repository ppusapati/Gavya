package config

import (
	"os"

	"github.com/ppusapati/gavya/libs/integrity/ports"
)

type Config struct {
	ServiceName string
	ServerAddr  string
	DatabaseURL string
}

func Load() *Config {
	return &Config{
		ServiceName: getEnv("SERVICE_NAME", "identity-service"),
		ServerAddr:  getEnv("SERVER_ADDR", ports.Addr(ports.Identity)),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://gavya_app@localhost:5432/dairy?sslmode=disable"),
	}
}

func getEnv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
