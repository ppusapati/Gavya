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
		ServiceName: getEnv("SERVICE_NAME", "feed-service"),
		ServerAddr:  getEnv("SERVER_ADDR", ports.Addr(ports.Feed)),
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
		DatabaseURL: os.Getenv("DATABASE_URL"),
	}
}

func getEnv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
