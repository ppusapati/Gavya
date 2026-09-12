//go:build e2e

// End-to-end tests over the real services.
//
// Every other test in this repository exercises one service in isolation. These
// build the actual binaries, run them against a real PostgreSQL, and drive them
// over HTTP exactly as another service would — which is the only way to show
// the platform is a pipeline rather than a pile of components.
//
// Run with:
//
//	TEST_DATABASE_DSN="postgres://user@host:port/%s?sslmode=disable" \
//	  go test -tags e2e ./...
//
// The DSN carries a single %s where the per-service database name goes.
package e2e

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ppusapati/gavya/libs/integrity/authz"
	"github.com/ppusapati/gavya/libs/integrity/serve"
	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

// service describes one binary the harness runs.
type service struct {
	name     string
	database string
	schema   string
	// env carries settings this service requires beyond the common ones. A
	// service that refuses to start without a setting belongs here, so the
	// refusal is exercised rather than assumed.
	env []string

	// needs names services that must already be running, because this one calls
	// them. Services start in the order they are declared, so this is a check
	// that the order is right rather than a scheduler — a list that disagrees
	// with the declaration order fails loudly instead of producing a service
	// pointed at an address nothing is listening on.
	needs []string
	// envOfDep maps a dependency's name to the variable this service reads its
	// address from.
	envOfDep map[string]string
}

// sharedSchemas are applied to every service's database, because every service
// needs them present on its own connection.
var sharedSchemas = []string{
	"services/audit-service/internal/db/schema.sql",
	// The hash chain and the append-only enforcement, which deployment applies
	// to the one database every service writes its trail into.
	//
	// The harness applied only schema.sql, so audit_logs here was a plain table:
	// the sealing function did not exist, SealAuditChain failed with "function
	// gavya_seal_audit_log does not exist", and the append-only triggers that
	// stop the application editing its own trail were not present either. The
	// harness was less faithful than production in exactly the place the
	// platform makes its strongest claim.
	"services/audit-service/internal/db/tamper_evidence.sql",
}

