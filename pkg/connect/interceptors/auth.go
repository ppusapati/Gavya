package interceptors

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect"

	"p9e.in/samavaya/packages/p9context"
	"p9e.in/samavaya/packages/p9log"
)

// JWTClaims represents the claims extracted from a JWT token.
type JWTClaims struct {
	UserID      string
	TenantID    string
	CompanyID   string
	BranchID    string
	Role        string
	Permissions []string
	SessionID   string
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
type JWTValidator interface {
	ValidateToken(ctx context.Context, token string) (*JWTClaims, error)
}

// SessionRepository is an interface for looking up sessions from the database.
type SessionRepository interface {
	GetActiveSession(ctx context.Context, sessionID string) (*SessionInfo, error)
	UpdateLastAccessed(ctx context.Context, sessionID string) error
}

// AuthInterceptorOption configures the Auth interceptor.
type AuthInterceptorOption func(*authConfig)

type authConfig struct {
	skipProcedures     map[string]struct{}
	sessionRepo        SessionRepository
	updateLastAccessed bool
}

// WithSkipProcedures sets procedures that should skip authentication.
func WithSkipProcedures(procedures ...string) AuthInterceptorOption {
	return func(c *authConfig) {
		for _, p := range procedures {
			c.skipProcedures[p] = struct{}{}
		}
	}
}

// WithSessionRepository sets the session repository for session validation.
func WithSessionRepository(repo SessionRepository) AuthInterceptorOption {
	return func(c *authConfig) {
		c.sessionRepo = repo
	}
}

// WithUpdateLastAccessed enables/disables updating the last accessed timestamp.
func WithUpdateLastAccessed(enabled bool) AuthInterceptorOption {
	return func(c *authConfig) {
		c.updateLastAccessed = enabled
	}
}

// AuthInterceptor returns a Connect interceptor that handles authentication.
// It validates the JWT token, optionally looks up the session, and enriches
// the context with user and session information.
func AuthInterceptor(jwtValidator JWTValidator, opts ...AuthInterceptorOption) connect.UnaryInterceptorFunc {
	cfg := &authConfig{
		skipProcedures:     make(map[string]struct{}),
		updateLastAccessed: true,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			procedure := req.Spec().Procedure

			// Check if this procedure should skip authentication
			if _, skip := cfg.skipProcedures[procedure]; skip {
				return next(ctx, req)
			}

			// Extract authorization header
			authHeader := req.Header().Get("Authorization")
			if authHeader == "" {
				p9log.Context(ctx).Warn("auth interceptor: no authorization header")
				return nil, connect.NewError(connect.CodeUnauthenticated, nil)
			}

			// Parse Bearer token
			token, err := extractBearerToken(authHeader)
			if err != nil {
				p9log.Context(ctx).Warnf("auth interceptor: invalid authorization header: %v", err)
				return nil, connect.NewError(connect.CodeUnauthenticated, nil)
			}

			// Validate JWT token
			claims, err := jwtValidator.ValidateToken(ctx, token)
			if err != nil {
				p9log.Context(ctx).Warnf("auth interceptor: token validation failed: %v", err)
				return nil, connect.NewError(connect.CodeUnauthenticated, nil)
			}

			// Validate session from database (if session repository is provided)
			var sessionInfo *SessionInfo
			if cfg.sessionRepo != nil && claims.SessionID != "" {
				sessionInfo, err = cfg.sessionRepo.GetActiveSession(ctx, claims.SessionID)
				if err != nil {
					p9log.Context(ctx).Warnf("auth interceptor: session lookup failed: %v", err)
					return nil, connect.NewError(connect.CodeUnauthenticated, nil)
				}
				if sessionInfo == nil {
					p9log.Context(ctx).Warn("auth interceptor: session not found or expired")
					return nil, connect.NewError(connect.CodeUnauthenticated, nil)
				}
				if sessionInfo.IsRevoked {
					p9log.Context(ctx).Warn("auth interceptor: session is revoked")
					return nil, connect.NewError(connect.CodeUnauthenticated, nil)
				}

				// Update last accessed timestamp asynchronously
				if cfg.updateLastAccessed {
					go func() {
						updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						defer cancel()
						if err := cfg.sessionRepo.UpdateLastAccessed(updateCtx, claims.SessionID); err != nil {
							p9log.Context(ctx).Warnf("auth interceptor: failed to update last accessed: %v", err)
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

			p9log.Context(ctx).Debugf("auth interceptor: authenticated user %s, tenant %s",
				claims.UserID, claims.TenantID)

			return next(ctx, req)
		}
	}
}

// extractBearerToken extracts the token from a Bearer authorization header.
func extractBearerToken(authHeader string) (string, error) {
	const bearerPrefix = "Bearer "
	if !strings.HasPrefix(authHeader, bearerPrefix) {
		return "", connect.NewError(connect.CodeUnauthenticated, nil)
	}
	token := strings.TrimPrefix(authHeader, bearerPrefix)
	if token == "" {
		return "", connect.NewError(connect.CodeUnauthenticated, nil)
	}
	return token, nil
}
