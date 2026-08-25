// Package service turns a sign-in attempt into a session, and a session into
// the tenant a request acts for.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/credential"
	"github.com/ppusapati/gavya/libs/integrity/tenantdb"

	"github.com/ppusapati/gavya/services/identity-service/internal/domain"
	"github.com/ppusapati/gavya/services/identity-service/internal/repository"
)

// IDs generates identifiers. Injected so a test can make them predictable.
type IDs interface{ New() string }

// Clock is the time source, injected for the same reason.
type Clock interface{ Now() time.Time }

type Service struct {
	repo    repository.Repository
	ids     IDs
	clock   Clock
	lockout domain.Lockout
	log     Logger
}

type Logger interface {
	Infof(string, ...any)
	Errorf(string, ...any)
}

func New(r repository.Repository, ids IDs, clock Clock, log Logger) *Service {
	return &Service{repo: r, ids: ids, clock: clock, lockout: domain.DefaultLockout, log: log}
}

// SignInRequest is one attempt.
type SignInRequest struct {
	Email    string
	Password string
	// TenantID is optional. When empty the tenant is chosen from the person's
	// memberships, and the attempt is refused if that choice is not obvious.
	TenantID  string
	IPAddress string
	UserAgent string
}

// SignInResult is a session.
type SignInResult struct {
	SessionID string
	TenantID  string
	UserID    string
	RoleID    string
	RoleName  string
	ExpiresAt time.Time
}

// SignIn verifies a password and opens a session.
//
// The order of what happens here is the security property, so it is worth
// stating. The account is looked up, a password is verified — against the stored
// hash if there is one and against a dummy hash if there is not — the decision
// is made, the outcome is recorded, and only then is a session created. Nothing
// returns early on a missing account, because returning early is microseconds
// against a tenth of a second and that gap answers "does this address have an
// account".
func (s *Service) SignIn(ctx context.Context, req SignInRequest) (*SignInResult, error) {
	email := normaliseEmail(req.Email)
	if email == "" || req.Password == "" {
		// Refused without a lookup, and told the same thing as any other
		// failure. There is no address here to leak anything about.
		return nil, domain.ErrSignInFailed
	}

	account, err := s.repo.FindAccount(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("sign in: %w", err)
	}

	// Always verify something. Which hash is used is the only difference between
	// the two paths, and it costs the same either way.
	hash := account.PasswordHash
	if hash == "" {
		hash = credential.Dummy()
	}
	matched := credential.Verify(req.Password, hash) == nil

	now := s.clock.Now()
	outcome := s.lockout.Decide(now, account, matched)

	if account.Exists {
		if err := s.repo.RecordLoginState(ctx, account.UserID,
			outcome.FailedAttempts, outcome.LockUntil, outcome.Allowed); err != nil {
			// Worth knowing about and not worth failing the sign-in over: the
			// alternative is that a database hiccup in the bookkeeping locks
			// everybody out.
			s.log.Errorf("could not record the login state for %s: %v", account.UserID, err)
		}
	}

	if !outcome.Allowed {
		s.record(ctx, email, account.UserID, "", false, outcome.Reason, req)
		return nil, domain.ErrSignInFailed
	}

	memberships, err := s.repo.MembershipsFor(ctx, account.UserID)
	if err != nil {
		return nil, fmt.Errorf("sign in: %w", err)
	}
	chosen, err := domain.ChooseTenant(req.TenantID, memberships)
	if err != nil {
		// These reach the caller as themselves rather than as ErrSignInFailed.
		// By this point the password has been proved, so nothing said here tells
		// anybody anything they did not already establish — and "you belong to
		// three tenants, name one" is useless as anything else.
		s.record(ctx, email, account.UserID, req.TenantID, false, "tenant: "+err.Error(), req)
		return nil, err
	}

	session := repository.Session{
		ID:        s.ids.New(),
		TenantID:  chosen.TenantID,
		UserID:    account.UserID,
		ExpiresAt: now.Add(domain.SessionLifetime),
		IPAddress: req.IPAddress,
		UserAgent: req.UserAgent,
		CreatedBy: account.UserID,
	}
	// Scoped from here on, like every other write in the platform.
	scoped := tenantdb.WithTenant(ctx, chosen.TenantID)
	if err := s.repo.CreateSession(scoped, session); err != nil {
		return nil, fmt.Errorf("sign in: %w", err)
	}

	s.record(ctx, email, account.UserID, chosen.TenantID, true, "", req)
	return &SignInResult{
		SessionID: session.ID,
		TenantID:  chosen.TenantID,
		UserID:    account.UserID,
		RoleID:    chosen.RoleID,
		RoleName:  chosen.RoleName,
		ExpiresAt: session.ExpiresAt,
	}, nil
}

