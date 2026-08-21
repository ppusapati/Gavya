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

	"p9e.in/samavaya/packages/p9log"

	"github.com/ppusapati/gavya/services/ingestion-service/internal/config"
	"github.com/ppusapati/gavya/services/ingestion-service/internal/handler"
	"github.com/ppusapati/gavya/services/ingestion-service/internal/repository"
	"github.com/ppusapati/gavya/services/ingestion-service/internal/service"
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

	repo := repository.New(pool)
	svc := service.New(repo, log)
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
