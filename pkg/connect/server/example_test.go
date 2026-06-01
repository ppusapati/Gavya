package server_test

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"p9e.in/samavaya/packages/connect/interceptors"
	"p9e.in/samavaya/packages/connect/server"
)

// Example_basicSetup demonstrates basic server setup with all middleware.
func Example_basicSetup() {
	// Assume these are provided via dependency injection
	var dbPool *pgxpool.Pool
	// var authHandler authconnect.AuthServiceHandler

	// Create JWT validator
	jwtValidator := interceptors.NewAuthzJWTValidator()

	// Build middleware configuration
	mwConfig := server.MiddlewareConfig{
		EnableRecovery:  true,
		EnableRequestID: true,
		EnableLogging:   true,
		EnableAuth:      true,
		EnableRLS:       true,
		EnableDB:        true,
		JWTValidator:    jwtValidator,
		DBPool:          dbPool,
		RLSLevel:        interceptors.ScopeLevelBranch,
		SkipAuthProcedures: []string{
			"/auth.v2.AuthService/Login",
			"/auth.v2.AuthService/Register",
			"/health",
			"/ready",
		},
	}

	// Build Connect option with interceptors
	connectOption := server.NewConnectOption(mwConfig)

	// Register service handler (example)
	mux := http.NewServeMux()
	// path, handler := authconnect.NewAuthServiceHandler(authHandler, connectOption)
	// mux.Handle(path, handler)
	_ = connectOption // Use connectOption when registering handlers

	// Add health endpoints
	server.RegisterHealthEndpoints(mux)

	// Wrap with CORS and h2c
	serverConfig := server.DefaultServerConfig("8080")
	handler := server.WrapWithCORS(mux, serverConfig.AllowedOrigins)
	handler = server.WrapWithH2C(handler)

	// Create HTTP server
	httpServer := server.NewHTTPServer(serverConfig, handler)
	_ = httpServer

	// Output:
}

// Example_companyLevelRLS demonstrates using company-level RLS for entities
// like Chart of Accounts or Party that don't have branch scoping.
func Example_companyLevelRLS() {
	var dbPool *pgxpool.Pool
	jwtValidator := interceptors.NewAuthzJWTValidator()

	mwConfig := server.MiddlewareConfig{
		EnableRecovery:  true,
		EnableRequestID: true,
		EnableLogging:   true,
		EnableAuth:      true,
		EnableRLS:       true,
		EnableDB:        true,
		JWTValidator:    jwtValidator,
		DBPool:          dbPool,
		RLSLevel:        interceptors.ScopeLevelCompany, // Company-level for masters
	}

	_ = server.NewConnectOption(mwConfig)

	// Output:
}

// Example_multiTenant demonstrates multi-tenant setup with independent databases.
func Example_multiTenant() {
	var sharedPool *pgxpool.Pool

	// Create pool resolver for multi-tenant setup
	poolConfig := interceptors.IndependentPoolConfig{
		Host:     "localhost",
		Port:     5432,
		User:     "app_user",
		Password: "secret",
		SSLMode:  "disable",
	}
	poolResolver := interceptors.NewSimpleDBPoolResolver(sharedPool, poolConfig)

	// Register some tenants as shared
	poolResolver.RegisterSharedTenant("tenant_small_1")
	poolResolver.RegisterSharedTenant("tenant_small_2")
	// Other tenants will get their own database

	jwtValidator := interceptors.NewAuthzJWTValidator()

	mwConfig := server.MiddlewareConfig{
		EnableRecovery:  true,
		EnableRequestID: true,
		EnableLogging:   true,
		EnableAuth:      true,
		EnableRLS:       true,
		EnableDB:        true,
		JWTValidator:    jwtValidator,
		DBPool:          sharedPool,
		DBPoolResolver:  poolResolver, // Use resolver for multi-tenant
		RLSLevel:        interceptors.ScopeLevelBranch,
	}

	_ = server.NewConnectOption(mwConfig)

	// Output:
}

// Example_minimalSetup demonstrates minimal setup without auth (for public endpoints).
func Example_minimalSetup() {
	var dbPool *pgxpool.Pool

	mwConfig := server.MiddlewareConfig{
		EnableRecovery:  true,
		EnableRequestID: true,
		EnableLogging:   true,
		EnableAuth:      false, // No auth for public service
		EnableRLS:       false, // No RLS without auth
		EnableDB:        true,
		DBPool:          dbPool,
	}

	_ = server.NewConnectOption(mwConfig)

	// Output:
}
