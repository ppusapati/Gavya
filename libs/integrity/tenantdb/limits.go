package tenantdb

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// How many connections a service may hold, and how long it may hold one.
//
// Nothing in this platform set either, and the second of those is the more
// interesting omission.
//
// # THE ARITHMETIC THAT DID NOT ADD UP
//
// pgxpool's default pool size is the greater of four and the number of CPUs.
// Twenty-eight services open a pool, every one of them against the same
// PostgreSQL and the same `dairy` database, and no deployment in this repository
// set `max_connections`, which leaves PostgreSQL's own default of 100.
//
// On the four-core machine the load test ran on that is 28 × 4 = 112 against
// 100, and on an eight-core host it is 224. The pools do not hold connections at
// rest — `min_conns` defaults to zero — so this does not show up until every
// service is busy at once, which is the morning collection. What it looks like
// when it happens is `FATAL: sorry, too many clients already` from whichever
// services asked last, and a booth being turned away.
//
// Nothing anywhere said that. The number was arrived at by two independent
// defaults, neither of them chosen, multiplying.
//
// # THE NUMBERS BELOW
//
// MaxConns is eight. The load measurement in e2e/load_test.go sustained about
// 2,800 collections a second at fifty booths at once with pools of four, so four
// was demonstrably enough for the throughput measured and eight is that with
// room. It is stated here once rather than in twenty-eight ConfigMaps, and a DSN
// that sets `pool_max_conns` still wins — a deployment that knows its own
// database is the one entitled to override this.
//
// The total is checked. `TestThePoolsFitTheDatabase` in gateway-service's
// deployment tests multiplies this by the number of services compose gives a
// DATABASE_URL and fails if the result does not leave the headroom below under
// the `max_connections` those files declare. That check is the point: a number
// chosen today is worth little, and a number that cannot silently stop adding up
// is worth a great deal.
const MaxConns int32 = 8

// Headroom is what the arithmetic above leaves for everything that is not a
// service: the migration runner, a backup, and somebody with psql open trying to
// find out why the platform is unhappy — which is exactly when the connections
// are all taken.
const Headroom = 40

// StatementTimeout bounds one statement.
//
// There was no bound at all. serve's WriteTimeout allows a request two minutes,
// deliberately, because a settlement print or a recall trace is slow; but that
// bounds the request, not the query. A statement waiting on a lock held a pooled
// connection for the whole two minutes, and with a pool of four that is most of
// a service's capacity spent waiting on one row.
//
// Sixty seconds: comfortably longer than anything measured — the slowest
// procedure in the load test finished in 84ms — and short enough that a lock
// nobody is going to release costs a connection for a minute rather than two.
const StatementTimeout = 60 * time.Second

// IdleInTransactionTimeout bounds a transaction that has stopped doing anything.
//
// The worse of the two failures, because it has no bound at all otherwise: a
// transaction left open by a handler that returned without committing holds its
// connection, its locks and the rows it has written from every other reader,
// until the process dies. A statement timeout does not touch it — nothing is
// running.
const IdleInTransactionTimeout = 60 * time.Second

// applyLimits sets the sizing and the timeouts, leaving alone anything the DSN
// asked for by name.
//
// The DSN is consulted rather than the parsed configuration because by the time
// pgxpool has parsed it the defaults are already in place, and a MaxConns of
// four coming from the DSN is indistinguishable from a MaxConns of four that
// nobody chose. That is the whole distinction being drawn here, so it has to be
// drawn before it is lost.
func applyLimits(cfg *pgxpool.Config, dsn string) {
	if settingOf(dsn, "pool_max_conns") == "" {
		cfg.MaxConns = MaxConns
	}

	// Runtime parameters travel in the startup packet, so they are set once per
	// connection rather than per query, and a connection returned to the pool
	// keeps them. Milliseconds, as an integer, because that is the one spelling
	// PostgreSQL accepts for these without a unit.
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	setIfAbsent(cfg.ConnConfig.RuntimeParams, "statement_timeout",
		strconv.FormatInt(StatementTimeout.Milliseconds(), 10))
	setIfAbsent(cfg.ConnConfig.RuntimeParams, "idle_in_transaction_session_timeout",
		strconv.FormatInt(IdleInTransactionTimeout.Milliseconds(), 10))
}

func setIfAbsent(params map[string]string, key, value string) {
	if _, already := params[key]; already {
		return
	}
	params[key] = value
}

// settingOf reads one setting out of either DSN shape pgx accepts.
//
// Both, because this repository uses the URL form and a deployment may well hand
// it the keyword form — and a check that understands one of the two formats
// passes everything written in the other.
func settingOf(dsn, key string) string {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "postgres://") || strings.HasPrefix(trimmed, "postgresql://") {
		u, err := url.Parse(trimmed)
		if err != nil {
			return ""
		}
		return strings.ToLower(strings.TrimSpace(u.Query().Get(key)))
	}

	// Keyword form: sslmode=require host=... — last occurrence wins, as libpq
	// reads it.
	found := ""
	for _, field := range strings.Fields(trimmed) {
		k, v, ok := strings.Cut(field, "=")
		if ok && strings.EqualFold(strings.TrimSpace(k), key) {
			found = strings.ToLower(strings.TrimSpace(v))
		}
	}
	return found
}
