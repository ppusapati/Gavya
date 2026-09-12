package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/serve"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"

	"github.com/ppusapati/gavya/services/health-service/internal/config"
	"github.com/ppusapati/gavya/services/health-service/internal/handler"
	"github.com/ppusapati/gavya/services/health-service/internal/repository"
	"github.com/ppusapati/gavya/services/health-service/internal/service"

	"p9e.in/samavaya/packages/p9log"
	ulidpkg "p9e.in/samavaya/packages/ulid"
)

// ids supplies the identifier each audit entry carries. No prefix: every id
// column in this platform is VARCHAR(26), which is exactly a ULID.
type ids struct{}

func (ids) New() string { return ulidpkg.New().String() }

func main() {
	cfg := config.Load()
	log := p9log.NewHelper(p9log.DefaultLogger)
	pool, err := tenantdb.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Errorf("db: %v", err)
		os.Exit(1)
	}
	defer pool.Close()
	repo := repository.New(pool, ids{})
	svc := service.New(repo, log)
	h := handler.New(svc)
	mux := http.NewServeMux()
	h.Register(mux)
	srv := serve.New(cfg.ServerAddr, mux)

	go func() {
		log.Infof("starting %s on %s", cfg.ServiceName, cfg.ServerAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Errorf("server: %v", err)
			os.Exit(1)
		}
	}()
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}