var services = []service{
	{name: "canonical-service", database: "e2e_canonical", schema: "services/canonical-service/internal/db/schema.sql"},
	{name: "shadow-settlement-service", database: "e2e_shadow", schema: "services/shadow-settlement-service/internal/db/schema.sql"},
	{name: "ingestion-service", database: "e2e_ingestion", schema: "services/ingestion-service/internal/db/schema.sql"},
	{
		name: "observation-service", database: "e2e_observation",
		schema: "services/observation-service/internal/db/schema.sql",
		// The measurement-control regime has no default and the service will not
		// start without it. Naming it here is what proves the requirement is
		// satisfiable; TestObservationRefusesToStartWithoutARegime proves it is
		// actually required.
		env: []string{"MEASUREMENT_REGIME=IN_LEGAL_METROLOGY"},
	},
	// Pooling is where milk becomes money. Its absence from this harness was
	// why the path from a collection to an amount a producer is paid had never
	// run end to end — every part of it was unit-tested and none of it had been
	// joined up.
	{name: "pooling-service", database: "e2e_pooling", schema: "services/pooling-service/internal/db/schema.sql"},
	// Procurement is the native path: a society prices its own collections from
	// its own chart, rather than the platform recomputing somebody else's.
	{name: "procurement-service", database: "e2e_procurement", schema: "services/procurement-service/internal/db/schema.sql"},
	// Settlement turns a fortnight of priced milk into what each producer is
	// actually handed. It reads the collections from procurement rather than
	// recomputing them, so the URL is not optional — a settlement service
	// pointed at nothing gathers nothing and reports a fortnight in which every
	// producer earned zero.
	{
		name: "settlement-service", database: "e2e_settlement",
		schema:   "services/settlement-service/internal/db/schema.sql",
		needs:    []string{"procurement-service"},
		envOfDep: map[string]string{"procurement-service": "PROCUREMENT_URL"},
	},
	// Material flow is the physical layer balance-service was missing: a node
	// is a cooler with a code and a tanker with a registration rather than a
	// string, and a movement is measured at both ends.
	{name: "material-service", database: "e2e_material", schema: "services/material-service/internal/db/schema.sql"},
	// balance-service is the mathematics material-service supplies the physical
	// model for. It was not in this harness, so the join between the two — the
	// only place either of them means anything — had never run.
	{name: "balance-service", database: "e2e_balance", schema: "services/balance-service/internal/db/schema.sql"},
	// The laboratory. Fat and SNF decide what a producer is paid, so a result
	// that cannot be traced to a sealed sample held by known hands is a number a
	// society cannot defend when a member asks about it.
	{name: "laboratory-service", database: "e2e_laboratory", schema: "services/laboratory-service/internal/db/schema.sql"},
	// Batch genealogy. The recall question — this tanker was bad, which cartons
	// contain it — is the one thing in this platform where a partial answer is
	// worse than none, because the list gets acted on.
	{name: "production-service", database: "e2e_production", schema: "services/production-service/internal/db/schema.sql"},

	// The seven services that predate the integrity work. They had no
	// end-to-end coverage at all: nothing anywhere showed that they start, that
	// they answer, or that one tenant cannot read another's rows through them —
	// which is the platform's central guarantee and the one thing a service
	// cannot be trusted without.
	{name: "product-catalog-service", database: "e2e_catalog", schema: "services/product-catalog-service/internal/db/schema.sql"},
	{name: "inventory-service", database: "e2e_inventory", schema: "services/inventory-service/internal/db/schema.sql"},
	{name: "billing-service", database: "e2e_billing", schema: "services/billing-service/internal/db/schema.sql"},
	{name: "cattle-market-service", database: "e2e_cattlemarket", schema: "services/cattle-market-service/internal/db/schema.sql"},
	{name: "breeding-service", database: "e2e_breeding", schema: "services/breeding-service/internal/db/schema.sql"},
	{name: "feed-service", database: "e2e_feed", schema: "services/feed-service/internal/db/schema.sql"},
	{name: "notification-service", database: "e2e_notification", schema: "services/notification-service/internal/db/schema.sql"},

	// The rest of the tree. Adding the seven above left eleven services still
	// unstarted by anything, which made "every service in the platform answers
	// health" a claim about eighteen of twenty-nine.
	//
	// Two are deliberately absent and stay absent:
	//   - gateway-service routes to every upstream and is covered by its own
	//     unit tests, which check the routing table rather than the network.
	//   - identity-service starts its own binary in identity_service_test.go,
	//     because what that test checks is the refusal to start.
	//
	// audit-service was a third, on the grounds that its schema is applied to
	// every database here already and audit_test.go drives its tamper-evidence
	// against a real database. Both of those are true and neither is about its
	// endpoints: SealAuditChain and VerifyAuditChain were reachable only through
	// SQL, so the seven routes that publish the chain had never been called.
	{name: "audit-service", database: "e2e_audit", schema: "services/audit-service/internal/db/schema.sql"},
	{name: "tenant-service", database: "e2e_tenant", schema: "services/tenant-service/internal/db/schema.sql"},
	{name: "cattle-service", database: "e2e_cattle", schema: "services/cattle-service/internal/db/schema.sql"},
	{name: "milk-service", database: "e2e_milk", schema: "services/milk-service/internal/db/schema.sql"},
	{name: "health-service", database: "e2e_health", schema: "services/health-service/internal/db/schema.sql"},
	{name: "farm-service", database: "e2e_farm", schema: "services/farm-service/internal/db/schema.sql"},
	{name: "order-service", database: "e2e_order", schema: "services/order-service/internal/db/schema.sql"},
	{name: "reporting-service", database: "e2e_reporting", schema: "services/reporting-service/internal/db/schema.sql"},
	{
		name: "file-service", database: "e2e_file",
		schema: "services/file-service/internal/db/schema.sql",
		// It refuses to start without somewhere to put a file, which is the
		// right refusal: a file service that silently accepts uploads and drops
		// them is worse than one that will not start.
		env: []string{"STORAGE_PROVIDER=local", "STORAGE_BUCKET=/tmp/gavya-e2e-files"},
	},
}

// platform is a running set of services, addressed by name.
type platform struct {
	clients map[string]*svcclient.Client
	// baseURLs is the same set as plain addresses, for the tests that reach a
	// service with something other than svcclient: the probes are ordinary GETs
	// and not Connect procedures, and a client that only speaks Connect cannot
	// ask a service whether it is ready.
	baseURLs map[string]string
	tenant   string
}

var (
	runNonce  = time.Now().UnixNano()
	idCounter atomic.Int64
)

// newID produces an identifier of the 26 characters every id column expects.
func newID(prefix string) string {
	body := fmt.Sprintf("%s%d", prefix, runNonce+idCounter.Add(1))
	if len(body) > 26 {
		return body[:26]
	}
	return body + strings.Repeat("0", 26-len(body))
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Dir(wd)
}

