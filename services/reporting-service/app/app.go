// Package app is reporting-service's composition root.
//
// The wiring used to live in main, which meant nothing but that main could build
// this service. It is here so the same code runs as its own process and as one
// module of the modulith, assembled identically either way — a module that is
// wired differently in the two shapes is a module that is tested in one of them.
package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/authz"
	"github.com/ppusapati/gavya/libs/integrity/serve"
	"github.com/ppusapati/gavya/libs/integrity/signedurl"
	"github.com/ppusapati/gavya/libs/integrity/svcclient"
	"github.com/ppusapati/gavya/libs/integrity/sys"
	"github.com/ppusapati/gavya/libs/integrity/tenantctx"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"

	"github.com/ppusapati/gavya/services/reporting-service/internal/config"
	"github.com/ppusapati/gavya/services/reporting-service/internal/handler"
	"github.com/ppusapati/gavya/services/reporting-service/internal/report"
	"github.com/ppusapati/gavya/services/reporting-service/internal/repository"
	"github.com/ppusapati/gavya/services/reporting-service/internal/runner"
	"github.com/ppusapati/gavya/services/reporting-service/internal/service"
	"github.com/ppusapati/gavya/services/reporting-service/internal/sources"

	"p9e.in/samavaya/packages/p9log"
	ulidpkg "p9e.in/samavaya/packages/ulid"
)

// Build opens this service's database and returns its handler and the pool.
//
// The pool rather than a close function, because the caller needs it for two
// things: closing it, and asking it whether the service is ready to serve.
func Build(ctx context.Context, log *p9log.Helper) (serve.Registrar, *pgxpool.Pool, error) {
	cfg := config.Load()
	pool, err := tenantdb.NewPoolFor(ctx, cfg.DatabaseURL, "reporting-service")
	if err != nil {
		return nil, nil, fmt.Errorf("reporting-service: db connect: %w", err)
	}
	repo := repository.New(pool)
	svc := service.New(repo, log).WithClock(sys.Clock{})

	// Signed download links.
	//
	// A browser following an <a href> sends no Authorization header, so a link
	// carries its own authority. Without a key this service still starts and
	// still serves everything else; GetReportDownloadURL refuses with the
	// setting named, and GetReportContent still hands a report over through
	// the authenticated channel.
	links := signedurl.FromEnv()
	if links.Why != "" {
		log.Infof("download links: %s", links.Why)
	}
	svc.WithDownloads(links.Keys, links.Base, links.Lifetime)

	// The readers a report draws on.
	//
	// Each is built only when this deployment was told where the service is.
	// A missing one is not a failure to start: the report that needs it fails
	// with the setting named, which is how somebody finds out that the setting
	// is missing, and every other procedure goes on working. Said once here at
	// startup and again on every report that wants it, because the startup line
	// is read once and the reports keep being requested.
	opts := callOptions(cfg.ServiceIdentity)
	src := &report.Sources{}

	if cfg.ProcurementURL != "" {
		src.Collections = sources.NewProcurement(svcclient.New(svcclient.Config{
			BaseURL: cfg.ProcurementURL,
			// A report pages thousands of rows, so a page is allowed longer
			// than an interactive call.
			Timeout: 30 * time.Second,
			// Reading collections has no effect, so retrying one is safe, and
			// it is the call most worth retrying: a report that failed halfway
			// through paging is a report somebody has to ask for again.
			MaxAttempts: 3,
		}), opts)
	} else {
		log.Infof("PROCUREMENT_URL is not set: a collections report will refuse and say so")
	}

	if cfg.SettlementURL != "" {
		src.Payables = sources.NewSettlement(svcclient.New(svcclient.Config{
			BaseURL: cfg.SettlementURL, Timeout: 30 * time.Second, MaxAttempts: 3,
		}), opts)
	} else {
		log.Infof("SETTLEMENT_URL is not set: a settlement summary will refuse and say so")
	}

	if cfg.ShadowSettlementURL != "" {
		src.Divergences = sources.NewShadow(svcclient.New(svcclient.Config{
			BaseURL: cfg.ShadowSettlementURL, Timeout: 30 * time.Second, MaxAttempts: 3,
		}), opts)
	} else {
		log.Infof("SHADOW_SETTLEMENT_URL is not set: a divergence report will refuse and say so")
	}

	// Producing what was asked for, and firing what was written down.
	//
	// Until this existed, reporting-service recorded intentions and acted on
	// none of them: a report was written with status 'pending' and stayed
	// there, and next_run_at was a column nothing computed. The console had to
	// say so on the page.
	//
	// Started on the context Build was given and never stopped: the process
	// ending ends it, and there is nothing to flush — a report claimed and not
	// finished is picked up by the reaper on the next start.
	if cfg.RunnerEnabled {
		r := runner.New(runner.Config{
			Store: repo, Sources: src, Log: log,
			IDs: ulidIDs{}, Clock: sys.Clock{},
			ReportEvery: cfg.ReportInterval, ScheduleEvery: cfg.ScheduleInterval,
		})
		svc.WithKicker(r)
		r.Run(ctx)
		log.Infof("reporting-service: the runner is sweeping reports every %s and schedules "+
			"every %s", cfg.ReportInterval, cfg.ScheduleInterval)
	} else {
		// Loudly, because a deployment with this off and nothing else running
		// the sweeps is the state this whole package was written to leave.
		log.Errorf("RUNNER_ENABLED is false: this process will record reports and produce " +
			"none of them, and will not fire any schedule")
	}

	h := handler.New(svc)
	return registrar{h}, pool, nil
}

// registrar adds the download route beside the procedures.
//
// serve.Registrar is one method, and the download route is deliberately not
// part of Register: it is not a procedure, it is not behind a session, and it
// is not in the permission table. Keeping it visible here rather than folding
// it into Register is the point.
type registrar struct{ h *handler.Handler }

func (r registrar) Register(mux *http.ServeMux) {
	r.h.Register(mux)
	r.h.RegisterDownload(mux)
}

// ulidIDs makes an identifier for a report a schedule creates.
type ulidIDs struct{}

func (ulidIDs) NewID() string { return ulidpkg.New().String() }

// callOptions is what every outbound call from this service carries.
//
// The tenant on the context is the verified one, put there by the handler from
// the transport, or by the runner from the row it claimed. It outranks anything
// in a request body for the same reason it does everywhere else: the body is
// what a caller asked for and the context is what was established.
func callOptions(identity string) sources.Options {
	return func(ctx context.Context, tenantID string) func() svcclient.CallOptions {
		tenant := tenantID
		if t, err := tenantctx.From(ctx); err == nil && t != "" {
			tenant = t
		}
		return func() svcclient.CallOptions {
			return svcclient.CallOptions{
				TenantID: tenant, Tenant: tenant,
				RequestID: ulidpkg.New().String(),
				// A service identity, not a person's. A schedule's report is
				// produced hours after anybody asked for anything, and often
				// nobody asked at all — attributing that read to whoever wrote
				// the schedule two years ago would put a name in the trail that
				// did not make the call.
				ServiceIdentity: identity,
				Permissions:     authz.Roles()["service"].Permissions.String(),
			}
		}
	}
}
