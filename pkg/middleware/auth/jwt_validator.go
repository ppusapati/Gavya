package auth

import (
	"context"
	"fmt"
	"time"

	"p9e.in/samavaya/packages/authz"
)

// AuthzJWTValidator implements JWTValidator using the authz package.
// This adapter bridges the middleware's JWTValidator interface with
// the existing authz.ParseJWT implementation.
type AuthzJWTValidator struct{}

// NewAuthzJWTValidator creates a new JWT validator that uses the authz package.
// Ensure authz.InitJWTFromConfig or authz.InitJWTFromEnv is called before using.
func NewAuthzJWTValidator() *AuthzJWTValidator {
	return &AuthzJWTValidator{}
}

// ValidateToken validates the JWT token and returns the claims.
// It uses authz.ParseJWT internally and converts the claims to the middleware format.
func (v *AuthzJWTValidator) ValidateToken(ctx context.Context, token string) (*JWTClaims, error) {
	claims, err := authz.ParseJWT(token)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JWT: %w", err)
	}

	// Project structured permissions to canonical "ns:res:act" wire form
	// via the package's single source of truth — keeps the format aligned
	// with what Login mints and what auth_handler.go projects through
	// ValidateTokenResponse.
	permissions := authz.PermissionsToStrings(claims.Permissions)

	// Convert authz claims to middleware JWTClaims format
	jwtClaims := &JWTClaims{
		UserID:      claims.UserID,
		TenantID:    claims.TenantID,
		CompanyID:   claims.CompanyID,
		BranchID:    claims.BranchID,
		Role:        claims.Role,
		Permissions: permissions,
		SessionID:   claims.SessionID,
	}

	// Set expiration time if available
	if claims.ExpiresAt != nil {
		jwtClaims.ExpiresAt = claims.ExpiresAt.Time
	}

	// Set issued at time if available
	if claims.IssuedAt != nil {
		jwtClaims.IssuedAt = claims.IssuedAt.Time
	} else {
		jwtClaims.IssuedAt = time.Now()
	}

	return jwtClaims, nil
}
