package dbmiddleware

import (
	"fmt"
	"os"
	"regexp"
	"sync"
	"unicode"

	"p9e.in/samavaya/packages/api/v1/config"
	"p9e.in/samavaya/packages/database/pgxpostgres"
	"p9e.in/samavaya/packages/p9context"
	"p9e.in/samavaya/packages/p9log"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TenantDBConfig holds configuration for tenant database connections
type TenantDBConfig struct {
	// SharedTenants is a set of tenant IDs that use the shared database
	SharedTenants map[string]struct{}
	// IndependentDBConfig holds database config for independent tenant databases
	IndependentDBConfig *config.Data_Postgres
	mu                  sync.RWMutex
}

// DBResolver manages database connection resolution for multi-tenant environments
type DBResolver struct {
	dbContext    *pgxpostgres.DBContext
	tenantConfig *TenantDBConfig
	mu           sync.RWMutex
}

var (
	// tenantIDPattern validates tenant IDs: alphanumeric, underscores, hyphens only
	tenantIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

	// Maximum tenant ID length to prevent abuse
	maxTenantIDLength = 64
)

// NewDBResolver creates a new gRPC server with the given DBContext
func NewDBResolver(dbContext *pgxpostgres.DBContext) *DBResolver {
	return &DBResolver{
		dbContext: dbContext,
		tenantConfig: &TenantDBConfig{
			SharedTenants: make(map[string]struct{}),
		},
	}
}

// NewDBResolverWithConfig creates a DBResolver with explicit tenant configuration
func NewDBResolverWithConfig(dbContext *pgxpostgres.DBContext, cfg *TenantDBConfig) *DBResolver {
	if cfg == nil {
		cfg = &TenantDBConfig{
			SharedTenants: make(map[string]struct{}),
		}
	}
	return &DBResolver{
		dbContext:    dbContext,
		tenantConfig: cfg,
	}
}

// SetIndependentDBConfig sets the database configuration for independent tenant databases
func (s *DBResolver) SetIndependentDBConfig(cfg *config.Data_Postgres) {
	s.tenantConfig.mu.Lock()
	defer s.tenantConfig.mu.Unlock()
	s.tenantConfig.IndependentDBConfig = cfg
}

// RegisterSharedTenant marks a tenant as using the shared database
func (s *DBResolver) RegisterSharedTenant(tenantID string) {
	s.tenantConfig.mu.Lock()
	defer s.tenantConfig.mu.Unlock()
	s.tenantConfig.SharedTenants[tenantID] = struct{}{}
}

// RegisterIndependentTenant marks a tenant as using an independent database
func (s *DBResolver) RegisterIndependentTenant(tenantID string) {
	s.tenantConfig.mu.Lock()
	defer s.tenantConfig.mu.Unlock()
	delete(s.tenantConfig.SharedTenants, tenantID)
}

// DbMiddleware is a gRPC interceptor that resolves the appropriate database pool for the tenant.
// It stores the resolved pool in context using p9context.NewDBPoolContext for thread-safe access.
// Legacy behavior: also sets s.dbContext.DBPool for backward compatibility.
func (s *DBResolver) DbMiddleware(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	if s.dbContext == nil {
		return nil, status.Errorf(codes.Internal, "dbContext is nil")
	}

	// Extract tenantID from the gRPC metadata
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Errorf(codes.Internal, "failed to get metadata")
	}

	tenantID := ""
	if values := md.Get("x-tenant-name"); len(values) > 0 {
		tenantID = values[0]
	}

	// Validate and sanitize the tenantID to prevent SQL injection or other security issues
	sanitizedTenantID, err := sanitizeTenantID(tenantID)
	if err != nil {
		p9log.Context(ctx).Warnf("invalid tenant ID rejected: %s, error: %v", tenantID, err)
		return nil, status.Errorf(codes.InvalidArgument, "invalid tenant ID: %v", err)
	}

	// Determine whether the tenant uses the shared database or an independent one
	var dbPool *pgxpool.Pool
	if s.isSharedDatabaseTenant(sanitizedTenantID) {
		dbPool = s.dbContext.DBPoolShared
	} else {
		// If the independent database pool for the tenant doesn't exist, create it
		s.mu.Lock()
		if _, exists := s.dbContext.DBPoolIndependent[sanitizedTenantID]; !exists {
			dbConnectionString, err := s.constructIndependentDBConnectionString(sanitizedTenantID)
			if err != nil {
				s.mu.Unlock()
				p9log.Context(ctx).Errorf("failed to construct connection string for tenant %s: %v", sanitizedTenantID, err)
				return nil, status.Errorf(codes.Internal, "failed to configure database connection")
			}

			independentDBPool, err := pgxpool.New(ctx, dbConnectionString)
			if err != nil {
				s.mu.Unlock()
				p9log.Context(ctx).Errorf("failed to connect to independent database for tenant %s: %v", sanitizedTenantID, err)
				return nil, status.Errorf(codes.Internal, "failed to connect to the independent database")
			}
			s.dbContext.DBPoolIndependent[sanitizedTenantID] = independentDBPool
		}
		dbPool = s.dbContext.DBPoolIndependent[sanitizedTenantID]
		s.mu.Unlock()
	}

	// Store the resolved pool in context (preferred approach for thread-safety)
	ctx = p9context.NewDBPoolContext(ctx, dbPool)

	// Legacy: Set the DBPool in the DBContext for backward compatibility
	// Note: This is not thread-safe for concurrent requests. Prefer using p9context.DBPool(ctx).
	s.dbContext.DBPool = dbPool

	// Call the next handler in the chain
	return handler(ctx, req)
}

