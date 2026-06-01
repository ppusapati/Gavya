// Package authz — asymmetric JWT keystore for RS256/ES256.
//
// HS256 (the original path) uses a shared symmetric secret loaded from
// JWT_SECRET or JWT_SECRET_FILE. That works inside one trust boundary
// but cannot be safely shared with a browser or third-party service.
//
// RS256/ES256 introduces an asymmetric keypair: the private key signs,
// the public key verifies. The public key can ship to anyone (frontend
// SPA, mobile app, partner webhook) without compromising the ability
// to mint. JWKS — the standard JSON Web Key Set format — is how
// verifiers fetch the public keys.
//
// This file provides:
//
//   - Keystore: holds the active mint key + zero-or-more verify keys.
//     Each key has a stable kid (key id) computed as the base64url
//     SHA-256 hash of its DER-encoded SubjectPublicKeyInfo.
//   - LoadKeystoreFromEnv: reads JWT_PRIVATE_KEY_FILE +
//     JWT_PUBLIC_KEYS_FILE; auto-generates a dev keypair to
//     /tmp/jwt-keys/dev-<alg>.pem when neither is set.
//   - JWKS: serializes public keys to the standard JWK shape so a
//     /.well-known/jwks.json endpoint can publish them.
//
// Rotation model: JWT_PUBLIC_KEYS_FILE may concatenate multiple PEM
// public keys. Verification accepts any of them (matched by kid in the
// token header). Mint uses ONLY the private key from JWT_PRIVATE_KEY_FILE.
// To rotate: (1) mint a new keypair, (2) prepend its public key to
// JWT_PUBLIC_KEYS_FILE, (3) deploy — old tokens still validate against
// the old public key, new tokens are minted with the new private key,
// (4) when all old tokens expire, remove the old public key from the
// file.

package authz

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Keystore holds the symmetric secret (HS256) and asymmetric keys
// (RS256/ES256) that ParseJWT and Mint* consult. Exactly one mint key
// is active at a time; verification accepts any matching kid.
type Keystore struct {
	mu sync.RWMutex

	// HS256 secret — populated from JWT_SECRET / JWT_SECRET_FILE via
	// the existing InitJWTFromEnv flow. Kept here so a single Keystore
	// covers all three algorithms.
	hsSecret []byte

	// Active mint key. nil if HS256 is the configured algorithm.
	mintKey    any    // *rsa.PrivateKey | *ecdsa.PrivateKey
	mintKID    string // kid header included on every minted asymmetric token
	mintAlg    string // "RS256" or "ES256"

	// Verify keys, keyed by kid. Includes the mint key's public half PLUS
	// any extra public keys for rotation grace.
	verifyKeys map[string]verifyKey
}

type verifyKey struct {
	kid    string
	alg    string // "RS256" or "ES256"
	pub    any    // *rsa.PublicKey | *ecdsa.PublicKey
}

// global keystore instance; populated by LoadKeystoreFromEnv. Read by
// ParseJWT + MintRS256/MintES256.
var (
	keystore     *Keystore
	keystoreOnce sync.Once
)

// LoadKeystoreFromEnv reads the keystore configuration from environment
// variables. Idempotent — first call wins, subsequent calls return the
// existing keystore.
//
// Algorithm selection:
//   - DEPLOY_JWT_ALGORITHM=HS256 (default) — only the symmetric secret
//     matters; no asymmetric keys loaded
//   - DEPLOY_JWT_ALGORITHM=RS256 — RSA private key required for mint;
//     auto-generate to /tmp/jwt-keys/dev-rs256.pem if no key file set
//   - DEPLOY_JWT_ALGORITHM=ES256 — ECDSA P-256 private key required;
//     auto-generate analogously
//
// Asymmetric keys come from:
//   - JWT_PRIVATE_KEY_FILE — single PEM-encoded private key (PKCS#8
//     preferred; PKCS#1 RSA also accepted for compatibility)
//   - JWT_PUBLIC_KEYS_FILE — one or more PEM-encoded public keys
//     concatenated; if unset, the public half is derived from the
//     private key
//
// Returns the loaded *Keystore or an error if a non-default algorithm
// was requested but key files are missing/corrupt.
func LoadKeystoreFromEnv() (*Keystore, error) {
	var loadErr error
	keystoreOnce.Do(func() {
		keystore, loadErr = buildKeystoreFromEnv()
	})
	if loadErr != nil {
		return nil, loadErr
	}
	return keystore, nil
}

