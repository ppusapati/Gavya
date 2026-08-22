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

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

// service describes one binary the harness runs.
type service struct {
	name     string
	database string
	schema   string
}

var services = []service{
	{name: "canonical-service", database: "e2e_canonical", schema: "services/canonical-service/internal/db/schema.sql"},
	{name: "shadow-settlement-service", database: "e2e_shadow", schema: "services/shadow-settlement-service/internal/db/schema.sql"},
	{name: "ingestion-service", database: "e2e_ingestion", schema: "services/ingestion-service/internal/db/schema.sql"},
}

// platform is a running set of services, addressed by name.
type platform struct {
	clients map[string]*svcclient.Client
	tenant  string
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

// startPlatform builds and runs every service, each against its own database.
func startPlatform(t *testing.T) *platform {
	t.Helper()
	root := repoRoot(t)
	binDir := t.TempDir()

	p := &platform{clients: map[string]*svcclient.Client{}, tenant: newID("tnt")}

	for _, svc := range services {
		prepareDatabase(t, svc)

		bin := filepath.Join(binDir, svc.name)
		build := exec.Command("go", "build", "-o", bin, "./cmd/server")
		build.Dir = filepath.Join(root, "services", svc.name)
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build %s: %v\n%s", svc.name, err, out)
		}

		port := freePort(t)
		addr := fmt.Sprintf("127.0.0.1:%d", port)

		cmd := exec.Command(bin)
		cmd.Env = append(os.Environ(),
			"SERVER_ADDR="+addr,
			"DATABASE_URL="+dsn(t, svc.database),
			// The ML tier is not part of this harness. Leaving these empty is a
			// supported deployment, and the assertions below hold without it —
			// which is itself worth proving.
			"DIVERGENCE_ML_URL=",
			"ANOMALY_ML_URL=",
			"UNCERTAINTY_ML_URL=",
		)
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatalf("start %s: %v", svc.name, err)
		}
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})

		client := svcclient.New(svcclient.Config{
			BaseURL: "http://" + addr,
			Timeout: 10 * time.Second,
		})
		waitReady(t, client, svc.name)
		p.clients[svc.name] = client
	}

	return p
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
func prepareDatabase(t *testing.T, svc service) {
	t.Helper()
	prepareMu.Lock()
	defer prepareMu.Unlock()
	if prepared[svc.database] {
		if err := schemaOnce[svc.database]; err != nil {
			t.Fatalf("prepare %s: %v", svc.database, err)
		}
		return
	}
	prepared[svc.database] = true

	err := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// "postgres" is the maintenance database every server has; a database
		// cannot be created from a connection to itself.
		admin, err := pgx.Connect(ctx, dsn(t, "postgres"))
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

		sql, err := os.ReadFile(filepath.Join(repoRoot(t), svc.schema))
		if err != nil {
			return fmt.Errorf("read schema: %w", err)
		}

		conn, err := pgx.Connect(ctx, dsn(t, svc.database))
		if err != nil {
			return fmt.Errorf("connect to %s: %w", svc.database, err)
		}
		defer conn.Close(ctx)

		// Every schema here is written with IF NOT EXISTS, so applying it to a
		// database left behind by an earlier run is a no-op rather than a
		// failure.
		if _, err := conn.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("apply schema to %s: %w", svc.database, err)
		}
		return nil
	}()

	schemaOnce[svc.database] = err
	if err != nil {
		t.Fatalf("prepare %s: %v", svc.database, err)
	}
}

func waitReady(t *testing.T, c *svcclient.Client, name string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := c.Health(ctx)
		cancel()
		if err == nil {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("%s did not become ready", name)
}

func (p *platform) canonical() *svcclient.Client { return p.clients["canonical-service"] }
func (p *platform) shadow() *svcclient.Client    { return p.clients["shadow-settlement-service"] }
func (p *platform) ingestion() *svcclient.Client { return p.clients["ingestion-service"] }

func (p *platform) opts() svcclient.CallOptions {
	return svcclient.CallOptions{TenantID: p.tenant, RequestID: newID("req")}
}
