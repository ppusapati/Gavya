package config

import (
	"os"

	"github.com/ppusapati/gavya/libs/integrity/ports"
)

type Config struct {
	ServiceName string
	ServerAddr  string
	DatabaseURL string

	// ProcurementURL is where the priced collections come from.
	//
	// No default that guesses. A settlement service pointed at nothing would
	// start, accept a cycle, and gather nothing — reporting a fortnight in which
	// every producer earned zero, which looks like a failed collection round
	// rather than a misconfiguration. It is better for the gather to refuse and
	// say why.
	ProcurementURL string

	// ServiceIdentity is who this service is when it calls another one. A
	// service borrowing a person's name produces an audit trail that attributes
	// its actions to somebody who was not there.
	ServiceIdentity string
}

func Load() *Config {
	return &Config{
		ServiceName:     getEnv("SERVICE_NAME", "settlement-service"),
		ServerAddr:      getEnv("SERVER_ADDR", ports.Addr(ports.Settlement)),
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://gavya_app@localhost:5432/dairy?sslmode=disable"),
		ProcurementURL:  os.Getenv("PROCUREMENT_URL"),
		ServiceIdentity: getEnv("SERVICE_IDENTITY", "SVC_SETTLEMENT"),
	}
}

func getEnv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