// CurrentKeystore returns the keystore initialized by LoadKeystoreFromEnv,
// or nil if it hasn't been called yet. ParseJWT consults this when a token
// uses RS256/ES256.
func CurrentKeystore() *Keystore { return keystore }

func buildKeystoreFromEnv() (*Keystore, error) {
	ks := &Keystore{verifyKeys: make(map[string]verifyKey)}

	// HS256 secret — best-effort. If neither env is set, the keystore
	// just has no symmetric secret; ParseJWT fails any HS256 token.
	if secret, err := resolveJWTSecret(); err == nil {
		ks.hsSecret = secret
	} else if !errors.Is(err, ErrJWTNotConfigured) {
		// A real read error (file unreadable) — surface it.
		return nil, fmt.Errorf("hs256 secret: %w", err)
	}

	alg := os.Getenv("DEPLOY_JWT_ALGORITHM")
	if alg == "" {
		alg = "HS256"
	}
	switch alg {
	case "HS256":
		// Done — no asymmetric keys needed.
		return ks, nil
	case "RS256", "ES256":
		// fall through to asymmetric loading
	default:
		return nil, fmt.Errorf("unknown DEPLOY_JWT_ALGORITHM=%q (want HS256|RS256|ES256)", alg)
	}

	// Asymmetric path: need a private key for minting.
	privPath := os.Getenv("JWT_PRIVATE_KEY_FILE")
	if privPath == "" {
		// Auto-generate a dev keypair so local boot works without
		// pre-baked key files. NEVER auto-generate in production —
		// the dev key is written to /tmp where any local user can read.
		// Production deploys MUST set JWT_PRIVATE_KEY_FILE explicitly.
		generated, err := autogenerateDevKeypair(alg)
		if err != nil {
			return nil, fmt.Errorf("auto-generate dev keypair: %w", err)
		}
		privPath = generated
	}

	priv, err := loadPrivateKeyFile(privPath)
	if err != nil {
		return nil, fmt.Errorf("load JWT_PRIVATE_KEY_FILE %q: %w", privPath, err)
	}
	if err := assertAlgMatches(priv, alg); err != nil {
		return nil, err
	}
	mintKID, err := computeKID(publicKeyOf(priv))
	if err != nil {
		return nil, fmt.Errorf("compute kid: %w", err)
	}
	ks.mintKey = priv
	ks.mintKID = mintKID
	ks.mintAlg = alg
	// Always include the mint key's public half in the verify set —
	// the same process must validate its own freshly-minted tokens.
	ks.verifyKeys[mintKID] = verifyKey{kid: mintKID, alg: alg, pub: publicKeyOf(priv)}

	// Optional: extra public keys for rotation grace window.
	if pubPath := os.Getenv("JWT_PUBLIC_KEYS_FILE"); pubPath != "" {
		pubs, err := loadPublicKeyFile(pubPath)
		if err != nil {
			return nil, fmt.Errorf("load JWT_PUBLIC_KEYS_FILE %q: %w", pubPath, err)
		}
		for _, pub := range pubs {
			kid, err := computeKID(pub)
			if err != nil {
				return nil, fmt.Errorf("compute kid for public key: %w", err)
			}
			pubAlg := algForPublicKey(pub)
			if pubAlg == "" {
				return nil, fmt.Errorf("unsupported public key type %T in JWT_PUBLIC_KEYS_FILE", pub)
			}
			ks.verifyKeys[kid] = verifyKey{kid: kid, alg: pubAlg, pub: pub}
		}
	}

	return ks, nil
}