// dsn builds the connection string for one service's database.
func dsn(t *testing.T, database string) string {
	t.Helper()
	tpl := os.Getenv("TEST_DATABASE_DSN")
	if tpl == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}
	return fmt.Sprintf(tpl, database)
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// The platform is built and started once for the whole package.
//
// It used to be per test, and with nine services that came to roughly eleven
// seconds of building and launching before each of sixty-five tests — over ten
// minutes of the suite spent starting processes it had just stopped. The suite
// crossed Go's default test timeout the day material-service was added, which
// is the sort of cost that only ever grows.
//
// What is not shared is the tenant. Every test still gets its own, so two tests
// cannot see each other's rows — the isolation the platform enforces is the
// same isolation the suite relies on, which is a reasonable thing for these
// tests to be sitting on top of.
var (
	sharedOnce     sync.Once
	sharedPlatform *platform
	sharedErr      error
	sharedProcs    []*exec.Cmd
	sharedBaseURLs = map[string]string{}
)

// startPlatform returns the running services, scoped to a tenant of this test's
// own.
func startPlatform(t *testing.T) *platform {
	t.Helper()
	// dsn skips when no database is configured, and it has to be called with a
	// live *testing.T for that skip to land on a test rather than in the once.
	_ = dsn(t, "postgres")

	sharedOnce.Do(func() { sharedPlatform, sharedErr = buildAndStart() })
	if sharedErr != nil {
		t.Fatalf("start the platform: %v", sharedErr)
	}
	return &platform{clients: sharedPlatform.clients, baseURLs: sharedBaseURLs, tenant: newID("tnt")}
}

// buildAndStart builds every service and runs it against its own database.
//
// Returns an error rather than taking a *testing.T, because it runs inside a
// sync.Once: a t.Fatalf in there would fail whichever test happened to be first
// and leave the rest reporting a nil platform.
func buildAndStart() (*platform, error) {
	root, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	root = filepath.Dir(root)

	binDir, err := os.MkdirTemp("", "gavya-e2e")
	if err != nil {
		return nil, err
	}
	// Recorded so TestMain can delete it. These directories hold a compiled
	// binary for every service in the platform — about half a gigabyte — and
	// nothing was removing them. Seventeen accumulated in one session's /tmp
	// before a build failed with "no space left on device", which is a failure
	// that reads as a broken service rather than as a full disk.
	sharedBinDirs = append(sharedBinDirs, binDir)

	p := &platform{clients: map[string]*svcclient.Client{}}
	// Where each service ended up, so one that calls another can be told.
	addrs := map[string]string{}

	for _, svc := range services {
		if err := prepareDatabaseErr(root, svc); err != nil {
			return nil, err
		}

		bin := filepath.Join(binDir, svc.name)
		build := exec.Command("go", "build", "-o", bin, "./cmd/server")
		build.Dir = filepath.Join(root, "services", svc.name)
		if out, err := build.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("build %s: %v\n%s", svc.name, err, out)
		}

		port, err := freePortErr()
		if err != nil {
			return nil, err
		}
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		addrs[svc.name] = addr

		cmd := exec.Command(bin)
		cmd.Env = append(os.Environ(),
			"SERVER_ADDR="+addr,
			"DATABASE_URL="+dsnFor(svc.database),
			// The ML tier is not part of this harness. Leaving these empty is a
			// supported deployment, and the assertions below hold without it —
			// which is itself worth proving.
			"DIVERGENCE_ML_URL=",
			"ANOMALY_ML_URL=",
			"UNCERTAINTY_ML_URL=",
		)
		cmd.Env = append(cmd.Env, svc.env...)
		for _, dep := range svc.needs {
			depAddr, up := addrs[dep]
			if !up {
				// A dependency declared after its dependent would leave this
				// service pointed at nothing, and the failure would surface as
				// an empty result rather than as a broken harness.
				return nil, fmt.Errorf("%s needs %s, which has not been started yet; move it "+
					"earlier in the services list", svc.name, dep)
			}
			variable, named := svc.envOfDep[dep]
			if !named {
				return nil, fmt.Errorf("%s needs %s but does not say which variable carries its address",
					svc.name, dep)
			}
			cmd.Env = append(cmd.Env, variable+"=http://"+depAddr)
		}
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		if err := cmd.Start(); err != nil {
			return nil, fmt.Errorf("start %s: %v", svc.name, err)
		}
		sharedProcs = append(sharedProcs, cmd)

		baseURL := "http://" + addr
		sharedBaseURLs[svc.name] = baseURL
		client := svcclient.New(svcclient.Config{
			BaseURL: baseURL,
			Timeout: 10 * time.Second,
		})
		if err := waitReadyErr(client); err != nil {
			return nil, fmt.Errorf("%s did not become ready: %w", svc.name, err)
		}
		p.clients[svc.name] = client
	}
	return p, nil
}

