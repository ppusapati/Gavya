package config

import "os"

type Config struct {
	ServiceName string
	ServerAddr  string
	DatabaseURL string
}

func Load() *Config {
	return &Config{
		ServiceName: getEnv("SERVICE_NAME", "inventory-service"),
		ServerAddr:  getEnv("SERVER_ADDR", ":8089"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://postgres:secret@localhost:5432/dairy?sslmode=disable"),
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