// sanitizeTenantID validates and sanitizes the tenantID to prevent SQL injection and other security issues
func sanitizeTenantID(tenantID string) (string, error) {
	// Empty tenant ID is valid (will use shared database)
	if tenantID == "" {
		return "", nil
	}

	// Check length
	if len(tenantID) > maxTenantIDLength {
		return "", fmt.Errorf("tenant ID exceeds maximum length of %d characters", maxTenantIDLength)
	}

	// Check for valid characters only (alphanumeric, underscore, hyphen)
	if !tenantIDPattern.MatchString(tenantID) {
		return "", fmt.Errorf("tenant ID contains invalid characters (only alphanumeric, underscore, and hyphen allowed)")
	}

	// Check for SQL injection patterns
	if containsSQLInjectionPatterns(tenantID) {
		return "", fmt.Errorf("tenant ID contains potentially dangerous patterns")
	}

	return tenantID, nil
}

// containsSQLInjectionPatterns checks for common SQL injection patterns
func containsSQLInjectionPatterns(s string) bool {
	// Since we already validate against alphanumeric pattern, this is defense in depth
	dangerousPatterns := []string{
		"--", ";", "'", "\"", "/*", "*/", "xp_", "sp_",
		"drop", "delete", "insert", "update", "select",
		"union", "exec", "execute",
	}

	lower := toLowerASCII(s)
	for _, pattern := range dangerousPatterns {
		if containsSubstring(lower, pattern) {
			return true
		}
	}
	return false
}

// toLowerASCII converts ASCII characters to lowercase without allocating for simple cases
func toLowerASCII(s string) string {
	hasUpper := false
	for i := 0; i < len(s); i++ {
		if 'A' <= s[i] && s[i] <= 'Z' {
			hasUpper = true
			break
		}
	}
	if !hasUpper {
		return s
	}

	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

// containsSubstring checks if s contains substr (simple implementation to avoid import)
func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// isSharedDatabaseTenant determines whether the tenant uses the shared database
func (s *DBResolver) isSharedDatabaseTenant(tenantID string) bool {
	// Empty tenant ID uses shared database
	if tenantID == "" {
		return true
	}

	s.tenantConfig.mu.RLock()
	defer s.tenantConfig.mu.RUnlock()

	// Check if explicitly marked as shared
	_, isShared := s.tenantConfig.SharedTenants[tenantID]

	// If SharedTenants is empty, default to shared database for all tenants
	// This maintains backward compatibility
	if len(s.tenantConfig.SharedTenants) == 0 {
		return true
	}

	return isShared
}

// constructIndependentDBConnectionString constructs the database connection string for independent tenant databases
func (s *DBResolver) constructIndependentDBConnectionString(tenantID string) (string, error) {
	s.tenantConfig.mu.RLock()
	cfg := s.tenantConfig.IndependentDBConfig
	s.tenantConfig.mu.RUnlock()

	// Try to get credentials from config first
	if cfg != nil {
		// Validate tenant ID contains only safe characters for database name
		for _, r := range tenantID {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
				return "", fmt.Errorf("invalid character in tenant ID for database name: %c", r)
			}
		}

		sslMode := "disable"
		if cfg.Sslmode {
			sslMode = "require"
		}

		return fmt.Sprintf(
			"user=%s password=%s host=%s port=%d dbname=%s sslmode=%s",
			cfg.User,
			cfg.Password,
			cfg.Host,
			cfg.Port,
			tenantID, // Use tenant ID as database name
			sslMode,
		), nil
	}

	// Fallback to environment variables
	user := os.Getenv("TENANT_DB_USER")
	password := os.Getenv("TENANT_DB_PASSWORD")
	host := os.Getenv("TENANT_DB_HOST")
	port := os.Getenv("TENANT_DB_PORT")
	sslMode := os.Getenv("TENANT_DB_SSLMODE")

	if user == "" || password == "" || host == "" {
		return "", fmt.Errorf("independent database configuration not provided: set TENANT_DB_USER, TENANT_DB_PASSWORD, TENANT_DB_HOST environment variables or provide config")
	}

	if port == "" {
		port = "5432"
	}
	if sslMode == "" {
		sslMode = "disable"
	}

	return fmt.Sprintf(
		"user=%s password=%s host=%s port=%s dbname=%s sslmode=%s",
		user,
		password,
		host,
		port,
		tenantID,
		sslMode,
	), nil
}