// sharedBinDirs is every temporary directory the harness built binaries into.
var sharedBinDirs []string

// TestMain stops the shared services once every test has finished, and removes
// what they were built from.
//
// The directories are deleted after the processes are killed, not before: a
// running binary whose file has been unlinked keeps running on Linux, but the
// test that notices it has gone is harder to read than one that does not need
// to. An interrupted run still leaves them behind — Go gives a test binary no
// chance to clean up after SIGKILL — so the message on a full disk names them.
func TestMain(m *testing.M) {
	// This suite is not a client any deployment will ever see.
	//
	// Dozens of tests run in parallel against one process, all of them from
	// 127.0.0.1, which is one bucket — so the general per-caller limit fires and
	// tests fail with 429 on work that has nothing to do with rate limiting. The
	// limit is right and the suite is the unusual one, so the suite says so here
	// rather than the default being loosened until this passes.
	//
	// Inherited by every service these tests start, because each of them builds
	// its environment from os.Environ. The sign-in limit is deliberately not
	// raised: it has its own budget, and two tests in onboarding_test.go exist to
	// meet it.
	for _, e := range []struct{ key, value string }{
		{serve.RateEnv, "100000"},
		{serve.BurstEnv, "100000"},
	} {
		if err := os.Setenv(e.key, e.value); err != nil {
			fmt.Fprintf(os.Stderr, "could not set %s: %v\n", e.key, err)
			os.Exit(1)
		}
	}

	code := m.Run()
	stopMLPlatform()
	for _, cmd := range sharedProcs {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	for _, dir := range sharedBinDirs {
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintf(os.Stderr, "could not remove %s: %v\n"+
				"It holds one binary per service. Left behind by enough runs these "+
				"fill the disk, and the next build fails with a message about space "+
				"rather than about them.\n", dir, err)
		}
	}
	os.Exit(code)
}

func dsnFor(database string) string {
	return fmt.Sprintf(os.Getenv("TEST_DATABASE_DSN"), database)
}

