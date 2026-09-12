// Package app is balance-service's composition root.
//
// The wiring used to live in main, which meant nothing but that main could build
// this service. It is here so the same code runs as its own process and as one
// module of the modulith, assembled identically either way — a module wired
// differently in the two shapes is one that is tested in only one of them.
//
// The reconciler is an optional ML client: without it a window is still
// validated and its imbalance still recorded.
package app

import (
	"context"
	"fmt"

	"github.com/ppusapati/gavya/libs/integrity/mlclient"
	"github.com/ppusapati/gavya/libs/integrity/serve"
	"github.com/ppusapati/gavya/libs/integrity/sys"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"

	"github.com/ppusapati/gavya/services/balance-service/internal/config"
	"github.com/ppusapati/gavya/services/balance-service/internal/handler"
	"github.com/ppusapati/gavya/services/balance-service/internal/repository"
	"github.com/ppusapati/gavya/services/balance-service/internal/service"

	"p9e.in/samavaya/packages/p9log"
)

// Build opens this service's database and returns its handler, and the function
// that closes what was opened.
func Build(ctx context.Context, log *p9log.Helper) (serve.Registrar, func(), error) {
	cfg := config.Load()
	pool, err := tenantdb.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("balance-service: db connect: %w", err)
	}

	// The reconciler is optional. Without it a window is still validated and its
	// imbalance still computed and recorded; the run simply carries no
	// adjustments and nominates nobody.
	var reconciler *mlclient.ReconciliationClient
	if cfg.ReconciliationMLURL != "" {
		reconciler = mlclient.NewReconciliationClient(mlclient.Config{
			BaseURL:      cfg.ReconciliationMLURL,
			Timeout:      cfg.ReconciliationMLTimeout,
			ModelVersion: cfg.ReconciliationModelPin,
		})
		log.Infof("reconciliation ml tier enabled at %s", cfg.ReconciliationMLURL)
	} else {
		log.Infof("reconciliation ml tier disabled; runs record the local imbalance only")
	}

	repo := repository.New(pool, sys.IDs{})
	svc := service.New(repo, log, reconciler)
	return handler.New(svc), pool.Close, nil
}
