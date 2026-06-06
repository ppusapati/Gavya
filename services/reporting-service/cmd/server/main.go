package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ppusapati/gavya/services/reporting-service/internal/config"
	"github.com/ppusapati/gavya/services/reporting-service/internal/handler"
	"github.com/ppusapati/gavya/services/reporting-service/internal/repository"
	"github.com/ppusapati/gavya/services/reporting-service/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"p9e.in/samavaya/packages/p9log"
)

func main() {
	cfg := config.Load()
	log := p9log.NewHelper(p9log.DefaultLogger)

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	repo := repository.New(pool)
	svc := service.New(repo, log)
	h := handler.New(svc, log)

	mux := http.NewServeMux()
	h.Register(mux)

	srv := &http.Server{
		Addr:    cfg.ServerAddr,
		Handler: h2c.NewHandler(mux, &http2.Server{}),
	}

	go func() {
		log.Infof("reporting-service listening on %s", cfg.ServerAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
	log.Info("reporting-service stopped")
}
