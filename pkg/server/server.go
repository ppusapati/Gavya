package server

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"p9e.in/samavaya/packages/p9log"
)

// Server represents the generic server framework.
type Server interface {
	Run(ctx context.Context) error
}

type MultiServer struct {
	servers []Server
}

func NewMultiServer(servers ...Server) *MultiServer {
	return &MultiServer{servers: servers}
}

// Run starts all registered servers and handles graceful shutdown.
func (s *MultiServer) Run(ctx context.Context) error {
	// Use a WaitGroup to track the completion of all servers
	var wg sync.WaitGroup

	// Create a cancelable context for graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Set up signal handling for graceful shutdown
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

	// Define a function to start a server and track it with the WaitGroup
	startServer := func(runner Server) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := runner.Run(ctx); err != nil {
				p9log.Error(err)
				// Handle server-specific error here
			}
		}()
	}

	// Start all registered servers
	for _, runner := range s.servers {
		startServer(runner)
	}

	select {
	case sig := <-signalChan:
		// Received an interrupt signal, start graceful shutdown
		// Handle signal-specific behavior here
		cancel() // Trigger the cancellation of the context
		p9log.Infof("Received signal: %v\n", sig)

	case <-ctx.Done():
		// Context was canceled, which means one of the servers failed or finished
		// Handle context-specific behavior here
	}

	// Wait for all servers to shut down gracefully
	wg.Wait()

	return nil
}

// type Server struct {
// 	Servers func(runner func(ctx context.Context) error)
// }

// func RunServer(s *ServerRunner, ctx context.Context) error {
// 	// Use a WaitGroup to track the completion of all servers
// 	var wg sync.WaitGroup

// 	// Create a cancelable context for graceful shutdown
// 	ctx, cancel := context.WithCancel(ctx)
// 	defer cancel()

// 	// Set up signal handling for graceful shutdown
// 	signalChan := make(chan os.Signal, 1)
// 	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)

// 	// Define a function to start a server and track it with the WaitGroup
// 	startServer := func(runner func(ctx context.Context) error) {
// 		wg.Add(1)
// 		go func() {
// 			defer wg.Done()
// 			if err := runner(ctx); err != nil {
// 				log.Errorf("Server error: %v", err)
// 				cancel() // Cancel the context if an error occurs
// 			}
// 		}()
// 	}

// 	// Start gRPC server
// 	startServer(func(ctx context.Context) error {
// 		return s.grpc.Run(ctx)
// 	})

// 	// Start HTTP server
// 	startServer(func(ctx context.Context) error {
// 		return s.http.Run(ctx)
// 	})

// 	// Start event server
// 	startServer(func(ctx context.Context) error {
// 		return s.event.Run(ctx)
// 	})

// 	select {
// 	case sig := <-signalChan:
// 		// Received an interrupt signal, start graceful shutdown
// 		log.Info("Received signal:", sig)
// 		cancel() // Trigger the cancellation of the context

// 	case <-ctx.Done():
// 		// Context was canceled, which means one of the servers failed or finished
// 		return ctx.Err()
// 	}

// 	// Wait for all servers to shut down gracefully
// 	log.Info("Waiting for servers to shut down gracefully...")
// 	wg.Wait()

// 	return nil
// }