func freePortErr() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func waitReadyErr(c *svcclient.Client) error {
	deadline := time.Now().Add(30 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		last = c.Health(ctx)
		cancel()
		if last == nil {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return last
}

// prepared remembers which databases this process has already set up, so the
// work happens once however many tests start a platform.
var (
	prepared   = map[string]bool{}
	prepareMu  sync.Mutex
	schemaOnce = map[string]error{}
)

// prepareDatabase creates a service's database if it is missing and applies its
// schema.
//
// The harness owns this rather than expecting an operator to have run it: a
// suite that silently assumes a hand-migrated database fails with a connection
// error the first time someone new runs it, and passes against a schema that
// may be several changes behind.
func prepareDatabaseErr(root string, svc service) error {
	prepareMu.Lock()
	defer prepareMu.Unlock()
	if prepared[svc.database] {
		return schemaOnce[svc.database]
	}
	prepared[svc.database] = true

	err := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// "postgres" is the maintenance database every server has; a database
		// cannot be created from a connection to itself.
		admin, err := pgx.Connect(ctx, dsnFor("postgres"))
		if err != nil {
			return fmt.Errorf("connect to the maintenance database: %w", err)
		}
		defer admin.Close(ctx)

		var exists bool
		if err := admin.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname=$1)`, svc.database).Scan(&exists); err != nil {
			return fmt.Errorf("look for %s: %w", svc.database, err)
		}
		if !exists {
			// The name comes from this file, not from any input, so interpolating
			// it is the only way to say CREATE DATABASE — which takes no
			// parameters.
			if _, err := admin.Exec(ctx, `CREATE DATABASE "`+svc.database+`"`); err != nil {
				return fmt.Errorf("create %s: %w", svc.database, err)
			}
		}

		conn, err := pgx.Connect(ctx, dsnFor(svc.database))
		if err != nil {
			return fmt.Errorf("connect to %s: %w", svc.database, err)
		}
		defer conn.Close(ctx)

		// The audit trail is written into the caller's transaction, so
		// audit_logs has to be reachable from every service's own connection.
		// That is a real deployment constraint — the platform runs one database
		// for all services — and applying it here is what makes this harness
		// reflect it rather than contradict it.
		for _, path := range append(sharedSchemas, svc.schema) {
			sql, err := os.ReadFile(filepath.Join(root, path))
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}
			// Every schema here is written with IF NOT EXISTS, so applying it to
			// a database left behind by an earlier run is a no-op rather than a
			// failure.
			if _, err := conn.Exec(ctx, string(sql)); err != nil {
				return fmt.Errorf("apply %s to %s: %w", path, svc.database, err)
			}
		}
		return nil
	}()

	schemaOnce[svc.database] = err
	if err != nil {
		return fmt.Errorf("prepare %s: %w", svc.database, err)
	}
	return nil
}

func (p *platform) canonical() *svcclient.Client { return p.clients["canonical-service"] }
func (p *platform) shadow() *svcclient.Client    { return p.clients["shadow-settlement-service"] }
func (p *platform) ingestion() *svcclient.Client { return p.clients["ingestion-service"] }
func (p *platform) observation() *svcclient.Client {
	return p.clients["observation-service"]
}
func (p *platform) pooling() *svcclient.Client { return p.clients["pooling-service"] }
func (p *platform) procurement() *svcclient.Client {
	return p.clients["procurement-service"]
}
func (p *platform) settlement() *svcclient.Client {
	return p.clients["settlement-service"]
}
func (p *platform) material() *svcclient.Client {
	return p.clients["material-service"]
}
func (p *platform) balance() *svcclient.Client {
	return p.clients["balance-service"]
}
func (p *platform) milk() *svcclient.Client {
	return p.clients["milk-service"]
}
func (p *platform) laboratory() *svcclient.Client {
	return p.clients["laboratory-service"]
}
func (p *platform) production() *svcclient.Client {
	return p.clients["production-service"]
}

func (p *platform) catalog() *svcclient.Client   { return p.clients["product-catalog-service"] }
func (p *platform) inventory() *svcclient.Client { return p.clients["inventory-service"] }

// tenantSvcClient is spelled out rather than called tenant(), because platform
// already has a tenant field holding this run's tenant id and a method of the
// same name cannot exist beside it.
func (p *platform) tenantSvcClient() *svcclient.Client { return p.clients["tenant-service"] }
func (p *platform) audit() *svcclient.Client           { return p.clients["audit-service"] }
func (p *platform) file() *svcclient.Client            { return p.clients["file-service"] }
func (p *platform) billing() *svcclient.Client         { return p.clients["billing-service"] }
func (p *platform) cattleMarket() *svcclient.Client    { return p.clients["cattle-market-service"] }
func (p *platform) breeding() *svcclient.Client        { return p.clients["breeding-service"] }
func (p *platform) feed() *svcclient.Client            { return p.clients["feed-service"] }
func (p *platform) notification() *svcclient.Client    { return p.clients["notification-service"] }
func (p *platform) order() *svcclient.Client           { return p.clients["order-service"] }
func (p *platform) health() *svcclient.Client          { return p.clients["health-service"] }
func (p *platform) cattle() *svcclient.Client          { return p.clients["cattle-service"] }
func (p *platform) farm() *svcclient.Client            { return p.clients["farm-service"] }
func (p *platform) reporting() *svcclient.Client       { return p.clients["reporting-service"] }

func (p *platform) opts() svcclient.CallOptions {
	// Tenant and Actor are what the gateway sets from a verified session. These
	// tests call services directly, so the harness sets them — which is what a
	// service-to-service caller does too, and keeps the rule that a service
	// reads who is acting from the transport rather than from the payload.
	return actingAs(p.tenant, "US_E2E_HARNESS_0000000000")
}

// actingAs is one caller, for one tenant, holding an administrator's
// permissions.
//
// The suite acts as an administrator throughout because it is testing what the
// procedures do, not who may call them — and a suite that had to pick the right
// role for each of two hundred and forty-nine routes would be a second, worse
// copy of the permission table. Who may call what is tested where it is decided:
// in libs/integrity/authz, exhaustively, and in authz_e2e_test.go over the wire.
//
// The permissions travel as a header, which is what a service-to-service caller
// does and what the gateway does on a person's behalf. That the services believe
// it is a property of where they sit on the network, not of this call: see the
// note on authz.Guard.
func actingAs(tenant, actor string) svcclient.CallOptions {
	return svcclient.CallOptions{
		TenantID: tenant, RequestID: newID("req"),
		Tenant: tenant, Actor: actor,
		Permissions: authz.Roles()["admin"].Permissions.String(),
	}
}
