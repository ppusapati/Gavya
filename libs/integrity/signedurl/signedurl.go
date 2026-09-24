// Package signedurl makes a link that stands on its own.
//
// Every other way into this platform carries a session: the gateway verifies
// it, establishes the tenant, and stamps the request. That works for a console
// making calls and does not work for a link — a browser following an <a href>
// sends no Authorization header, and neither does curl, a spreadsheet's web
// query, or whoever the link was forwarded to.
//
// So the link carries its own authority. A token names what it is for, which
// tenant it belongs to, which one thing it may fetch and when it stops working,
// and it is signed so that none of those can be changed by whoever holds it.
//
// # WHAT A SIGNED URL ACTUALLY IS
//
// A bearer credential in a place that gets written down. It appears in browser
// history, in a proxy's access log, in the Referer header of whatever the
// download page links to next, and in the chat message somebody pastes it into.
// Signing stops it being forged or widened; it does not stop it being copied,
// and nothing can.
//
// That is why the three bindings below are not optional and why the expiry is
// short by default:
//
//   - Purpose. A token for a report cannot fetch a file. Without this, two
//     services sharing a key would each honour the other's tokens, and the
//     narrower permission would be the one that decided nothing.
//   - Tenant. The tenant comes out of the verified token and is never read from
//     a header on a signed request — there is no session to have established
//     one, and a header on such a request is whatever the caller typed.
//   - Resource. One identifier, compared against what is served. A token that
//     named a kind of thing rather than a thing would be a key to the whole
//     table.
//
// # WHAT IS NOT HERE
//
// No presigned S3 or GCS URL. This platform has no object storage, and a
// presigner for a bucket nobody has would be a large amount of code that could
// not be run. The service that holds the bytes serves them, and the token is
// what lets it answer without a session.
//
// No revocation. A token is valid until it expires, and the only way to cancel
// one early is to rotate the key, which cancels every token signed with it. An
// expiry of hours rather than days is the design that makes this tolerable, and
// pretending otherwise with a revocation list nothing checks would be worse.
package signedurl

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The ways verification fails, kept apart because they mean different things to
// whoever is reading a log.
//
// A malformed or badly signed token is somebody meddling, or a link that was
// mangled in an email. An expired one is ordinary — links expire, and a person
// following one is entitled to be told that rather than to be told they are not
// allowed.
var (
	ErrMalformed    = errors.New("the link is not one this platform issued")
	ErrUnknownKey   = errors.New("the link was signed with a key this platform no longer holds")
	ErrBadSignature = errors.New("the link has been altered since it was issued")
	ErrExpired      = errors.New("the link has expired")
	// ErrWrongPurpose and ErrWrongTenant are for a caller checking a verified
	// grant against what it is about to serve. Verification alone proves the
	// token is genuine, not that it is genuine for this.
	ErrWrongPurpose = errors.New("the link is for something else")
)

// MinSecretLength is the shortest key this package will use.
//
// Thirty-two bytes, which is the output size of the hash underneath. A shorter
// key does not make HMAC-SHA256 weaker in any way anybody can exploit, and a
// key short enough to be typed is a key somebody typed — refusing it is a
// cheap way to keep "temporary" out of production.
const MinSecretLength = 32

// MaxLifetime bounds how long a link may be made to last.
//
// A day. Long enough to email somebody a report in the morning and have them
// open it after lunch; short enough that a link found in a log next month is
// worth nothing. A caller asking for longer is refused rather than quietly
// given a day, because a caller that believed it had a week would build on it.
const MaxLifetime = 24 * time.Hour

// DefaultLifetime is what a caller gets for asking for nothing.
const DefaultLifetime = 15 * time.Minute

// Grant is what a token says.
type Grant struct {
	// Purpose is what the link is for, such as "report" or "file". It is part
	// of the signature, so a token cannot be carried from one to the other.
	Purpose string
	// TenantID is the authority a signed request acts under. It comes from
	// here and from nowhere else.
	TenantID string
	// Resource is the one thing this token may fetch.
	Resource string
	// Expires is when it stops working, to the second.
	Expires time.Time
}

// Key is one signing key.
type Key struct {
	// ID names the key inside the token, so a verifier holding several knows
	// which to try. It is not secret and it is not a hint about the secret.
	ID string
	// Secret is the key itself.
	Secret []byte
}

