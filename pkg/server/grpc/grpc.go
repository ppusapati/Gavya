package grpc

import (
	"io"
	"net"
	"strconv"

	"p9e.in/samavaya/packages/config"
	"p9e.in/samavaya/packages/middleware/auth"
	"p9e.in/samavaya/packages/middleware/dbmiddleware"
	"p9e.in/samavaya/packages/middleware/localize"
	"p9e.in/samavaya/packages/middleware/requestid"
	"p9e.in/samavaya/packages/middleware/rls"
	"p9e.in/samavaya/packages/middleware/tenant"
	"p9e.in/samavaya/packages/p9log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

type GRPCServer interface {
	Start(serviceRegister func(server *grpc.Server))
	io.Closer
}

type gRPCServer struct {
	grpcServer *grpc.Server
	config     config.GrpcServerConfig
	log        p9log.Helper
}

func NewGrpcServer(config config.GrpcServerConfig, includeTenant bool, log p9log.Helper) (GRPCServer, error) {
	options, err := buildOptions(config, includeTenant)
	if err != nil {
		return nil, err
	}

	server := grpc.NewServer(options...)

	return &gRPCServer{
		config:     config,
		grpcServer: server,
		log:        log,
	}, err
}

// NewGrpcServerWithMiddleware creates a new gRPC server with full middleware configuration.
// This allows fine-grained control over which middlewares are enabled and their configuration.
func NewGrpcServerWithMiddleware(config config.GrpcServerConfig, mwConfig MiddlewareConfig, log p9log.Helper) (GRPCServer, error) {
	options, err := buildOptionsWithMiddleware(config, mwConfig)
	if err != nil {
		return nil, err
	}

	server := grpc.NewServer(options...)

	return &gRPCServer{
		config:     config,
		grpcServer: server,
		log:        log,
	}, err
}

// MiddlewareConfig holds configuration for optional middleware components.
type MiddlewareConfig struct {
	// IncludeTenant enables tenant resolution middleware
	IncludeTenant bool
	// IncludeAuth enables authentication middleware
	IncludeAuth bool
	// IncludeRLS enables RLS scope middleware
	IncludeRLS bool
	// AuthMiddleware is the auth middleware instance (required if IncludeAuth is true)
	AuthMiddleware *auth.AuthMiddleware
	// RLSLevel specifies the RLS scoping level (branch, company, or tenant)
	RLSLevel rls.ScopeLevel
	// SkipAuthPaths are paths that should skip authentication
	SkipAuthPaths []string
}

func buildOptions(config config.GrpcServerConfig, includeTenant bool) ([]grpc.ServerOption, error) {
	return buildOptionsWithMiddleware(config, MiddlewareConfig{
		IncludeTenant: includeTenant,
	})
}

func buildOptionsWithMiddleware(config config.GrpcServerConfig, mwConfig MiddlewareConfig) ([]grpc.ServerOption, error) {
	// Build interceptor chain in the correct order:
	// 1. Request ID (first - for tracing)
	// 2. Localization
	// 3. DB Middleware (resolve database pool)
	// 4. Tenant Middleware (if enabled)
	// 5. Auth Middleware (if enabled)
	// 6. RLS Middleware (if enabled)

	interceptors := []grpc.UnaryServerInterceptor{
		// Request ID generation/extraction (always first for tracing)
		requestid.UnaryServerInterceptor(),
		// Localization middleware
		localize.I18N,
		// Database pool resolution
		dbmiddleware.NewDBResolver(config.DBContext).DbMiddleware,
	}

	// Tenant resolution middleware
	if mwConfig.IncludeTenant {
		interceptors = append(interceptors, tenant.GrpcTenantMiddleware)
	}

	// Authentication middleware
	if mwConfig.IncludeAuth && mwConfig.AuthMiddleware != nil {
		interceptors = append(interceptors, mwConfig.AuthMiddleware.GrpcAuthMiddleware)
	}

	// RLS scope middleware
	if mwConfig.IncludeRLS {
		switch mwConfig.RLSLevel {
		case rls.ScopeLevelBranch:
			interceptors = append(interceptors, rls.BranchLevelInterceptor())
		case rls.ScopeLevelCompany:
			interceptors = append(interceptors, rls.CompanyLevelInterceptor())
		case rls.ScopeLevelTenant:
			interceptors = append(interceptors, rls.TenantLevelInterceptor())
		default:
			// Default to branch level for most granular RLS
			interceptors = append(interceptors, rls.BranchLevelInterceptor())
		}
	}

	return []grpc.ServerOption{
		grpc.KeepaliveParams(buildKeepaliveParams(config.KeepaliveParams)),
		grpc.KeepaliveEnforcementPolicy(buildKeepalivePolicy(config.KeepalivePolicy)),
		grpc.ChainUnaryInterceptor(interceptors...),
	}, nil
}

func buildKeepalivePolicy(config keepalive.EnforcementPolicy) keepalive.EnforcementPolicy {
	return keepalive.EnforcementPolicy{
		MinTime:             config.MinTime,
		PermitWithoutStream: config.PermitWithoutStream,
	}
}

func buildKeepaliveParams(config keepalive.ServerParameters) keepalive.ServerParameters {
	return keepalive.ServerParameters{
		MaxConnectionIdle:     config.MaxConnectionIdle,
		MaxConnectionAge:      config.MaxConnectionAge,
		MaxConnectionAgeGrace: config.MaxConnectionAgeGrace,
		Time:                  config.Time,
		Timeout:               config.Timeout,
	}
}

func (g gRPCServer) Start(serviceRegister func(server *grpc.Server)) {
	grpcListener, err := net.Listen("tcp", ":"+strconv.Itoa(int(g.config.Port)))
	if err != nil {
		g.log.Error("failed to start grpc server", err)
	}

	serviceRegister(g.grpcServer)

	g.log.Info("start grpc server success, Endpoint: ", grpcListener.Addr())
	if err := g.grpcServer.Serve(grpcListener); err != nil {
		g.log.Error("failed to grpc server serve", err)
	}
}

func (g gRPCServer) Close() error {
	g.log.Info("close gRPC server")
	g.grpcServer.GracefulStop()
	return nil
}
