// Package app is observation-service's composition root.
//
// The wiring used to live in main, which meant nothing but that main could build
// this service. It is here so the same code runs as its own process and as one
// module of the modulith, assembled identically either way — a module wired
// differently in the two shapes is one that is tested in only one of them.
//
// Two optional ML clients and the deployment's measurement-control regime.
// Both tiers are advisory: a reading is recorded either way.
package app

import (
	"context"
	"fmt"

	"github.com/ppusapati/gavya/libs/integrity/mlclient"
	"github.com/ppusapati/gavya/libs/integrity/serve"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"

	"github.com/ppusapati/gavya/services/observation-service/internal/config"
	"github.com/ppusapati/gavya/services/observation-service/internal/domain"
	"github.com/ppusapati/gavya/services/observation-service/internal/handler"
	"github.com/ppusapati/gavya/services/observation-service/internal/repository"
	"github.com/ppusapati/gavya/services/observation-service/internal/service"

	"p9e.in/samavaya/packages/p9log"
)

// Build opens this service's database and returns its handler, and the function
// that closes what was opened.
func Build(ctx context.Context, log *p9log.Helper) (serve.Registrar, func(), error) {
	cfg := config.Load()
	pool, err := tenantdb.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("observation-service: db connect: %w", err)
	}

	var uncertainty *mlclient.UncertaintyClient
	if cfg.UncertaintyMLURL != "" {
		uncertainty = mlclient.NewUncertaintyClient(mlclient.Config{
			BaseURL:      cfg.UncertaintyMLURL,
			Timeout:      cfg.UncertaintyMLTimeout,
			ModelVersion: cfg.UncertaintyModelPin,
		})
		log.Infof("uncertainty ml tier enabled at %s", cfg.UncertaintyMLURL)
	} else {
		log.Infof("uncertainty ml tier disabled; observations record with no stated confidence")
	}

	var anomaly *mlclient.AnomalyClient
	if cfg.AnomalyMLURL != "" {
		anomaly = mlclient.NewAnomalyClient(mlclient.Config{
			BaseURL:      cfg.AnomalyMLURL,
			Timeout:      cfg.AnomalyMLTimeout,
			ModelVersion: cfg.AnomalyModelPin,
		})
		log.Infof("anomaly ml tier enabled at %s", cfg.AnomalyMLURL)
	} else {
		log.Infof("anomaly ml tier disabled; observations record unscored")
	}

	// The measurement-control regime is required, and the service refuses to
	// start without one. Defaulting it would mean a deployment in a country
	// nobody configured issuing eligibility verdicts that cite the wrong law —
	// and looking, to every reader, exactly like one that had been configured.
	//
	// The setting is named in the message, and so are the options. An operator
	// who is told only that something is missing guesses, and the guess that
	// starts the service is the wrong one.
	regime, err := domain.LookupRegime(cfg.MeasurementRegime)
	if err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("MEASUREMENT_REGIME: %w", err)
	}
	log.Infof("eligibility assessed under %s (%s)", regime.Name, regime.ID)

	repo := repository.New(pool)
	svc := service.New(repo, log, uncertainty, anomaly, regime)
	return handler.New(svc), pool.Close, nil
}