// Keyring signs with one key and verifies against several.
//
// The several is what makes rotation possible. A key changed with no overlap
// invalidates every link already sent, which somebody discovers as a morning of
// reports that will not open — so a deployment sets the new key as current and
// keeps the old one until the longest lifetime has passed.
type Keyring struct {
	current Key
	byID    map[string]Key
}

// NewKeyring builds a keyring.
//
// previous are verified against and never signed with. A duplicate id is
// refused: two keys answering to one name means a token's key id no longer
// identifies which key signed it, and verification would depend on map order.
func NewKeyring(current Key, previous ...Key) (*Keyring, error) {
	if err := validKey(current); err != nil {
		return nil, fmt.Errorf("current key: %w", err)
	}
	ring := &Keyring{current: current, byID: map[string]Key{current.ID: current}}
	for i, p := range previous {
		if err := validKey(p); err != nil {
			return nil, fmt.Errorf("previous key %d: %w", i, err)
		}
		if _, clash := ring.byID[p.ID]; clash {
			return nil, fmt.Errorf("two keys are called %q, so a token's key id no longer "+
				"says which one signed it", p.ID)
		}
		ring.byID[p.ID] = p
	}
	return ring, nil
}

func validKey(k Key) error {
	if strings.TrimSpace(k.ID) == "" {
		return errors.New("a key needs an id, which goes in the token so a verifier " +
			"holding several knows which to try")
	}
	if strings.ContainsAny(k.ID, separator+".") {
		return fmt.Errorf("the key id %q holds a character the token format uses as a "+
			"delimiter", k.ID)
	}
	if len(k.Secret) < MinSecretLength {
		return fmt.Errorf("the key is %d bytes and the shortest this platform will use is %d; "+
			"a key short enough to type is a key somebody typed", len(k.Secret), MinSecretLength)
	}
	return nil
}

// separator joins the fields inside a token.
//
// A unit separator rather than a comma or a colon, and any field containing one
// is refused. Without that, a tenant id ending in the separator and a resource
// beginning with one would produce the same signed bytes as a different pair —
// the signature would verify and the token would fetch something nobody issued
// a token for.
const separator = "\x1f"

// version prefixes every token, so a later format can be told from this one
// rather than failing as a bad signature.
const version = "v1"

// Sign issues a token for a grant.
//
// The expiry is taken from the grant rather than computed here, so the caller
// decides and this bounds it. A zero expiry is the default lifetime from now;
// anything beyond MaxLifetime is refused.
func (k *Keyring) Sign(g Grant, now time.Time) (string, error) {
	if k == nil {
		return "", errors.New("no signing key is configured, so this platform cannot issue a link")
	}
	if strings.TrimSpace(g.Purpose) == "" {
		return "", errors.New("a link needs a purpose, or a token for one thing would fetch another")
	}
	if strings.TrimSpace(g.TenantID) == "" {
		return "", errors.New("a link needs a tenant; it is the authority the request acts under")
	}
	if strings.TrimSpace(g.Resource) == "" {
		return "", errors.New("a link needs the one thing it may fetch, or it is a key to the table")
	}
	for name, field := range map[string]string{
		"purpose": g.Purpose, "tenant": g.TenantID, "resource": g.Resource,
	} {
		if strings.Contains(field, separator) {
			return "", fmt.Errorf("the %s holds the character this token format joins fields "+
				"with, which would let two different grants sign identically", name)
		}
	}

	expires := g.Expires
	if expires.IsZero() {
		expires = now.Add(DefaultLifetime)
	}
	if !expires.After(now) {
		return "", fmt.Errorf("the link would expire at %s, which is not after %s",
			expires.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339))
	}
	if expires.Sub(now) > MaxLifetime {
		return "", fmt.Errorf("a link may last at most %s and this one would last %s; a caller "+
			"quietly given less than it asked for would build on the longer number",
			MaxLifetime, expires.Sub(now).Round(time.Second))
	}

	payload := encode(strings.Join([]string{
		g.Purpose, g.TenantID, g.Resource, strconv.FormatInt(expires.Unix(), 10),
	}, separator))

	signed := version + "." + k.current.ID + "." + payload
	return signed + "." + encode(string(mac(k.current.Secret, signed))), nil
}