// HSSecret returns the symmetric secret (or nil if none configured).
func (k *Keystore) HSSecret() []byte {
	if k == nil {
		return nil
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.hsSecret
}

// MintKey returns the active private key + kid + alg for signing new
// tokens. Returns nil priv if HS256 is the configured algorithm.
func (k *Keystore) MintKey() (priv any, kid, alg string) {
	if k == nil {
		return nil, "", ""
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.mintKey, k.mintKID, k.mintAlg
}

// VerifyKey looks up a public key by kid. Returns (nil, "", false) if
// the kid is unknown.
func (k *Keystore) VerifyKey(kid string) (pub any, alg string, ok bool) {
	if k == nil {
		return nil, "", false
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	v, ok := k.verifyKeys[kid]
	if !ok {
		return nil, "", false
	}
	return v.pub, v.alg, true
}

// VerifyKeys returns a snapshot of all verify keys. Order is not
// deterministic. Used by the JWKS endpoint to list every published key.
func (k *Keystore) VerifyKeys() []VerifyKeyInfo {
	if k == nil {
		return nil
	}
	k.mu.RLock()
	defer k.mu.RUnlock()
	out := make([]VerifyKeyInfo, 0, len(k.verifyKeys))
	for _, v := range k.verifyKeys {
		out = append(out, VerifyKeyInfo{KID: v.kid, Alg: v.alg, Pub: v.pub})
	}
	return out
}

// VerifyKeyInfo is the public-facing record for a verify key — what the
// JWKS endpoint emits.
type VerifyKeyInfo struct {
	KID string
	Alg string
	Pub any // *rsa.PublicKey | *ecdsa.PublicKey
}

// ============================================================================
// Internals: PEM parsing, keypair generation, kid computation
// ============================================================================

func loadPrivateKeyFile(path string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	switch block.Type {
	case "PRIVATE KEY":
		// PKCS#8 — covers both RSA and ECDSA
		return x509.ParsePKCS8PrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		return x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("unsupported PEM type %q", block.Type)
	}
}

func loadPublicKeyFile(path string) ([]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var keys []any
	rest := data
	for {
		block, remainder := pem.Decode(rest)
		if block == nil {
			break
		}
		rest = remainder
		switch block.Type {
		case "PUBLIC KEY":
			pub, err := x509.ParsePKIXPublicKey(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("parse PUBLIC KEY: %w", err)
			}
			keys = append(keys, pub)
		case "RSA PUBLIC KEY":
			pub, err := x509.ParsePKCS1PublicKey(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("parse RSA PUBLIC KEY: %w", err)
			}
			keys = append(keys, pub)
		default:
			return nil, fmt.Errorf("unsupported public key PEM type %q", block.Type)
		}
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("no public keys found in file")
	}
	return keys, nil
}

func publicKeyOf(priv any) any {
	switch p := priv.(type) {
	case *rsa.PrivateKey:
		return &p.PublicKey
	case *ecdsa.PrivateKey:
		return &p.PublicKey
	}
	return nil
}

func algForPublicKey(pub any) string {
	switch pub.(type) {
	case *rsa.PublicKey:
		return "RS256"
	case *ecdsa.PublicKey:
		return "ES256"
	}
	return ""
}

func assertAlgMatches(priv any, alg string) error {
	switch priv.(type) {
	case *rsa.PrivateKey:
		if alg != "RS256" {
			return fmt.Errorf("DEPLOY_JWT_ALGORITHM=%s but private key is RSA — set DEPLOY_JWT_ALGORITHM=RS256 or change the key", alg)
		}
	case *ecdsa.PrivateKey:
		if alg != "ES256" {
			return fmt.Errorf("DEPLOY_JWT_ALGORITHM=%s but private key is ECDSA — set DEPLOY_JWT_ALGORITHM=ES256 or change the key", alg)
		}
	default:
		return fmt.Errorf("unsupported private key type %T", priv)
	}
	return nil
}

// computeKID hashes the DER-encoded SubjectPublicKeyInfo and returns the
// first 16 bytes base64url-encoded. Stable across restarts as long as the
// key is the same. Truncated to keep token headers small.
func computeKID(pub any) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(der)
	return base64.RawURLEncoding.EncodeToString(sum[:16]), nil
}

// autogenerateDevKeypair generates a dev keypair (RSA-2048 or
// ECDSA-P256) and writes it to /tmp/jwt-keys/dev-<alg>.pem. Returns
// the path. If a key already exists at that path, returns it without
// regenerating — so the kid stays stable across dev reboots.
//
// **NEVER USE IN PRODUCTION.** /tmp is world-readable; any local user
// can mint admin tokens. Production deployments MUST set
// JWT_PRIVATE_KEY_FILE explicitly to a key in a private path.
func autogenerateDevKeypair(alg string) (string, error) {
	dir := filepath.Join(os.TempDir(), "jwt-keys")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("dev-%s.pem", alg))
	if _, err := os.Stat(path); err == nil {
		// Already generated on a previous boot — reuse so the kid stays stable.
		return path, nil
	}

	var pemBlock *pem.Block
	switch alg {
	case "RS256":
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return "", err
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return "", err
		}
		pemBlock = &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	case "ES256":
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return "", err
		}
		der, err := x509.MarshalPKCS8PrivateKey(key)
		if err != nil {
			return "", err
		}
		pemBlock = &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	default:
		return "", fmt.Errorf("autogenerate: unknown alg %q", alg)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := pem.Encode(f, pemBlock); err != nil {
		return "", err
	}
	// Stamp the file with the issue time as a comment for ops visibility.
	fmt.Fprintf(f, "# Auto-generated dev key for DEPLOY_JWT_ALGORITHM=%s at %s\n", alg, time.Now().UTC().Format(time.RFC3339))
	return path, nil
}

