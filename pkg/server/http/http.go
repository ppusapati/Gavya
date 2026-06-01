package http

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync"

	"p9e.in/samavaya/packages/api/v1/config"
	"p9e.in/samavaya/packages/middleware/tenant"
	"p9e.in/samavaya/packages/p9log"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/rs/cors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Default CORS settings for when config is not provided
var (
	defaultAllowedMethods = []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"}
	defaultAllowedHeaders = []string{"Authorization", "Content-Type", "X-Tenant-ID", "X-Tenant-Name", "X-Request-ID"}
)

// CORSConfig holds CORS configuration options.
// This can be populated from environment variables, config files, or programmatically.
// Once proto is regenerated, this will be replaced by config.Server_CORS.
type CORSConfig struct {
	// AllowedOrigins is a list of origins a cross-domain request can be executed from.
	AllowedOrigins []string
	// AllowedMethods is a list of methods the client is allowed to use.
	AllowedMethods []string
	// AllowedHeaders is a list of non-simple headers the client is allowed to use.
	AllowedHeaders []string
	// ExposedHeaders indicates which headers are safe to expose.
	ExposedHeaders []string
	// AllowCredentials indicates whether the request can include user credentials.
	AllowCredentials bool
	// MaxAge indicates how long (in seconds) the results of a preflight request can be cached.
	MaxAge int
	// Debug adds additional output to debug server side CORS issues.
	Debug bool
}

// CustomHttpServer represents a custom HTTP server.
type CustomHttpServer struct {
	httpServer    *http.Server
	mux           *runtime.ServeMux
	activeCounter sync.WaitGroup
	log           p9log.Helper
	cfg           *config.Server
	corsConfig    *CORSConfig
}

// NewCustomHttpServer creates a new instance of the CustomHttpServer with a provided mux and custom header matcher.
func NewCustomHttpServer(cfg *config.Server, mux *runtime.ServeMux, log p9log.Helper) *CustomHttpServer {
	httpServer := &http.Server{
		Addr:    cfg.Http.Addr,
		Handler: nil,
	}

	return &CustomHttpServer{
		httpServer: httpServer,
		mux:        mux,
		log:        log,
		cfg:        cfg,
	}
}

// NewCustomHttpServerWithCORS creates a CustomHttpServer with explicit CORS configuration.
func NewCustomHttpServerWithCORS(cfg *config.Server, mux *runtime.ServeMux, log p9log.Helper, corsConfig *CORSConfig) *CustomHttpServer {
	server := NewCustomHttpServer(cfg, mux, log)
	server.corsConfig = corsConfig
	return server
}

// SetCORSConfig sets the CORS configuration for the server.
func (s *CustomHttpServer) SetCORSConfig(corsConfig *CORSConfig) {
	s.corsConfig = corsConfig
}

// Registers a gRPC service with the gateway.
func (s *CustomHttpServer) RegisterService(
	ctx context.Context,
	endpoint string,
	mux *runtime.ServeMux,
	registerFunc func(ctx context.Context, mux *runtime.ServeMux, endpoint string, opts []grpc.DialOption) error,
) {
	grpcDialOption := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}

	err := registerFunc(ctx, mux, endpoint, grpcDialOption)
	if err != nil {
		s.log.Fatal("Failed to Register GRPC service to HTTP with error:", err)
	}
}

// ListenAndServe starts the HTTP server.
func (s *CustomHttpServer) ListenAndServe() error {
	// Start the HTTP server with your custom middleware
	s.httpServer.Handler = s.CreateHandler(s.mux)

	s.log.Info("Serving HTTP on connection: ", s.cfg.Http.Addr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the HTTP server.
func (s *CustomHttpServer) Shutdown(ctx context.Context) error {
	s.log.Info("Shutting down HTTP server...")
	return s.httpServer.Shutdown(ctx)
}

// buildCORSOptions builds CORS options from configuration or environment variables
func (s *CustomHttpServer) buildCORSOptions() cors.Options {
	corsOpts := cors.Options{
		AllowedMethods: defaultAllowedMethods,
		AllowedHeaders: defaultAllowedHeaders,
		Debug:          false, // Disabled by default in production
	}

	// Try to get CORS config from explicit CORSConfig
	if s.corsConfig != nil {
		corsCfg := s.corsConfig

		// Allowed origins from config
		if len(corsCfg.AllowedOrigins) > 0 {
			corsOpts.AllowedOrigins = corsCfg.AllowedOrigins
		}

		// Allowed methods from config
		if len(corsCfg.AllowedMethods) > 0 {
			corsOpts.AllowedMethods = corsCfg.AllowedMethods
		}

		// Allowed headers from config
		if len(corsCfg.AllowedHeaders) > 0 {
			corsOpts.AllowedHeaders = corsCfg.AllowedHeaders
		}

		// Exposed headers from config
		if len(corsCfg.ExposedHeaders) > 0 {
			corsOpts.ExposedHeaders = corsCfg.ExposedHeaders
		}

		// Allow credentials from config
		corsOpts.AllowCredentials = corsCfg.AllowCredentials

		// Max age from config
		if corsCfg.MaxAge > 0 {
			corsOpts.MaxAge = corsCfg.MaxAge
		}

		// Debug mode from config (should be false in production)
		corsOpts.Debug = corsCfg.Debug
	}

	// Fallback to environment variables if origins not set
	if len(corsOpts.AllowedOrigins) == 0 {
		if origins := os.Getenv("CORS_ALLOWED_ORIGINS"); origins != "" {
			corsOpts.AllowedOrigins = strings.Split(origins, ",")
			// Trim whitespace from each origin
			for i, origin := range corsOpts.AllowedOrigins {
				corsOpts.AllowedOrigins[i] = strings.TrimSpace(origin)
			}
		} else {
			// Default to allow all origins only in development
			// In production, this should be explicitly configured
			s.log.Warn("CORS: No allowed origins configured, defaulting to allow all (*). Configure CORS_ALLOWED_ORIGINS or server.http.cors.allowed_origins for production.")
			corsOpts.AllowedOrigins = []string{"*"}
		}
	}

	// Override debug from environment if set
	if debugEnv := os.Getenv("CORS_DEBUG"); debugEnv == "true" {
		corsOpts.Debug = true
	}

	return corsOpts
}

// CreateHandler creates the HTTP handler with middleware, CORS, and the header matcher.
func (s *CustomHttpServer) CreateHandler(mux *runtime.ServeMux) http.Handler {
	corsOptions := s.buildCORSOptions()

	// Log CORS configuration (without debug details)
	s.log.Infof("CORS configured with %d allowed origins", len(corsOptions.AllowedOrigins))

	corsHandler := cors.New(corsOptions)
	handler := tenant.HttpTenantMiddleware(corsHandler.Handler(mux))
	handler = s.ActiveCounterMiddleware(handler)
	return handler
}

func (s *CustomHttpServer) ActiveCounterMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.activeCounter.Add(1)
		defer s.activeCounter.Done()
		next.ServeHTTP(w, r)
	})
}