// Verify reads a token and returns what it says.
//
// The returned grant is the only thing a caller may act on. Nothing else in the
// request means anything on a signed link: there is no session, so any header
// naming a tenant is whatever the caller typed.
//
// The signature is checked before the payload is interpreted, which is the
// order that matters: a verifier that parsed first would be making decisions
// about fields nobody has yet shown to be genuine.
func (k *Keyring) Verify(token string, now time.Time) (Grant, error) {
	if k == nil {
		return Grant{}, errors.New("no signing key is configured, so no link can be checked")
	}

	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != version {
		return Grant{}, ErrMalformed
	}
	keyID, payload, sig := parts[1], parts[2], parts[3]

	key, known := k.byID[keyID]
	if !known {
		return Grant{}, ErrUnknownKey
	}

	want := mac(key.Secret, version+"."+keyID+"."+payload)
	got, err := decode(sig)
	if err != nil {
		return Grant{}, ErrMalformed
	}
	// Constant time, so a token cannot be guessed a byte at a time by timing
	// the refusals. hmac.Equal also handles the length difference without
	// leaking it.
	if !hmac.Equal([]byte(got), want) {
		return Grant{}, ErrBadSignature
	}

	// Genuine. Only now is what it says worth reading.
	raw, err := decode(payload)
	if err != nil {
		return Grant{}, ErrMalformed
	}
	fields := strings.Split(string(raw), separator)
	if len(fields) != 4 {
		return Grant{}, ErrMalformed
	}
	unix, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return Grant{}, ErrMalformed
	}
	g := Grant{
		Purpose: fields[0], TenantID: fields[1], Resource: fields[2],
		Expires: time.Unix(unix, 0).UTC(),
	}
	if g.Purpose == "" || g.TenantID == "" || g.Resource == "" {
		// A genuine token cannot hold these, because Sign refuses them. One
		// that does was signed by something else holding the key, which is
		// worth refusing rather than serving.
		return Grant{}, ErrMalformed
	}

	// Expiry last, so an expired token is reported as expired rather than as
	// anything more alarming — and so a tampered token is never reported as
	// merely expired, which would be a hint about what to change next.
	if !g.Expires.After(now) {
		return g, ErrExpired
	}
	return g, nil
}

// ForPurpose verifies a token and checks it is for what the caller is serving.
//
// The check is here rather than left to each caller because forgetting it is
// invisible: the token verifies, the resource loads, and the only thing wrong
// is that a link issued for one kind of thing fetched another.
func (k *Keyring) ForPurpose(token, purpose string, now time.Time) (Grant, error) {
	g, err := k.Verify(token, now)
	if err != nil {
		return g, err
	}
	if g.Purpose != purpose {
		return g, fmt.Errorf("%w: it is for %q and this serves %q",
			ErrWrongPurpose, g.Purpose, purpose)
	}
	return g, nil
}

// keyID names a key from the key itself.
//
// A hash of the secret rather than a value somebody configures, so a deployment
// sets one thing per key and cannot pair an id with the wrong secret. Truncated
// to eight hex characters: a verifier holding two keys needs only to tell them
// apart, and a longer id would be more of the hash in every link for no gain.
//
// It is derived from the secret and it is not a usable hint about it — SHA-256
// is not reversible, and thirty-two bits of it narrows nothing anybody could
// search. It goes in the token because a verifier that had to try every key it
// holds would do more work and leak more timing.
func keyID(secret []byte) string {
	sum := sha256.Sum256(append([]byte("gavya/signedurl/keyid\x00"), secret...))
	return hex.EncodeToString(sum[:4])
}

func mac(secret []byte, over string) []byte {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(over))
	return m.Sum(nil)
}

// Raw URL-safe base64 throughout: it survives a query string, an email client
// and a copy-paste without escaping, and padding would only add a character
// that some of those mangle.
func encode(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

// Strict, so that a token has exactly one spelling.
//
// A 32-byte HMAC is 43 base64 characters: 43 sixes is 258 bits carrying 256,
// so the last character has two bits that encode nothing. The default decoder
// ignores them, which means four different final characters decode to the same
// signature and one grant has four spellings. The end-to-end test that changes
// one character of a link and demands a refusal caught this by landing on one
// of those four, which it does about one run in sixteen — it had passed on
// every run before.
//
// This is not a way past the signature. All four spellings carry the same
// signature over the same payload, so whoever holds one already holds a valid
// link and gains nothing by respelling it. What it costs is canonicality: the
// token string stops being an identifier for the grant, and anything that keys
// on it — a log line, a replay cache, a revocation list somebody adds later —
// sees four different tokens where there is one. Strict decoding refuses the
// non-zero trailing bits, so there is one spelling and any single character
// changed anywhere in the token is refused every time rather than 15 times in
// 16.
func decode(s string) (string, error) {
	b, err := base64.RawURLEncoding.Strict().DecodeString(s)
	return string(b), err
}
