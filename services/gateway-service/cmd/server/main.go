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
	"p9e.in/samavaya/packages/p9log"

	"github.com/ppusapati/gavya/services/gateway-service/internal/config"
	"github.com/ppusapati/gavya/services/gateway-service/internal/handler"
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

	srv := &http.Server{
		Addr:              cfg.ServerAddr,
		Handler:           h2c.NewHandler(h.CORS(mux), &http2.Server{}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Infof("gateway-service listening on %s", cfg.ServerAddr)
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
	log.Info("gateway-service stopped")
}
