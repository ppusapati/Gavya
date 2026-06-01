package consumer

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"p9e.in/samavaya/packages/errors"
	"p9e.in/samavaya/packages/p9log"

	"github.com/IBM/sarama"
)

// GracefulShutdown manages the lifecycle of Kafka consumers with minimal lock contention
type GracefulShutdown struct {
	log         p9log.Helper
	cancelFuncs atomic.Value // Replace slice with atomic value
	consumers   sync.Map     // Use sync.Map for lock-free access
	wg          sync.WaitGroup
	closed      atomic.Bool
}

// NewGracefulShutdown creates a new instance of GracefulShutdown
func NewGracefulShutdown(log p9log.Logger) *GracefulShutdown {
	gs := &GracefulShutdown{
		log: *p9log.NewHelper(p9log.With(log, "module", "graceful-shutdown")),
	}
	gs.cancelFuncs.Store([]context.CancelFunc{})
	return gs
}

// RegisterConsumer adds a consumer to be managed during shutdown
func (gs *GracefulShutdown) RegisterConsumer(consumer sarama.ConsumerGroup) {
	gs.consumers.Store(consumer, struct{}{})
}

// WatchSignals sets up signal handling for graceful shutdown
func (gs *GracefulShutdown) WatchSignals(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)

	// Atomic update of cancel functions
	gs.updateCancelFuncs(cancel)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)

	go func() {
		sig := <-sigChan
		gs.log.Infof("Received signal %v: initiating graceful shutdown", sig)

		// Atomic cancellation
		gs.cancelAll()
		gs.closeConsumers()
		gs.wg.Wait()

		gs.log.Info("Graceful shutdown completed")
		os.Exit(0)
	}()

	return ctx
}

// updateCancelFuncs updates cancel functions atomically
func (gs *GracefulShutdown) updateCancelFuncs(cancel context.CancelFunc) {
	for {
		oldFuncs := gs.cancelFuncs.Load().([]context.CancelFunc)
		newFuncs := append(oldFuncs, cancel)
		if gs.cancelFuncs.CompareAndSwap(oldFuncs, newFuncs) {
			break
		}
	}
}

// cancelAll cancels all registered context cancel functions
func (gs *GracefulShutdown) cancelAll() {
	funcs := gs.cancelFuncs.Load().([]context.CancelFunc)
	for _, cancelFunc := range funcs {
		cancelFunc()
	}
}

// closeConsumers safely closes all registered Kafka consumers with minimal lock contention
func (gs *GracefulShutdown) closeConsumers() {
	if !gs.closed.CompareAndSwap(false, true) {
		return // Already closed
	}

	var consumerErrors []error

	gs.consumers.Range(func(consumer, _ interface{}) bool {
		gs.wg.Add(1)
		go func(cg sarama.ConsumerGroup) {
			defer gs.wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			select {
			case <-ctx.Done():
				consumerErrors = append(consumerErrors,
					errors.New(500, "timeout", "Consumer close timed out"))
			default:
				if err := cg.Close(); err != nil {
					consumerErrors = append(consumerErrors,
						errors.New(500, "close_error", "Error closing consumer group").WithCause(err),
					)
				}
			}
		}(consumer.(sarama.ConsumerGroup))
		return true
	})

	gs.wg.Wait()

	// Log accumulated errors using p9e.in/samavaya/packages/errors package
	if len(consumerErrors) > 0 {
		gs.log.Errorf("Shutdown errors: %v", consumerErrors)
	}
}

// WithTimeout adds a timeout to the context to prevent indefinite blocking
func (gs *GracefulShutdown) WithTimeout(ctx context.Context, duration time.Duration) context.Context {
	ctx, cancel := context.WithTimeout(ctx, duration)
	gs.updateCancelFuncs(cancel)
	return ctx
}
