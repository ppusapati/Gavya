package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/ppusapati/gavya/libs/integrity/tenantdb"

	"github.com/ppusapati/gavya/services/identity-service/internal/config"
	"github.com/ppusapati/gavya/services/identity-service/internal/handler"
	"github.com/ppusapati/gavya/services/identity-service/internal/repository"
	"github.com/ppusapati/gavya/services/identity-service/internal/service"

	"p9e.in/samavaya/packages/p9log"
	ulidpkg "p9e.in/samavaya/packages/ulid"
)

// ids and clock are the two things the service takes from the outside world, so
// a test can hold both still.
//
// No prefix on the identifier. Every id column in this platform is VARCHAR(26),
// which is exactly a ULID and leaves no room for one.
type ids struct{}

func (ids) New() string { return ulidpkg.New().String() }

type clock struct{}

func (clock) Now() time.Time { return time.Now().UTC() }

func main() {
	cfg := config.Load()
	log := p9log.NewHelper(p9log.DefaultLogger)

	pool, err := tenantdb.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Errorf("db connect: %v", err)
		os.Exit(1)
	}
	defer pool.Close()

	repo := repository.New(pool)
	svc := service.New(repo, ids{}, clock{}, log)
	h := handler.New(svc)

	mux := http.NewServeMux()
	h.Register(mux)

	srv := &http.Server{Addr: cfg.ServerAddr, Handler: h2c.NewHandler(mux, &http2.Server{})}

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
