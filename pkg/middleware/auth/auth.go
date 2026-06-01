package auth

import (
	"context"
	"strings"
	"time"

	"p9e.in/samavaya/packages/p9context"
	"p9e.in/samavaya/packages/p9log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// JWTClaims represents the claims extracted from a JWT token.
// This matches the JWT structure defined in the architecture plan.
type JWTClaims struct {
	UserID      string   `json:"user_id"`
	TenantID    string   `json:"tenant_id"`
	CompanyID   string   `json:"company_id"`
	BranchID    string   `json:"branch_id,omitempty"`
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
	SessionID   string   `json:"session_id"`
	ExpiresAt   time.Time
	IssuedAt    time.Time
}

// SessionInfo represents session information from the database.
type SessionInfo struct {
	SessionID   string
	UserID      string
	TenantID    string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	LastActive  time.Time
	IPAddress   string
	UserAgent   string
	IsRevoked   bool
	RevokedAt   *time.Time
	RevokedBy   string
}

// JWTValidator is an interface for validating JWT tokens.
// Implement this interface to integrate with your JWT library.
type JWTValidator interface {
	// ValidateToken validates the JWT token and returns the claims.
	// Returns an error if the token is invalid or expired.
	ValidateToken(ctx context.Context, token string) (*JWTClaims, error)
}

// SessionRepository is an interface for looking up sessions from the database.
// Implement this interface to integrate with your session storage.
type SessionRepository interface {
	// GetSessionByID retrieves an active session by session ID.
	// Returns nil if the session is not found, expired, or revoked.
	GetActiveSession(ctx context.Context, sessionID string) (*SessionInfo, error)
	// UpdateLastAccessed updates the last accessed timestamp for a session.
	UpdateLastAccessed(ctx context.Context, sessionID string) error
}

// AuthMiddleware provides authentication middleware for gRPC services.
type AuthMiddleware struct {
	jwtValidator       JWTValidator
	sessionRepo        SessionRepository
	skipPaths          map[string]struct{}
	updateLastAccessed bool
}

// AuthMiddlewareOption is a functional option for configuring AuthMiddleware.
type AuthMiddlewareOption func(*AuthMiddleware)

// WithSkipPaths sets paths that should skip authentication.
func WithSkipPaths(paths ...string) AuthMiddlewareOption {
	return func(m *AuthMiddleware) {
		for _, path := range paths {
			m.skipPaths[path] = struct{}{}
		}
	}
}

// WithUpdateLastAccessed enables updating the last accessed timestamp on each request.
func WithUpdateLastAccessed(enabled bool) AuthMiddlewareOption {
	return func(m *AuthMiddleware) {
		m.updateLastAccessed = enabled
	}
}

// NewAuthMiddleware creates a new auth middleware with the given JWT validator and session repository.
func NewAuthMiddleware(jwtValidator JWTValidator, sessionRepo SessionRepository, opts ...AuthMiddlewareOption) *AuthMiddleware {
	m := &AuthMiddleware{
		jwtValidator:       jwtValidator,
		sessionRepo:        sessionRepo,
		skipPaths:          make(map[string]struct{}),
		updateLastAccessed: true, // Default to true
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// GrpcAuthMiddleware is a gRPC unary interceptor that handles authentication.
// It validates the JWT token, looks up the session, and enriches the context
// with user and session information.
func (m *AuthMiddleware) GrpcAuthMiddleware(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	// Check if this path should skip authentication
	if _, skip := m.skipPaths[info.FullMethod]; skip {
		return handler(ctx, req)
	}

	// Extract metadata from context
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		p9log.Context(ctx).Warn("auth middleware: no metadata in context")
		return nil, status.Errorf(codes.Unauthenticated, "missing metadata")
	}

	// Extract authorization header
	authHeader := md.Get("authorization")
	if len(authHeader) == 0 {
		p9log.Context(ctx).Warn("auth middleware: no authorization header")
		return nil, status.Errorf(codes.Unauthenticated, "missing authorization header")
	}

	// Parse Bearer token
	token, err := extractBearerToken(authHeader[0])
	if err != nil {
		p9log.Context(ctx).Warnf("auth middleware: invalid authorization header: %v", err)
		return nil, status.Errorf(codes.Unauthenticated, "invalid authorization header")
	}

	// Validate JWT token
	claims, err := m.jwtValidator.ValidateToken(ctx, token)
	if err != nil {
		p9log.Context(ctx).Warnf("auth middleware: token validation failed: %v", err)
		return nil, status.Errorf(codes.Unauthenticated, "invalid token")
	}

	// Validate session from database (if session repository is provided)
	var sessionInfo *SessionInfo
	if m.sessionRepo != nil && claims.SessionID != "" {
		sessionInfo, err = m.sessionRepo.GetActiveSession(ctx, claims.SessionID)
		if err != nil {
			p9log.Context(ctx).Warnf("auth middleware: session lookup failed: %v", err)
			return nil, status.Errorf(codes.Unauthenticated, "session lookup failed")
		}
		if sessionInfo == nil {
			p9log.Context(ctx).Warn("auth middleware: session not found or expired")
			return nil, status.Errorf(codes.Unauthenticated, "session expired or revoked")
		}
		if sessionInfo.IsRevoked {
			p9log.Context(ctx).Warn("auth middleware: session is revoked")
			return nil, status.Errorf(codes.Unauthenticated, "session revoked")
		}

		// Update last accessed timestamp asynchronously
		if m.updateLastAccessed {
			go func() {
				updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := m.sessionRepo.UpdateLastAccessed(updateCtx, claims.SessionID); err != nil {
					p9log.Context(ctx).Warnf("auth middleware: failed to update last accessed: %v", err)
				}
			}()
		}
	}

	// Enrich context with user context
	ctx = p9context.NewUserContext(ctx, p9context.UserContext{
		UserID:      claims.UserID,
		TenantID:    claims.TenantID,
		CompanyID:   claims.CompanyID,
		BranchID:    claims.BranchID,
		Role:        claims.Role,
		Permissions: claims.Permissions,
	})

	// Enrich context with session context (if session info is available)
	if sessionInfo != nil {
		ctx = p9context.NewSessionContext(ctx, p9context.SessionContext{
			SessionID:  sessionInfo.SessionID,
			UserID:     sessionInfo.UserID,
			TenantID:   sessionInfo.TenantID,
			CreatedAt:  sessionInfo.CreatedAt,
			ExpiresAt:  sessionInfo.ExpiresAt,
			LastActive: sessionInfo.LastActive,
			IPAddress:  sessionInfo.IPAddress,
			UserAgent:  sessionInfo.UserAgent,
			IsRevoked:  sessionInfo.IsRevoked,
			RevokedAt:  sessionInfo.RevokedAt,
			RevokedBy:  sessionInfo.RevokedBy,
		})
	}

	p9log.Context(ctx).Debugf("auth middleware: authenticated user %s, tenant %s", claims.UserID, claims.TenantID)

	return handler(ctx, req)
}

// extractBearerToken extracts the token from a Bearer authorization header.
func extractBearerToken(authHeader string) (string, error) {
	const bearerPrefix = "Bearer "
	if !strings.HasPrefix(authHeader, bearerPrefix) {
		return "", status.Errorf(codes.Unauthenticated, "authorization header must use Bearer scheme")
	}
	token := strings.TrimPrefix(authHeader, bearerPrefix)
	if token == "" {
		return "", status.Errorf(codes.Unauthenticated, "bearer token is empty")
	}
	return token, nil
}

// NoOpJWTValidator is a validator that accepts all tokens (for testing/development).
// WARNING: Do not use in production!
type NoOpJWTValidator struct {
	DefaultClaims *JWTClaims
}

// ValidateToken always returns the default claims (for testing only).
func (v *NoOpJWTValidator) ValidateToken(ctx context.Context, token string) (*JWTClaims, error) {
	if v.DefaultClaims != nil {
		return v.DefaultClaims, nil
	}
	return &JWTClaims{
		UserID:    "test-user",
		TenantID:  "test-tenant",
		CompanyID: "test-company",
		Role:      "admin",
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}, nil
}
