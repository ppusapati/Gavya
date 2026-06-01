// Package server provides utilities for building ConnectRPC servers with the unified p9context architecture.
package server

import (
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"p9e.in/samavaya/packages/connect/interceptors"
	"p9e.in/samavaya/packages/p9log"
)

// ServerConfig holds configuration for the Connect server.
type ServerConfig struct {
	// Port is the server port (e.g., "9090")
	Port string

	// AllowedOrigins for CORS (use ["*"] for development)
	AllowedOrigins []string

	// ReadHeaderTimeout is the timeout for reading request headers
	ReadHeaderTimeout time.Duration

	// ReadTimeout is the timeout for reading the entire request
	ReadTimeout time.Duration

	// WriteTimeout is the timeout for writing the response
	WriteTimeout time.Duration

	// IdleTimeout is the timeout for idle connections
	IdleTimeout time.Duration
}

// DefaultServerConfig returns a ServerConfig with sensible defaults.
func DefaultServerConfig(port string) ServerConfig {
	return ServerConfig{
		Port:              port,
		AllowedOrigins:    []string{"*"},
		ReadHeaderTimeout: 30 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

// MiddlewareConfig holds configuration for middleware/interceptors.
type MiddlewareConfig struct {
	// EnableRecovery enables panic recovery interceptor
	EnableRecovery bool

	// EnableRequestID enables request ID generation interceptor
	EnableRequestID bool

	// EnableLogging enables request logging interceptor
	EnableLogging bool

	// EnableAuth enables authentication interceptor
	EnableAuth bool

	// EnableRLS enables RLS scope interceptor
	EnableRLS bool

	// EnableDB enables database pool resolution interceptor
	EnableDB bool

	// JWTValidator is the JWT validator (required if EnableAuth is true)
	JWTValidator interceptors.JWTValidator

	// SessionRepository is optional for session validation
	SessionRepository interceptors.SessionRepository

	// RLSLevel is the RLS scoping level (default: ScopeLevelBranch)
	RLSLevel interceptors.ScopeLevel

	// SkipAuthProcedures are procedures that skip authentication
	SkipAuthProcedures []string

	// DBPool is the shared database pool (required if EnableDB is true)
	DBPool *pgxpool.Pool

	// DBPoolResolver is optional for multi-tenant database resolution
	DBPoolResolver interceptors.DBPoolResolver

	// Logger for logging interceptor. Pointer so nil = "use context logger".
	Logger *p9log.Helper

	// SlowRequestThreshold for logging slow requests
	SlowRequestThreshold time.Duration
}

// DefaultMiddlewareConfig returns a MiddlewareConfig with common defaults.
func DefaultMiddlewareConfig() MiddlewareConfig {
	return MiddlewareConfig{
		EnableRecovery:       true,
		EnableRequestID:      true,
		EnableLogging:        true,
		EnableAuth:           true,
		EnableRLS:            true,
		EnableDB:             true,
		RLSLevel:             interceptors.ScopeLevelBranch,
		SlowRequestThreshold: 5 * time.Second,
	}
}

// BuildInterceptors builds the interceptor chain based on configuration.
// The order is: Recovery -> RequestID -> Logging -> DB -> Auth -> RLS
func BuildInterceptors(cfg MiddlewareConfig) []connect.Interceptor {
	var chain []connect.Interceptor

	// 1. Recovery (always first to catch panics from all other interceptors)
	if cfg.EnableRecovery {
		chain = append(chain, interceptors.RecoveryInterceptor())
	}

	// 2. Request ID (early for tracing/correlation)
	if cfg.EnableRequestID {
		chain = append(chain, interceptors.RequestIDInterceptor())
	}

	// 3. Logging (after request ID so logs have correlation ID)
	if cfg.EnableLogging {
		opts := []interceptors.LoggingInterceptorOption{}
		if cfg.Logger != nil {
			opts = append(opts, interceptors.WithLogger(cfg.Logger))
		}
		if cfg.SlowRequestThreshold > 0 {
			opts = append(opts, interceptors.WithSlowRequestThreshold(cfg.SlowRequestThreshold))
		}
		chain = append(chain, interceptors.LoggingInterceptor(opts...))
	}

	// 4. Database pool resolution (before auth so auth can use DB if needed)
	if cfg.EnableDB && cfg.DBPool != nil {
		opts := []interceptors.DBInterceptorOption{}
		if cfg.DBPoolResolver != nil {
			opts = append(opts, interceptors.WithDBPoolResolver(cfg.DBPoolResolver))
		}
		chain = append(chain, interceptors.DBInterceptor(cfg.DBPool, opts...))
	}

	// 5. Authentication (extracts user context)
	if cfg.EnableAuth && cfg.JWTValidator != nil {
		opts := []interceptors.AuthInterceptorOption{}
		if len(cfg.SkipAuthProcedures) > 0 {
			opts = append(opts, interceptors.WithSkipProcedures(cfg.SkipAuthProcedures...))
		}
		if cfg.SessionRepository != nil {
			opts = append(opts, interceptors.WithSessionRepository(cfg.SessionRepository))
		}
		chain = append(chain, interceptors.AuthInterceptor(cfg.JWTValidator, opts...))
	}

	// 6. RLS (sets scope based on user context, must be after auth)
	if cfg.EnableRLS {
		switch cfg.RLSLevel {
		case interceptors.ScopeLevelCompany:
			chain = append(chain, interceptors.CompanyLevelInterceptor())
		case interceptors.ScopeLevelTenant:
			chain = append(chain, interceptors.TenantLevelInterceptor())
		default:
			chain = append(chain, interceptors.BranchLevelInterceptor())
		}
	}

	return chain
}

// NewConnectOption creates a connect.Option with the configured interceptors.
func NewConnectOption(cfg MiddlewareConfig) connect.Option {
	return connect.WithInterceptors(BuildInterceptors(cfg)...)
}

// NewHTTPServer creates an HTTP server configured for ConnectRPC.
func NewHTTPServer(cfg ServerConfig, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
	}
}

// WrapWithH2C wraps the handler with h2c for HTTP/2 without TLS.
func WrapWithH2C(handler http.Handler) http.Handler {
	return h2c.NewHandler(handler, &http2.Server{})
}

// WrapWithCORS wraps the handler with CORS middleware.
func WrapWithCORS(handler http.Handler, allowedOrigins []string) http.Handler {
	return CORSMiddleware(allowedOrigins)(handler)
}

// CORSMiddleware returns a middleware that adds CORS headers.
func CORSMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				origin = "*"
			}

			// Check if origin is allowed
			allowed := false
			for _, allowedOrigin := range allowedOrigins {
				if allowedOrigin == "*" || allowedOrigin == origin {
					allowed = true
					break
				}
			}

			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Connect-Protocol-Version, Connect-Timeout-Ms, X-Request-ID, X-Trace-ID, X-Branch-ID, X-Tenant-Name")
				w.Header().Set("Access-Control-Expose-Headers", "Connect-Protocol-Version, Connect-Timeout-Ms, X-Request-ID, X-Trace-ID")
				w.Header().Set("Access-Control-Max-Age", "7200")
			}

			// Handle preflight requests
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RegisterHealthEndpoints adds /health and /ready endpoints to the mux.
func RegisterHealthEndpoints(mux *http.ServeMux) {
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("READY"))
	})
}
