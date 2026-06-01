package authz

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"p9e.in/samavaya/packages/api/v1/config"

	"github.com/golang-jwt/jwt/v5"
)

// CustomClaims defines the structure embedded in JWT
type CustomClaims struct {
	UserID      string       `json:"sub"`
	TenantID    string       `json:"tenant_id"`
	CompanyID   string       `json:"company_id,omitempty"`
	BranchID    string       `json:"branch_id,omitempty"` // User's default branch, may be empty for company-scoped entities
	Role        string       `json:"role"`
	Permissions []Permission `json:"permissions"`
	SessionID   string       `json:"session_id,omitempty"` // Links to database session for revocation support
	jwt.RegisteredClaims
}

// JWTConfig holds JWT configuration
type JWTConfig struct {
	secret []byte
	issuer string
	mu     sync.RWMutex
}

var (
	jwtConfig     *JWTConfig
	jwtConfigOnce sync.Once

	// ErrJWTNotConfigured is returned when JWT secret is not configured
	ErrJWTNotConfigured = errors.New("JWT secret not configured: set JWT_SECRET environment variable or provide config")
)

// InitJWTFromSecret initializes JWT configuration from a literal secret +
// optional issuer. Used by the auth service so its locally-validated
// JWTSecret value (sourced via the auth ConfigModule from JWT_SECRET /
// dev fallback) becomes the shared secret seen by authz.MintHS256 +
// ParseJWT. This guarantees that tokens minted by Login and tokens
// validated by jwtAuthMiddleware use the same key — the previous design
// kept two parallel copies of the secret which silently diverged in dev
// mode (auth service generated a random secret, middleware used JWT_SECRET).
//
// Idempotent and safe to call multiple times — subsequent calls overwrite
// the secret, which is the intended behavior when the auth service starts
// after main.go has already called InitJWTFromEnv. The auth service is
// the authoritative configuration source because it validates the secret
// value upfront via Config.Validate.
//
// Returns an error if secret is empty (matches the no-silent-fallback
// posture of InitJWTFromConfig and InitJWTFromEnv).
func InitJWTFromSecret(secret string, issuer string) error {
	if secret == "" {
		return ErrJWTNotConfigured
	}
	jwtConfigOnce.Do(func() {
		jwtConfig = &JWTConfig{}
	})
	jwtConfig.mu.Lock()
	defer jwtConfig.mu.Unlock()
	jwtConfig.secret = []byte(secret)
	if issuer != "" {
		jwtConfig.issuer = issuer
	}
	return nil
}

// InitJWTFromConfig initializes JWT configuration from the security config.
// This should be called during application startup.
func InitJWTFromConfig(cfg *config.Security) error {
	if cfg == nil || cfg.Jwt == nil || cfg.Jwt.Secret == "" {
		return ErrJWTNotConfigured
	}

	jwtConfigOnce.Do(func() {
		jwtConfig = &JWTConfig{}
	})

	jwtConfig.mu.Lock()
	defer jwtConfig.mu.Unlock()

	jwtConfig.secret = []byte(cfg.Jwt.Secret)
	jwtConfig.issuer = cfg.Jwt.Issuer

	return nil
}

// InitJWTFromEnv initializes JWT configuration from environment variables.
//
// Secret resolution order (first match wins):
//  1. JWT_SECRET_FILE — path to a file whose contents are the secret.
//     Whitespace at the end is trimmed (handles trailing newline from
//     `echo` or `kubectl create secret`). This is the production-shape
//     pattern: Kubernetes secret mounts, Docker secrets, Vault sidecars
//     all materialize as a file path. Avoids leaking the secret via
//     `ps` / `/proc/<pid>/environ` / container metadata that env vars
//     are visible in.
//  2. JWT_SECRET — literal value as an env var. Convenient for local dev
//     and CI; not recommended for production.
//
// JWT_ISSUER (optional) is read directly from the env regardless of which
// secret source is used.
//
// Returns ErrJWTNotConfigured if neither source resolves to a non-empty
// secret. Returns a wrapped error if JWT_SECRET_FILE is set but unreadable.
func InitJWTFromEnv() error {
	secret, err := resolveJWTSecret()
	if err != nil {
		return err
	}

	jwtConfigOnce.Do(func() {
		jwtConfig = &JWTConfig{}
	})

	jwtConfig.mu.Lock()
	defer jwtConfig.mu.Unlock()

	jwtConfig.secret = secret
	jwtConfig.issuer = os.Getenv("JWT_ISSUER")

	return nil
}

