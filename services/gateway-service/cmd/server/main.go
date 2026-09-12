package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"p9e.in/samavaya/packages/p9log"

	"github.com/ppusapati/gavya/libs/integrity/observe"
	"github.com/ppusapati/gavya/libs/integrity/serve"

	"github.com/ppusapati/gavya/services/gateway-service/config"
	"github.com/ppusapati/gavya/services/gateway-service/handler"
)

// main stays deliberately thin. Routing used to live here as a second, inline
// copy of the upstream table, which drifted: nine services — every one in the
// integrity layer among them — were reachable in the handler package and
// unreachable in the binary that actually runs. There is now one table, in one
// place, covered by the handler package's tests.
func main() {
	cfg := config.Load()
	log := p9log.NewHelper(p9log.DefaultLogger)

	mux := http.NewServeMux()
	h := handler.New(cfg, log)
	h.Register(mux)

	// The gateway's readiness is whether it can verify a session, and that is
	// the identity service's answer rather than a database of its own — this
	// process has none. Counted the same way as everything else, through the
	// metrics it is handed.
	metrics := observe.NewMetrics()
	mux.HandleFunc("/readyz", observe.Ready(observe.Check{
		Name: "identity-service", Ping: h.PingIdentity,
	}))
	mux.HandleFunc("/metrics", metrics.Handler())

	srv := serve.Unguarded(cfg.ServerAddr, h.CORS(mux), metrics)

	go func() {
		if err := serve.Run(srv, func(what string) {
			log.Infof("gateway-service %s", what)
		}); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
	log.Info("gateway-service stopped")
}
