package config

import "os"

type Config struct {
	ServiceName, ServerAddr, DatabaseURL string
}

func Load() *Config {
	return &Config{
		ServiceName: getEnv("SERVICE_NAME", "audit-service"),
		ServerAddr:  getEnv("SERVER_ADDR", ":8097"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://postgres:secret@localhost:5432/dairy?sslmode=disable"),
	}
}

func getEnv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
