package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
	"github.com/ppusapati/gavya/libs/integrity/tenantctx"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"

	"github.com/ppusapati/gavya/services/settlement-service/internal/config"
	"github.com/ppusapati/gavya/services/settlement-service/internal/handler"
	"github.com/ppusapati/gavya/services/settlement-service/internal/procurement"
	"github.com/ppusapati/gavya/services/settlement-service/internal/repository"
	"github.com/ppusapati/gavya/services/settlement-service/internal/service"

	"p9e.in/samavaya/packages/p9log"
	ulidpkg "p9e.in/samavaya/packages/ulid"
)

type ids struct{}

func (ids) New() string { return ulidpkg.New().String() }

type clock struct{}

func (clock) Now() time.Time { return time.Now().UTC() }

func main() {
	cfg := config.Load()
	log := p9log.NewHelper(p9log.DefaultLogger)

	pool, err := tenantdb.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Errorf("db connect: %v", err)
		os.Exit(1)
	}
	defer pool.Close()

	repo := repository.New(pool, ids{})

	// The reader over procurement, or nothing.
	//
	// Left nil when no URL is configured, so a gather refuses with a message
	// naming the missing setting rather than reporting a fortnight in which
	// nobody earned anything.
	var milk service.Collections
	if cfg.ProcurementURL != "" {
		milk = &scoped{
			client: svcclient.New(svcclient.Config{
				BaseURL: cfg.ProcurementURL,
				// A settlement reads thousands of rows in pages, so a page is
				// allowed longer than an interactive call.
				Timeout: 30 * time.Second,
				// Reading collections has no effect, so retrying one is safe. It
				// is also the call most worth retrying: a settlement that failed
				// halfway through paging is a settlement somebody has to start
				// again.
				MaxAttempts: 3,
			}),
			identity: cfg.ServiceIdentity,
		}
	} else {
		log.Infof("PROCUREMENT_URL is not set: this service will refuse to gather")
	}

	svc := service.New(repo, milk, ids{}, clock{}, log)
	h := handler.New(svc)

	mux := http.NewServeMux()
	h.Register(mux)

	srv := &http.Server{Addr: cfg.ServerAddr, Handler: h2c.NewHandler(mux, &http2.Server{})}

	go func() {
		log.Infof("starting %s on %s", cfg.ServiceName, cfg.ServerAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errorf("server: %v", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

// scoped carries the calling tenant through to procurement.
//
// Without it every gather would reach procurement with no tenant on the
// transport, and procurement's connection would be unscoped — which the database
// refuses, so it would fail closed rather than leak. Failing closed is the right
// direction and it is still a broken service, so the tenant is passed on
// explicitly here.
// One client, held for the life of the process so connections are reused. Only
// the per-call options are rebuilt, which is where the tenant and the request id
// belong.
type scoped struct {
	client   *svcclient.Client
	identity string
}

func (s *scoped) Collections(ctx context.Context, tenantID, societyCode string, from, to time.Time) ([]procurement.Collection, error) {
	// The tenant on the context is the verified one, put there by the handler
	// from the transport. It outranks the id in the request body for the same
	// reason it does everywhere else: the body is what the caller asked for and
	// the context is what they proved.
	tenant := tenantID
	if t, err := tenantctx.From(ctx); err == nil && t != "" {
		tenant = t
	}
	reader := procurement.New(s.client, func() svcclient.CallOptions {
		return svcclient.CallOptions{
			TenantID: tenant, Tenant: tenant,
			RequestID: ulidpkg.New().String(),
			// A service identity, not a person's. Settlement reads these
			// collections on its own behalf, and attributing the read to
			// whoever pressed the button would put a name in the trail that
			// did not make the call.
			ServiceIdentity: s.identity,
		}
	})
	return reader.Collections(ctx, tenantID, societyCode, from, to)
}
