package signedurl

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"
)

// Reading the key out of the environment.
//
// Here rather than in each service's config, because two services read the same
// three settings and a difference between them is the kind that shows up as one
// service's links working and the other's not. It is also the one place to say
// what a bad key does: it stops the links, and it does not stop the service.

// The settings a deployment sets.
const (
	// KeyEnv is the current signing key, as base64 or hex.
	KeyEnv = "DOWNLOAD_SIGNING_KEY"
	// PreviousKeyEnv is the key before it, kept while links signed with it are
	// still in flight. A key changed with no overlap invalidates every link
	// already sent, which somebody discovers as a morning of reports that will
	// not open.
	PreviousKeyEnv = "DOWNLOAD_SIGNING_KEY_PREVIOUS"
	// BaseEnv is what makes a link absolute. Unset gives a root-relative link,
	// which is right for a console already talking to this platform through a
	// gateway and wrong for a link somebody emails.
	BaseEnv = "DOWNLOAD_PUBLIC_BASE_URL"
	// LifetimeEnv is how long a link lasts, as a Go duration.
	LifetimeEnv = "DOWNLOAD_LINK_LIFETIME"
)

// Settings is what a service needs to issue and check links.
type Settings struct {
	// Keys is nil when no key is set. A service with a nil keyring still
	// starts and still serves every other procedure; the one that issues links
	// refuses with the setting named.
	Keys     *Keyring
	Base     string
	Lifetime time.Duration
	// Why is empty when a keyring was built, and says what is wrong when one
	// was not. The caller logs it, because a key that is set and unusable is
	// the case worth saying out loud: it looks configured.
	Why string
}

// FromEnv reads the settings.
//
// A missing key is not an error. It is a deployment that has not turned
// downloads on, and refusing to start would take down the twenty procedures
// that have nothing to do with downloads to protect the one that does.
//
// A key that is set and unusable is different, and is reported: somebody
// intended downloads to work, and the difference between "not configured" and
// "configured wrongly" is the whole message.
func FromEnv() Settings {
	s := Settings{
		Base:     strings.TrimSpace(os.Getenv(BaseEnv)),
		Lifetime: DefaultLifetime,
	}

	if v := strings.TrimSpace(os.Getenv(LifetimeEnv)); v != "" {
		d, err := time.ParseDuration(v)
		switch {
		case err != nil:
			s.Why = fmt.Sprintf("%s is %q, which is not a duration; links will last %s",
				LifetimeEnv, v, DefaultLifetime)
		case d <= 0:
			s.Why = fmt.Sprintf("%s is %q, which is not positive; links will last %s",
				LifetimeEnv, v, DefaultLifetime)
		case d > MaxLifetime:
			// Refused rather than clamped. A deployment that asked for a week
			// and silently got a day would build on the week.
			s.Why = fmt.Sprintf("%s is %q and a link may last at most %s; links will last %s",
				LifetimeEnv, v, MaxLifetime, DefaultLifetime)
		default:
			s.Lifetime = d
		}
	}

	raw := strings.TrimSpace(os.Getenv(KeyEnv))
	if raw == "" {
		s.Why = KeyEnv + " is not set, so this service cannot issue or check download links"
		return s
	}

	current, err := parseKey("current", raw)
	if err != nil {
		s.Why = err.Error()
		return s
	}

	var previous []Key
	if p := strings.TrimSpace(os.Getenv(PreviousKeyEnv)); p != "" {
		old, err := parseKey("previous", p)
		if err != nil {
			// The current key is still good, so links still work; the ones
			// signed before a rotation do not. Said, and carried on with.
			s.Why = err.Error() + " — links signed before the last rotation will not open"
		} else {
			previous = append(previous, old)
		}
	}

	ring, err := NewKeyring(current, previous...)
	if err != nil {
		s.Why = err.Error()
		return s
	}
	s.Keys = ring
	if s.Why == "" && len(previous) > 0 {
		s.Why = ""
	}
	return s
}

// parseKey reads one key.
//
// The id is derived from the secret rather than configured, so a deployment
// sets one value per key and cannot get the pairing wrong. It is the first
// eight hex characters of the key's own hash, which names the key without
// being a usable hint about it: a verifier holding two keys needs only to tell
// them apart.
func parseKey(which, raw string) (Key, error) {
	secret, err := decodeSecret(raw)
	if err != nil {
		return Key{}, fmt.Errorf("the %s download signing key is neither base64 nor hex", which)
	}
	if len(secret) < MinSecretLength {
		return Key{}, fmt.Errorf("the %s download signing key is %d bytes and the shortest "+
			"this platform will use is %d; generate one with "+
			"`openssl rand -base64 32`", which, len(secret), MinSecretLength)
	}
	return Key{ID: keyID(secret), Secret: secret}, nil
}

// decodeSecret accepts base64 or hex, because both are what somebody reaches
// for and a deployment refused for choosing the wrong one is a deployment that
// pastes the key in as raw text instead.
func decodeSecret(raw string) ([]byte, error) {
	if b, err := base64.StdEncoding.DecodeString(raw); err == nil {
		return b, nil
	}
	if b, err := base64.RawURLEncoding.DecodeString(raw); err == nil {
		return b, nil
	}
	if b, err := hex.DecodeString(raw); err == nil {
		return b, nil
	}
	return nil, fmt.Errorf("not base64 or hex")
}