// SignInService opens a session for a calling service.
func (s *Service) SignInService(ctx context.Context, name, secret, ip string) (*SignInResult, error) {
	if name == "" || secret == "" {
		return nil, domain.ErrSignInFailed
	}
	identity, err := s.repo.FindServiceIdentity(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("service sign in: %w", err)
	}

	hash := identity.SecretHash
	if hash == "" {
		hash = credential.Dummy()
	}
	matched := credential.Verify(secret, hash) == nil

	now := s.clock.Now()
	outcome := domain.DecideService(now, identity, matched)
	if !outcome.Allowed {
		s.log.Infof("service sign-in refused for %q: %s", name, outcome.Reason)
		return nil, domain.ErrSignInFailed
	}
	if identity.TenantID == "" {
		// A platform-wide service identity has no tenant, and a session must
		// have one — every policy downstream compares against a single value.
		// Refused rather than given an empty scope that would read as "no rows"
		// everywhere and look like a data problem.
		return nil, errors.New("this service identity is not issued to a tenant, so it cannot open a session")
	}

	session := repository.Session{
		ID:                s.ids.New(),
		TenantID:          identity.TenantID,
		ServiceIdentityID: identity.ID,
		ExpiresAt:         now.Add(domain.ServiceSessionLifetime),
		IPAddress:         ip,
		CreatedBy:         identity.ID,
	}
	scoped := tenantdb.WithTenant(ctx, identity.TenantID)
	if err := s.repo.CreateSession(scoped, session); err != nil {
		return nil, fmt.Errorf("service sign in: %w", err)
	}
	return &SignInResult{
		SessionID: session.ID,
		TenantID:  identity.TenantID,
		UserID:    identity.ID,
		ExpiresAt: session.ExpiresAt,
	}, nil
}

// ErrNotSignedIn is what a caller presenting an unusable session is told. One
// value for every cause: expired, revoked and never-existed are different things
// to an operator and the same thing to whoever is holding the session.
var ErrNotSignedIn = errors.New("not signed in")

// Verified is what a session turned out to authorise.
type Verified struct {
	TenantID          string
	UserID            string
	ServiceIdentityID string
}

// Verify resolves a session to the tenant it acts for.
//
// This is what the gateway calls on every request, and what makes the tenant
// something that was checked rather than something the caller asserted.
func (s *Service) Verify(ctx context.Context, sessionID string) (*Verified, error) {
	if sessionID == "" {
		return nil, ErrNotSignedIn
	}
	res, err := s.repo.ResolveSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("verify: %w", err)
	}
	if !res.Valid {
		s.log.Infof("session %s refused: %s", sessionID, res.Reason)
		return nil, ErrNotSignedIn
	}
	return &Verified{
		TenantID:          res.TenantID,
		UserID:            res.UserID,
		ServiceIdentityID: res.ServiceIdentityID,
	}, nil
}

// SignOut ends a session.
//
// Idempotent by design: a client that retries a sign-out because the first reply
// was lost must not be told it failed, or it will keep retrying against a
// session that is already gone.
func (s *Service) SignOut(ctx context.Context, sessionID, reason string) error {
	if sessionID == "" {
		return ErrNotSignedIn
	}
	if reason == "" {
		reason = "signed out"
	}
	res, err := s.repo.ResolveSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if res.TenantID == "" {
		// Already revoked, expired, or never existed. Nothing to do and nothing
		// to report — saying which would answer "is this a real session id".
		return nil
	}
	scoped := tenantdb.WithTenant(ctx, res.TenantID)
	if _, err := s.repo.RevokeSession(scoped, sessionID, reason); err != nil {
		return err
	}
	return nil
}

// SignOutEverywhere ends every session a person has in a tenant. What a
// dismissal and a stolen laptop both need, and the thing a signed token cannot
// do on its own.
func (s *Service) SignOutEverywhere(ctx context.Context, tenantID, userID, reason string) (int64, error) {
	if tenantID == "" || userID == "" {
		return 0, errors.New("both the tenant and the person have to be named")
	}
	if reason == "" {
		reason = "signed out everywhere"
	}
	scoped := tenantdb.WithTenant(ctx, tenantID)
	return s.repo.RevokeSessionsFor(scoped, userID, reason)
}

func (s *Service) record(ctx context.Context, email, userID, tenantID string, ok bool, reason string, req SignInRequest) {
	err := s.repo.RecordAttempt(ctx, repository.Attempt{
		ID:              s.ids.New(),
		EmailNormalised: email,
		UserID:          userID,
		TenantID:        tenantID,
		Succeeded:       ok,
		FailureReason:   reason,
		IPAddress:       req.IPAddress,
		UserAgent:       req.UserAgent,
	})
	if err != nil {
		// The log is what makes a credential-stuffing run visible, so losing a
		// line matters — but failing the request because the log failed would
		// turn a logging problem into an outage.
		s.log.Errorf("could not record a sign-in attempt for %q: %v", email, err)
	}
}

// normaliseEmail folds case and trims, matching what the schema stores and is
// unique on. Addresses differ only in case far more often than anyone intends,
// and two accounts for one person is how a revoked login stays usable.
func normaliseEmail(e string) string {
	return strings.ToLower(strings.TrimSpace(e))
}
