package config

import "os"

type Config struct {
	ServiceName string
	ServerAddr  string
	DatabaseURL string
}

func Load() *Config {
	return &Config{
		ServiceName: getEnv("SERVICE_NAME", "notification-service"),
		ServerAddr:  getEnv("SERVER_ADDR", ":8092"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://postgres:secret@localhost:5432/dairy?sslmode=disable"),
	}
}

func getEnv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
