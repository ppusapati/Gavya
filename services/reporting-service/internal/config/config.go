package config

import (
	"os"
	"strconv"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/ports"
)

type Config struct {
	ServiceName string
	ServerAddr  string
	DatabaseURL string

	// Where a report's data comes from.
	//
	// Each is optional and none has a default. A deployment that sets none of
	// them still serves every procedure — listing reports, writing a schedule —
	// and every report it runs fails with the missing setting named. That is
	// better than refusing to start: a service that will not come up because it
	// cannot produce one kind of report takes down the seven procedures that
	// have nothing to do with producing reports.
	ProcurementURL      string
	SettlementURL       string
	ShadowSettlementURL string

	// ReportInterval is how often the runner looks for work. A request from the
	// console kicks a sweep, so this is the floor for a report nobody asked for
	// interactively rather than the wait anybody normally sees.
	ReportInterval time.Duration
	// ScheduleInterval is how often schedules are checked. A cron expression's
	// finest resolution is a minute, so sweeping faster cannot fire anything
	// sooner and only costs queries.
	ScheduleInterval time.Duration

	// RunnerEnabled turns both sweeps off.
	//
	// For a deployment that runs several replicas of this service and wants
	// only some of them producing reports. The sweeps are safe to run on every
	// replica — the claim uses SKIP LOCKED and schedules advance under a lock —
	// so this is not needed for correctness, and it exists because "turn it
	// off" is a thing somebody needs at three in the morning.
	RunnerEnabled bool

	// ServiceIdentity is who this service is when it calls another one.
	ServiceIdentity string
}

func Load() *Config {
	return &Config{
		ServiceName: getEnv("SERVICE_NAME", "reporting-service"),
		ServerAddr:  getEnv("SERVER_ADDR", ports.Addr(ports.Reporting)),
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

		ProcurementURL:      os.Getenv("PROCUREMENT_URL"),
		SettlementURL:       os.Getenv("SETTLEMENT_URL"),
		ShadowSettlementURL: os.Getenv("SHADOW_SETTLEMENT_URL"),

		ReportInterval:   durationEnv("REPORT_INTERVAL", 10*time.Second),
		ScheduleInterval: durationEnv("SCHEDULE_INTERVAL", time.Minute),
		RunnerEnabled:    boolEnv("RUNNER_ENABLED", true),

		ServiceIdentity: getEnv("SERVICE_IDENTITY", "SVC_REPORTING"),
	}
}

// durationEnv reads a Go duration such as 30s or 2m.
//
// A value that will not parse keeps the default rather than failing the start,
// and the default is one this service can work with. A typo in an interval is
// not a reason for a service to refuse to serve the procedures that do not use
// it.
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

func boolEnv(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func getEnv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
