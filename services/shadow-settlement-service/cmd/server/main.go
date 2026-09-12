package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/serve"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"

	"github.com/ppusapati/gavya/libs/integrity/mlclient"
	"p9e.in/samavaya/packages/p9log"

	"github.com/ppusapati/gavya/services/shadow-settlement-service/internal/config"
	"github.com/ppusapati/gavya/services/shadow-settlement-service/internal/handler"
	"github.com/ppusapati/gavya/services/shadow-settlement-service/internal/repository"
	"github.com/ppusapati/gavya/services/shadow-settlement-service/internal/service"
)

func main() {
	cfg := config.Load()
	log := p9log.NewHelper(p9log.DefaultLogger)

	pool, err := tenantdb.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Errorf("db connect: %v", err)
		os.Exit(1)
	}
	defer pool.Close()

	// The divergence model is optional. Without it the platform still classifies
	// every divergence deterministically; it simply stops offering hypotheses
	// for the unexplained remainder.
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

	repo := repository.New(pool)
	svc := service.New(repo, log, ml)
	h := handler.New(svc)

	mux := http.NewServeMux()
	h.Register(mux)

	srv := serve.New(cfg.ServerAddr, mux)

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