// resolveJWTSecret implements the JWT_SECRET_FILE → JWT_SECRET fallback.
// Exposed at package scope so tests can call it directly.
func resolveJWTSecret() ([]byte, error) {
	if path := os.Getenv("JWT_SECRET_FILE"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read JWT_SECRET_FILE %q: %w", path, err)
		}
		// Trim trailing whitespace (\n from `echo`, `\r\n` from Windows,
		// stray spaces) but NOT leading whitespace — a secret might
		// legitimately start with a tab or space.
		trimmed := bytes.TrimRight(data, " \t\r\n")
		if len(trimmed) == 0 {
			return nil, fmt.Errorf("JWT_SECRET_FILE %q is empty", path)
		}
		return trimmed, nil
	}
	if v := os.Getenv("JWT_SECRET"); v != "" {
		return []byte(v), nil
	}
	return nil, ErrJWTNotConfigured
}

// getJWTSecret returns the configured JWT secret.
// It first checks if config was initialized, then falls back to environment variable.
func getJWTSecret() ([]byte, error) {
	if jwtConfig != nil {
		jwtConfig.mu.RLock()
		defer jwtConfig.mu.RUnlock()
		if len(jwtConfig.secret) > 0 {
			return jwtConfig.secret, nil
		}
	}

	// Fallback to environment variable
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return nil, ErrJWTNotConfigured
	}

	return []byte(secret), nil
}

// ParseJWT parses the token and returns claims. Supports HS256, RS256,
// and ES256 — selected per-token from the `alg` header. The `none`
// algorithm is REJECTED unconditionally via jwt.WithValidMethods (the
// historical jwt-library `none` vulnerability).
//
// Algorithm dispatch:
//   - HS256: validates with the HS secret loaded by InitJWTFromEnv
//     (JWT_SECRET / JWT_SECRET_FILE). If no secret configured, HS256
//     tokens are rejected with ErrJWTNotConfigured.
//   - RS256 / ES256: looks up the public key by `kid` header in the
//     keystore loaded by LoadKeystoreFromEnv. If no keystore is loaded
//     OR the kid is unknown, the token is rejected.
//
// kid is required for asymmetric tokens — without it we don't know
// which public key to verify with, and accepting any in the keystore
// would let an attacker downgrade RS256 → HS256 by claiming the
// public-key bytes are the HS secret. Enforcing kid blocks that.
func ParseJWT(tokenString string) (*CustomClaims, error) {
	keyFunc := func(token *jwt.Token) (interface{}, error) {
		switch token.Method.(type) {
		case *jwt.SigningMethodHMAC:
			secret, err := getJWTSecret()
			if err != nil {
				return nil, fmt.Errorf("HS256 secret unconfigured: %w", err)
			}
			return secret, nil
		case *jwt.SigningMethodRSA, *jwt.SigningMethodECDSA:
			ks := CurrentKeystore()
			if ks == nil {
				return nil, errors.New("asymmetric token but keystore not loaded — call LoadKeystoreFromEnv first")
			}
			kidVal, ok := token.Header["kid"]
			if !ok {
				return nil, errors.New("asymmetric token missing required kid header")
			}
			kid, ok := kidVal.(string)
			if !ok || kid == "" {
				return nil, errors.New("asymmetric token kid header is not a non-empty string")
			}
			pub, _, ok := ks.VerifyKey(kid)
			if !ok {
				return nil, fmt.Errorf("unknown kid %q (not in keystore)", kid)
			}
			return pub, nil
		default:
			return nil, fmt.Errorf("unsupported signing method: %v", token.Header["alg"])
		}
	}

	// jwt.WithValidMethods is the explicit allowlist. golang-jwt would
	// otherwise accept ANY method the keyFunc returns a key for —
	// including the catastrophic `none` algorithm. By passing only the
	// three we support, `none` and `HS512`/`RS512`/etc. all reject at
	// the parser layer before keyFunc ever runs.
	token, err := jwt.ParseWithClaims(
		tokenString,
		&CustomClaims{},
		keyFunc,
		jwt.WithValidMethods([]string{"HS256", "RS256", "ES256"}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	claims, ok := token.Claims.(*CustomClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token or claims")
	}

	if claims.ExpiresAt != nil && claims.ExpiresAt.Time.Before(time.Now()) {
		return nil, errors.New("token expired")
	}

	return claims, nil
}
