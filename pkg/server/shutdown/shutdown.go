package shutdown

import (
	"io"
	"os"
	"os/signal"
	"syscall"

	"p9e.in/samavaya/packages/p9log"
)

func AddShutdownHook(logs p9log.Helper, closers ...io.Closer) {
	logs.Info("listening signals...")
	c := make(chan os.Signal, 1)
	signal.Notify(
		c, os.Interrupt, syscall.SIGHUP, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM,
	)

	<-c
	logs.Info("graceful shutdown...")

	for _, closer := range closers {
		if err := closer.Close(); err != nil {
			logs.Error("failed to stop closer", err)
		}
	}

	logs.Info("completed graceful shutdown")

}
