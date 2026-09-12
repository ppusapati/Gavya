// Command server runs settlement-service as its own process.
//
// Everything this service is made of is assembled in internal/app, which the
// modulith also calls. This file is only the part that differs between running
// alone and running alongside: a port, a signal, and a shutdown.
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

	"github.com/ppusapati/gavya/services/settlement-service/app"
	"github.com/ppusapati/gavya/services/settlement-service/internal/config"

	"p9e.in/samavaya/packages/p9log"
)

func main() {
	cfg := config.Load()
	log := p9log.NewHelper(p9log.DefaultLogger)

	h, closePool, err := app.Build(context.Background(), log)
	if err != nil {
		log.Errorf("%v", err)
		os.Exit(1)
	}
	defer closePool()

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

	shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
}
