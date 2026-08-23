package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/ppusapati/gavya/libs/integrity/mlclient"
	"p9e.in/samavaya/packages/p9log"

	"github.com/ppusapati/gavya/services/observation-service/internal/config"
	"github.com/ppusapati/gavya/services/observation-service/internal/domain"
	"github.com/ppusapati/gavya/services/observation-service/internal/handler"
	"github.com/ppusapati/gavya/services/observation-service/internal/repository"
	"github.com/ppusapati/gavya/services/observation-service/internal/service"
)

func main() {
	cfg := config.Load()
	log := p9log.NewHelper(p9log.DefaultLogger)

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Errorf("db connect: %v", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Both models are optional. Without them observations are still recorded,
	// validated and given their legal-metrology verdict; they simply carry no
	// stated confidence and are never marked for review.
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

	repo := repository.New(pool)
	// The measurement-control regime is required, and the service refuses to
	// start without one. Defaulting it would mean a deployment in a country
	// nobody configured issuing eligibility verdicts that cite the wrong law —
	// and looking, to every reader, exactly like one that had been configured.
	regime, err := domain.LookupRegime(cfg.MeasurementRegime)
	if err != nil {
		log.Fatalf("MEASUREMENT_REGIME: %v", err)
	}
	log.Infof("eligibility assessed under %s (%s)", regime.Name, regime.ID)

	svc := service.New(repo, log, uncertainty, anomaly, regime)
	h := handler.New(svc)

	mux := http.NewServeMux()
	h.Register(mux)

	srv := &http.Server{
		Addr:              cfg.ServerAddr,
		Handler:           h2c.NewHandler(mux, &http2.Server{}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Infof("%s listening on %s", cfg.ServiceName, cfg.ServerAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Errorf("listen: %v", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Infof("shutdown signal received, draining")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Errorf("shutdown: %v", err)
	}
}
