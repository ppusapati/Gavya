package p9context

import (
	"context"
	"time"
)

// SessionContext contains database-backed session information.
// This is set by the auth middleware after validating the JWT and looking up the session.
type SessionContext struct {
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

type sessionContextKey struct{}

// NewSessionContext creates a new context with the session context.
func NewSessionContext(ctx context.Context, session SessionContext) context.Context {
	return context.WithValue(ctx, sessionContextKey{}, &session)
}

// FromSessionContext retrieves the session context from context.
// Returns nil and false if not present.
func FromSessionContext(ctx context.Context) (*SessionContext, bool) {
	v, ok := ctx.Value(sessionContextKey{}).(*SessionContext)
	if ok && v != nil {
		return v, true
	}
	return nil, false
}

// MustSessionContext retrieves the session context from context.
// Panics if not present.
func MustSessionContext(ctx context.Context) SessionContext {
	v, ok := FromSessionContext(ctx)
	if !ok || v == nil {
		panic("session context not found in context")
	}
	return *v
}

// SessionID retrieves the session ID from context.
// Returns empty string if not present.
func SessionID(ctx context.Context) string {
	if session, ok := FromSessionContext(ctx); ok {
		return session.SessionID
	}
	return ""
}

// IsSessionValid checks if the session is valid (not expired and not revoked).
// Returns false if session context is not present.
func IsSessionValid(ctx context.Context) bool {
	session, ok := FromSessionContext(ctx)
	if !ok || session == nil {
		return false
	}

	// Check if revoked
	if session.IsRevoked {
		return false
	}

	// Check if expired
	if time.Now().After(session.ExpiresAt) {
		return false
	}

	return true
}

// SessionExpiresAt retrieves the session expiration time from context.
// Returns zero time if not present.
func SessionExpiresAt(ctx context.Context) time.Time {
	if session, ok := FromSessionContext(ctx); ok {
		return session.ExpiresAt
	}
	return time.Time{}
}

// SessionCreatedAt retrieves the session creation time from context.
// Returns zero time if not present.
func SessionCreatedAt(ctx context.Context) time.Time {
	if session, ok := FromSessionContext(ctx); ok {
		return session.CreatedAt
	}
	return time.Time{}
}

// SessionLastActive retrieves the session's last activity time from context.
// Returns zero time if not present.
func SessionLastActive(ctx context.Context) time.Time {
	if session, ok := FromSessionContext(ctx); ok {
		return session.LastActive
	}
	return time.Time{}
}

// SessionIPAddress retrieves the session's originating IP address from context.
// Returns empty string if not present.
func SessionIPAddress(ctx context.Context) string {
	if session, ok := FromSessionContext(ctx); ok {
		return session.IPAddress
	}
	return ""
}

// SessionUserAgent retrieves the session's user agent from context.
// Returns empty string if not present.
func SessionUserAgent(ctx context.Context) string {
	if session, ok := FromSessionContext(ctx); ok {
		return session.UserAgent
	}
	return ""
}

// HasSessionContext returns true if the context has session context set.
func HasSessionContext(ctx context.Context) bool {
	_, ok := FromSessionContext(ctx)
	return ok
}

// IsSessionRevoked returns true if the session has been revoked.
// Returns false if session context is not present.
func IsSessionRevoked(ctx context.Context) bool {
	session, ok := FromSessionContext(ctx)
	if !ok || session == nil {
		return false
	}
	return session.IsRevoked
}

// IsSessionExpired returns true if the session has expired.
// Returns true if session context is not present (fail-safe).
func IsSessionExpired(ctx context.Context) bool {
	session, ok := FromSessionContext(ctx)
	if !ok || session == nil {
		return true // Fail-safe: no session = expired
	}
	return time.Now().After(session.ExpiresAt)
}

// SessionTimeRemaining returns the time remaining until session expiration.
// Returns zero duration if session is expired or not present.
func SessionTimeRemaining(ctx context.Context) time.Duration {
	session, ok := FromSessionContext(ctx)
	if !ok || session == nil {
		return 0
	}
	remaining := time.Until(session.ExpiresAt)
	if remaining < 0 {
		return 0
	}
	return remaining
}
