// Package app is settlement-service's composition root.
//
// The wiring used to live in main, which meant nothing but that main could build
// this service. It is here so the same code runs as its own process and as one
// module of the modulith, assembled identically either way.
//
// This is the one service that holds a client to another. What that costs and
// why it acts as itself rather than as the person who asked is on scoped's
// Collections method below.
package app

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ppusapati/gavya/libs/integrity/authz"

	"github.com/ppusapati/gavya/libs/integrity/serve"
	"github.com/ppusapati/gavya/libs/integrity/svcclient"
	"github.com/ppusapati/gavya/libs/integrity/sys"
	"github.com/ppusapati/gavya/libs/integrity/tenantctx"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"

	"github.com/ppusapati/gavya/services/settlement-service/internal/canonical"
	"github.com/ppusapati/gavya/services/settlement-service/internal/config"
	"github.com/ppusapati/gavya/services/settlement-service/internal/handler"
	"github.com/ppusapati/gavya/services/settlement-service/internal/notify"
	"github.com/ppusapati/gavya/services/settlement-service/internal/procurement"
	"github.com/ppusapati/gavya/services/settlement-service/internal/repository"
	"github.com/ppusapati/gavya/services/settlement-service/internal/service"

	"p9e.in/samavaya/packages/p9log"
	ulidpkg "p9e.in/samavaya/packages/ulid"
)

// Build opens this service's database and returns its handler and the pool.
//
// The pool rather than a close function, because the caller needs it for two
// things: closing it, and asking it whether the service is ready to serve.
func Build(ctx context.Context, log *p9log.Helper) (serve.Registrar, *pgxpool.Pool, error) {
	cfg := config.Load()
	pool, err := tenantdb.NewPoolFor(ctx, cfg.DatabaseURL, "settlement-service")
	if err != nil {
		return nil, nil, fmt.Errorf("settlement-service: db connect: %w", err)
	}

	repo := repository.New(pool, sys.IDs{})

	// The reader over procurement, or nothing.
	//
	// Left nil when no URL is configured, so a gather refuses with a message
	// naming the missing setting rather than reporting a fortnight in which
	// nobody earned anything.
	var milk service.Collections
	var readers *scoped
	if cfg.ProcurementURL != "" {
		readers = &scoped{
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
		milk = readers
	} else {
		log.Infof("PROCUREMENT_URL is not set: this service will refuse to gather")
	}

	// The reader over canonical, for explaining a payment. Optional, and only
	// meaningful beside procurement: an explanation with no collections has
	// nothing to look identities up for.
	if readers != nil && cfg.CanonicalURL != "" {
		readers.canonical = svcclient.New(svcclient.Config{
			BaseURL:     cfg.CanonicalURL,
			Timeout:     10 * time.Second,
			MaxAttempts: 3,
		})
	} else if cfg.CanonicalURL == "" {
		log.Infof("CANONICAL_URL is not set: an explained payment will say identity " +
			"history was not consulted")
	}

	svc := service.New(repo, milk, sys.IDs{}, sys.Clock{}, log)
	if readers != nil {
		svc.WithExplainers(readers)
	}

	// Delivering what the outbox owes.
	//
	// The dispatcher runs whether or not there is anywhere to deliver to. With
	// no NOTIFICATION_URL it still sweeps, and every sweep that finds messages
	// owed says so in the log — because an outbox that fills in silence is a
	// queue nobody knows about, which is the failure this whole arrangement
	// exists to remove. With one, a commit kicks a sweep and a held payment is
	// in the supervisor's inbox in milliseconds.
	//
	// Started here, on the context Build was given, and never stopped: the
	// process ending ends it, and there is nothing to flush — anything not yet
	// delivered is still in the table for the next start.
	var sender notify.Sender
	if cfg.NotificationURL != "" {
		if readers == nil {
			readers = &scoped{identity: cfg.ServiceIdentity}
		}
		sender = notify.NewClient(svcclient.New(svcclient.Config{
			BaseURL:     cfg.NotificationURL,
			Timeout:     10 * time.Second,
			MaxAttempts: 1, // the outbox is the retry
		}), readers.options)
	} else {
		log.Infof("NOTIFICATION_URL is not set: notifications will be queued and every sweep " +
			"will report them as owed")
	}
	dispatcher := notify.New(repo, sender, log, cfg.NotifyInterval)
	svc.WithKicker(dispatcher)
	go dispatcher.Run(ctx)

	// And make the outbox watchable. The sweep above logs what it finds, which
	// is a line somebody reads if they are already looking; this is the number
	// that pages them when they are not. A sweep that stops leaves every money
	// event recorded correctly and nobody told about any of them.
	notify.PublishOutboxDepth(ctx, pool, 15*time.Second)

	return handler.New(svc), pool, nil
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
	// canonical is the second reader, for identity history. Nil when not
	// configured, in which case IdentityHistory says so rather than guessing.
	canonical *svcclient.Client
}

// options is what every outbound call from this service carries: the verified
// tenant, a fresh request id, and this service's own identity and permissions.
func (s *scoped) options(ctx context.Context, tenantID string) func() svcclient.CallOptions {
	// The tenant on the context is the verified one, put there by the handler
	// from the transport. It outranks the id in the request body for the same
	// reason it does everywhere else: the body is what the caller asked for and
	// the context is what they proved.
	tenant := tenantID
	if t, err := tenantctx.From(ctx); err == nil && t != "" {
		tenant = t
	}
	return func() svcclient.CallOptions {
		return svcclient.CallOptions{
			TenantID: tenant, Tenant: tenant,
			RequestID: ulidpkg.New().String(),
			// A service identity, not a person's. Settlement reads these
			// collections on its own behalf, and attributing the read to
			// whoever pressed the button would put a name in the trail that
			// did not make the call.
			ServiceIdentity: s.identity,
			// And its own permissions, for the same reason. The alternative is
			// to forward the caller's — settlement reading collections as the
			// accountant who asked — which sounds stricter and buys nothing
			// here: what comes back to that accountant is the gathered cycle,
			// which settlement.write already entitles them to, not the
			// collections themselves. Acting as itself keeps one answer to
			// "what is settlement allowed to read", which is a thing somebody
			// can look up, rather than one that varies by who pressed the
			// button.
			Permissions: authz.Roles()["service"].Permissions.String(),
		}
	}
}

func (s *scoped) Collections(ctx context.Context, tenantID, societyCode string, from, to time.Time) ([]procurement.Collection, error) {
	return procurement.New(s.client, s.options(ctx, tenantID)).Collections(ctx, tenantID, societyCode, from, to)
}

func (s *scoped) Versions(ctx context.Context, tenantID, collectionID string) ([]procurement.Version, error) {
	return procurement.New(s.client, s.options(ctx, tenantID)).Versions(ctx, tenantID, collectionID)
}

func (s *scoped) RateCard(ctx context.Context, tenantID, id string) (*procurement.RateCard, error) {
	return procurement.New(s.client, s.options(ctx, tenantID)).RateCard(ctx, tenantID, id)
}

func (s *scoped) IdentityHistory(ctx context.Context, tenantID, sourceSystemID, externalID string) ([]canonical.Mapping, error) {
	if s.canonical == nil {
		return nil, service.ErrNoCanonical
	}
	return canonical.New(s.canonical, s.options(ctx, tenantID)).History(ctx, tenantID, sourceSystemID, externalID)
}
