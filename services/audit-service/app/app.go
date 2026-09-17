// Package app is audit-service's composition root.
//
// The wiring used to live in main, which meant nothing but that main could build
// this service. It is here so the same code runs as its own process and as one
// module of the modulith, assembled identically either way — a module that is
// wired differently in the two shapes is a module that is tested in one of them.
package app

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/serve"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"

	"github.com/ppusapati/gavya/services/audit-service/internal/config"
	"github.com/ppusapati/gavya/services/audit-service/internal/handler"
	"github.com/ppusapati/gavya/services/audit-service/internal/repository"
	"github.com/ppusapati/gavya/services/audit-service/internal/service"

	"p9e.in/samavaya/packages/p9log"
)

// Build opens this service's database and returns its handler and the pool.
//
// The pool rather than a close function, because the caller needs it for two
// things: closing it, and asking it whether the service is ready to serve.
func Build(ctx context.Context, log *p9log.Helper) (serve.Registrar, *pgxpool.Pool, error) {
	cfg := config.Load()
	// public, and the only service that is.
	//
	// audit-service's schema is one table, audit_logs, and twenty-five services
	// write to it through libs/integrity/audit with an unqualified name. Put it
	// in a schema of its own and every one of those writes would have to know
	// where it went. It is the platform's shared table rather than this
	// service's private one, so it lives where everything can see it and this
	// service reads it there like everybody else.
	pool, err := tenantdb.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("audit-service: db connect: %w", err)
	}
	repo := repository.New(pool)
	svc := service.New(repo, log)

	// Walk the chain on a schedule and publish what it found.
	//
	// The trail is hash-chained so a row altered after the fact can be shown to
	// have been. Nothing ever checked: a break was found when somebody thought
	// to ask, which in practice is during the audit the chain exists to survive.
	// Evidence nobody has looked at since it was written is a claim.
	svc.WatchChain(ctx, repo.TenantsWithEntries)
	return handler.New(svc), pool, nil
}
