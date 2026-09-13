package config

import (
	"os"
	"time"

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

	// CanonicalURL is where the history of what an imported member number has
	// meant comes from, for explaining a payment.
	//
	// Optional, and its absence is reported rather than hidden: an explanation
	// produced without it says in so many words that identity history was not
	// consulted. The money trace is still worth having without the identity
	// trace; what would not be worth having is one that looked complete.
	CanonicalURL string

	// NotificationURL is where the inboxes are, for telling somebody a payment
	// was held, approved or paid.
	//
	// Optional, and its absence is loud rather than quiet: the messages are
	// still queued, and every sweep of the queue logs that they are owed and
	// nothing is configured to deliver them.
	NotificationURL string

	// NotifyInterval is how often the outbox is swept. A commit kicks a sweep
	// immediately, so this is the retry cadence rather than the latency.
	NotifyInterval time.Duration

	// ServiceIdentity is who this service is when it calls another one. A
	// service borrowing a person's name produces an audit trail that attributes
	// its actions to somebody who was not there.
	ServiceIdentity string
}

func Load() *Config {
	return &Config{
		ServiceName: getEnv("SERVICE_NAME", "settlement-service"),
		ServerAddr:  getEnv("SERVER_ADDR", ports.Addr(ports.Settlement)),
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
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		ProcurementURL:  os.Getenv("PROCUREMENT_URL"),
		CanonicalURL:    os.Getenv("CANONICAL_URL"),
		NotificationURL: os.Getenv("NOTIFICATION_URL"),
		NotifyInterval:  durationEnv("NOTIFY_INTERVAL", 5*time.Second),
		ServiceIdentity: getEnv("SERVICE_IDENTITY", "SVC_SETTLEMENT"),
	}
}

func getEnv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// durationEnv reads a duration, falling back when unset or unreadable.
//
// Unreadable falls back rather than failing: a sweep interval is not a setting
// a service should refuse to start over, and the default is a reasonable one.
func durationEnv(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return def
	}
	return d
}
