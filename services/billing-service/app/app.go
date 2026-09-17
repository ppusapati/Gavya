// Package app is billing-service's composition root.
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
	"github.com/ppusapati/gavya/libs/integrity/sys"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"

	"github.com/ppusapati/gavya/services/billing-service/internal/config"
	"github.com/ppusapati/gavya/services/billing-service/internal/handler"
	"github.com/ppusapati/gavya/services/billing-service/internal/repository"
	"github.com/ppusapati/gavya/services/billing-service/internal/service"

	"p9e.in/samavaya/packages/p9log"
)

// Build opens this service's database and returns its handler and the pool.
//
// The pool rather than a close function, because the caller needs it for two
// things: closing it, and asking it whether the service is ready to serve.
func Build(ctx context.Context, log *p9log.Helper) (serve.Registrar, *pgxpool.Pool, error) {
	cfg := config.Load()
	pool, err := tenantdb.NewPoolFor(ctx, cfg.DatabaseURL, "billing-service")
	if err != nil {
		return nil, nil, fmt.Errorf("billing-service: db connect: %w", err)
	}
	repo := repository.New(pool, sys.IDs{})
	svc := service.New(repo, log)
	return handler.New(svc), pool, nil
}
