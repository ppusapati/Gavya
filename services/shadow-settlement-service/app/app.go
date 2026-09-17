// Package app is shadow-settlement-service's composition root.
//
// The wiring used to live in main, which meant nothing but that main could build
// this service. It is here so the same code runs as its own process and as one
// module of the modulith, assembled identically either way — a module wired
// differently in the two shapes is one that is tested in only one of them.
//
// The divergence model is optional and advisory: its answer is stored beside
// the deterministic verdict, never in place of it.
package app

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ppusapati/gavya/libs/integrity/mlclient"

	"github.com/ppusapati/gavya/libs/integrity/serve"
	"github.com/ppusapati/gavya/libs/integrity/sys"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"

	"github.com/ppusapati/gavya/services/shadow-settlement-service/internal/config"
	"github.com/ppusapati/gavya/services/shadow-settlement-service/internal/handler"
	"github.com/ppusapati/gavya/services/shadow-settlement-service/internal/repository"
	"github.com/ppusapati/gavya/services/shadow-settlement-service/internal/service"

	"p9e.in/samavaya/packages/p9log"
)

// Build opens this service's database and returns its handler and the pool.
//
// The pool rather than a close function, because the caller needs it for two
// things: closing it, and asking it whether the service is ready to serve.
func Build(ctx context.Context, log *p9log.Helper) (serve.Registrar, *pgxpool.Pool, error) {
	cfg := config.Load()
	pool, err := tenantdb.NewPoolFor(ctx, cfg.DatabaseURL, "shadow-settlement-service")
	if err != nil {
		return nil, nil, fmt.Errorf("shadow-settlement-service: db connect: %w", err)
	}

	var ml *mlclient.DivergenceClient
	if cfg.DivergenceMLURL != "" {
		ml = mlclient.NewDivergenceClient(mlclient.Config{
			BaseURL:      cfg.DivergenceMLURL,
			Timeout:      cfg.DivergenceMLTimeout,
			ModelVersion: cfg.DivergenceModelPin,
		})
		log.Infof("divergence ml tier enabled at %s", cfg.DivergenceMLURL)
	} else {
		log.Infof("divergence ml tier disabled; unexplained divergences go straight to review")
	}

	repo := repository.New(pool, sys.IDs{})
	svc := service.New(repo, log, ml)
	return handler.New(svc), pool, nil
}
