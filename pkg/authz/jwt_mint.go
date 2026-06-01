// jwt_mint.go — HS256 JWT minting helper.
//
// Used by the JWT walkthrough harness to create signed tokens for two
// dev tenants without going through the full auth-service login flow.
// Production callers go through identity/auth's `Login` RPC — this file
// is for tests, smoke harnesses, and dev-mode token issuance.
//
// Mirrors ParseJWT's HS256 + CustomClaims contract.

package authz

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// MintInput bundles the payload + lifetime for a minted token. Permissions
// + Role are propagated verbatim into the claims; ParseJWT round-trips them.
type MintInput struct {
	UserID      string
	TenantID    string
	CompanyID   string
	BranchID    string
	Role        string
	Permissions []Permission
	SessionID   string
	Issuer      string        // optional; falls back to jwtConfig.issuer if empty
	Audience    []string      // optional; embedded in standard `aud` claim
	Subject     string        // optional; falls back to UserID
	Lifetime    time.Duration // mandatory; how long until the token expires
}

// MintHS256 signs a CustomClaims token with the configured HS256 secret.
// Errors if the secret is unconfigured or the signing call itself fails.
func MintHS256(in MintInput) (string, error) {
	secret, err := getJWTSecret()
	if err != nil {
		return "", fmt.Errorf("mint: %w", err)
	}
	if in.Lifetime <= 0 {
		return "", fmt.Errorf("mint: Lifetime must be positive")
	}

	now := time.Now()
	subject := in.Subject
	if subject == "" {
		subject = in.UserID
	}
	issuer := in.Issuer
	if issuer == "" && jwtConfig != nil {
		jwtConfig.mu.RLock()
		issuer = jwtConfig.issuer
		jwtConfig.mu.RUnlock()
	}

	claims := CustomClaims{
		UserID:      in.UserID,
		TenantID:    in.TenantID,
		CompanyID:   in.CompanyID,
		BranchID:    in.BranchID,
		Role:        in.Role,
		Permissions: in.Permissions,
		SessionID:   in.SessionID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			Issuer:    issuer,
			Audience:  jwt.ClaimStrings(in.Audience),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-30 * time.Second)), // small clock-skew tolerance
			ExpiresAt: jwt.NewNumericDate(now.Add(in.Lifetime)),
		},
	}

	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(secret)
	if err != nil {
		return "", fmt.Errorf("mint: sign: %w", err)
	}
	return signed, nil
}

// MintAsymmetric signs a CustomClaims token with the keystore's active
// private key. Algorithm is determined by the keystore's configured
// alg (RS256 or ES256). The token's `kid` header is populated so
// verifiers can find the matching public key in the JWKS document.
//
// LoadKeystoreFromEnv must be called first; otherwise this returns
// ErrJWTNotConfigured.
func MintAsymmetric(in MintInput) (string, error) {
	ks := CurrentKeystore()
	if ks == nil {
		return "", fmt.Errorf("mint asymmetric: keystore not loaded — call LoadKeystoreFromEnv first")
	}
	priv, kid, alg := ks.MintKey()
	if priv == nil {
		return "", fmt.Errorf("mint asymmetric: keystore has no asymmetric mint key — DEPLOY_JWT_ALGORITHM must be RS256 or ES256")
	}
	if in.Lifetime <= 0 {
		return "", fmt.Errorf("mint asymmetric: Lifetime must be positive")
	}

	now := time.Now()
	subject := in.Subject
	if subject == "" {
		subject = in.UserID
	}
	issuer := in.Issuer
	if issuer == "" && jwtConfig != nil {
		jwtConfig.mu.RLock()
		issuer = jwtConfig.issuer
		jwtConfig.mu.RUnlock()
	}

	claims := CustomClaims{
		UserID:      in.UserID,
		TenantID:    in.TenantID,
		CompanyID:   in.CompanyID,
		BranchID:    in.BranchID,
		Role:        in.Role,
		Permissions: in.Permissions,
		SessionID:   in.SessionID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			Issuer:    issuer,
			Audience:  jwt.ClaimStrings(in.Audience),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-30 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(now.Add(in.Lifetime)),
		},
	}

	var method jwt.SigningMethod
	switch alg {
	case "RS256":
		method = jwt.SigningMethodRS256
	case "ES256":
		method = jwt.SigningMethodES256
	default:
		return "", fmt.Errorf("mint asymmetric: unsupported keystore alg %q", alg)
	}

	tok := jwt.NewWithClaims(method, claims)
	tok.Header["kid"] = kid // verifier uses this to find the public key

	signed, err := tok.SignedString(priv)
	if err != nil {
		return "", fmt.Errorf("mint asymmetric: sign: %w", err)
	}
	return signed, nil
}