// ============================================================================
// JWKS serialization — the wire format the /.well-known/jwks.json endpoint emits
// ============================================================================

// JWK is a single key in JWKS format, per RFC 7517.
type JWK struct {
	Kty string `json:"kty"`           // "RSA" or "EC"
	Alg string `json:"alg"`           // "RS256" or "ES256"
	Use string `json:"use"`           // always "sig" — these keys are for signature verification
	Kid string `json:"kid"`           // base64url SHA-256 prefix
	N   string `json:"n,omitempty"`   // RSA modulus (base64url)
	E   string `json:"e,omitempty"`   // RSA public exponent (base64url)
	Crv string `json:"crv,omitempty"` // ECDSA curve, e.g. "P-256"
	X   string `json:"x,omitempty"`   // ECDSA x coordinate (base64url)
	Y   string `json:"y,omitempty"`   // ECDSA y coordinate (base64url)
}

// JWKS is the wrapper { "keys": [...] } that /.well-known/jwks.json serves.
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// MarshalJWKS builds the JWKS document from the keystore's verify keys.
// HS256 secrets are NOT included — they're symmetric and must never leave
// the process.
func (k *Keystore) MarshalJWKS() JWKS {
	out := JWKS{Keys: []JWK{}}
	if k == nil {
		return out
	}
	for _, info := range k.VerifyKeys() {
		jwk, ok := publicKeyToJWK(info.Pub, info.Alg, info.KID)
		if !ok {
			continue
		}
		out.Keys = append(out.Keys, jwk)
	}
	return out
}

func publicKeyToJWK(pub any, alg, kid string) (JWK, bool) {
	switch p := pub.(type) {
	case *rsa.PublicKey:
		return JWK{
			Kty: "RSA",
			Alg: alg,
			Use: "sig",
			Kid: kid,
			N:   base64.RawURLEncoding.EncodeToString(p.N.Bytes()),
			E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(p.E)).Bytes()),
		}, true
	case *ecdsa.PublicKey:
		curve := ""
		switch p.Curve {
		case elliptic.P256():
			curve = "P-256"
		case elliptic.P384():
			curve = "P-384"
		case elliptic.P521():
			curve = "P-521"
		default:
			return JWK{}, false
		}
		// Pad x/y to the curve's byte size (per RFC 7518 §6.2.1.2/3 —
		// fixed-width encoding).
		byteSize := (p.Curve.Params().BitSize + 7) / 8
		xBytes := leftPad(p.X.Bytes(), byteSize)
		yBytes := leftPad(p.Y.Bytes(), byteSize)
		return JWK{
			Kty: "EC",
			Alg: alg,
			Use: "sig",
			Kid: kid,
			Crv: curve,
			X:   base64.RawURLEncoding.EncodeToString(xBytes),
			Y:   base64.RawURLEncoding.EncodeToString(yBytes),
		}, true
	}
	return JWK{}, false
}

func leftPad(b []byte, n int) []byte {
	if len(b) >= n {
		return b
	}
	out := make([]byte, n)
	copy(out[n-len(b):], b)
	return out
}
