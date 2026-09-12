// Package repository is the identity service's access to its tables.
//
// The pre-authentication path — everything up to the moment a tenant is
// established — goes through the definer-rights functions in
// internal/db/isolation.sql rather than reading the tables directly. Those
// queries genuinely have no tenant to be scoped by, and the alternative is a
// role that can read every tenant's identity data for the sake of a handful of
// lookups.
//
// Everything after that point runs on a connection scoped to the chosen tenant,
// like every other service in the platform.
package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/audit"
	"github.com/ppusapati/gavya/libs/integrity/sys"

	"github.com/ppusapati/gavya/services/identity-service/internal/domain"
)

type Repository interface {
	FindAccount(ctx context.Context, emailNormalised string) (domain.Account, error)
	MembershipsFor(ctx context.Context, userID string) ([]domain.Membership, error)
	FindServiceIdentity(ctx context.Context, name string) (domain.ServiceIdentity, error)

	RecordAttempt(ctx context.Context, a Attempt) error
	RecordLoginState(ctx context.Context, userID string, failed int, lockUntil *time.Time, ok bool) error

	CreateSession(ctx context.Context, s Session) error
	ResolveSession(ctx context.Context, sessionID string) (Resolved, error)
	RevokeSession(ctx context.Context, sessionID, reason string) (bool, error)
	RevokeSessionsFor(ctx context.Context, userID, reason string) (int64, error)

	// Administration: the routes that let a co-operative be set up without a
	// database console. See administration.go.
	EnsureBuiltinRoles(ctx context.Context, tenantID, actor string) error
	RoleIDFor(ctx context.Context, tenantID, name string) (string, error)
	AddMember(ctx context.Context, tenantID, email, fullName, passwordHash, roleID, actor, membershipID, userID string) (string, bool, error)
	ListMembers(ctx context.Context, tenantID string) ([]Member, error)
	SetMemberRole(ctx context.Context, tenantID, userID, roleID, actor string) error
	SetMemberStatus(ctx context.Context, tenantID, userID, status, actor string) error
	SetPassword(ctx context.Context, userID, hash, actor string) error
	PasswordHashFor(ctx context.Context, userID string) (string, error)
	CreateServiceIdentity(ctx context.Context, id, tenantID, name, secretHash string, expiresAt time.Time, actor string) error
	RevokeServiceIdentity(ctx context.Context, tenantID, id, actor string) error
	ListServiceIdentities(ctx context.Context, tenantID string) ([]ServiceIdentityRow, error)
}

// Attempt is one line of the sign-in log.
type Attempt struct {
	ID              string
	EmailNormalised string
	UserID          string
	TenantID        string
	Succeeded       bool
	FailureReason   string
	IPAddress       string
	UserAgent       string
}

// Session is a live sign-in.
type Session struct {
	ID                string
	TenantID          string
	UserID            string
	ServiceIdentityID string
	ExpiresAt         time.Time
	IPAddress         string
	UserAgent         string
	CreatedBy         string
}

// Resolved is what a session id turned out to mean.
type Resolved struct {
	Valid             bool
	Reason            string
	TenantID          string
	UserID            string
	ServiceIdentityID string
}

type repo struct {
	db  *pgxpool.Pool
	ids audit.IDs
}

// serviceName is what this service's audit entries are attributed to.
const serviceName = "identity-service"

// New builds the repository.
//
// The identifier source is fixed here rather than taken as an argument, because
// every caller passed the same one and the alternative was changing the
// composition root of the one service whose changes most need recording.
func New(db *pgxpool.Pool) Repository { return &repo{db: db, ids: sys.IDs{}} }

func (r *repo) FindAccount(ctx context.Context, emailNormalised string) (domain.Account, error) {
	var a domain.Account
	var userID, hash, status *string
	err := r.db.QueryRow(ctx,
		`SELECT found, user_id, password_hash, status, locked_until, failed_attempts
		 FROM gavya_find_user_for_login($1)`, emailNormalised).
		Scan(&a.Exists, &userID, &hash, &status, &a.LockedUntil, &a.FailedAttempts)
	if err != nil {
		return domain.Account{}, fmt.Errorf("find account: %w", err)
	}
	a.UserID = str(userID)
	a.PasswordHash = str(hash)
	a.Status = str(status)
	return a, nil
}

func (r *repo) MembershipsFor(ctx context.Context, userID string) ([]domain.Membership, error) {
	rows, err := r.db.Query(ctx,
		`SELECT tenant_id, role_id, role_name, status, is_default
		 FROM gavya_memberships_for_login($1)`, userID)
	if err != nil {
		return nil, fmt.Errorf("memberships: %w", err)
	}
	defer rows.Close()

	var out []domain.Membership
	for rows.Next() {
		var m domain.Membership
		if err := rows.Scan(&m.TenantID, &m.RoleID, &m.RoleName, &m.Status, &m.IsDefault); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *repo) FindServiceIdentity(ctx context.Context, name string) (domain.ServiceIdentity, error) {
	var s domain.ServiceIdentity
	var id, tenant, hash, status *string
	var expires *time.Time
	err := r.db.QueryRow(ctx,
		`SELECT found, identity_id, tenant_id, secret_hash, status, expires_at, permissions
		 FROM gavya_find_service_identity($1)`, name).
		Scan(&s.Exists, &id, &tenant, &hash, &status, &expires, &s.Permissions)
	if err != nil {
		return domain.ServiceIdentity{}, fmt.Errorf("find service identity: %w", err)
	}
	s.ID, s.TenantID, s.SecretHash, s.Status = str(id), str(tenant), str(hash), str(status)
	if expires != nil {
		s.ExpiresAt = *expires
	}
	return s, nil
}

func (r *repo) RecordAttempt(ctx context.Context, a Attempt) error {
	_, err := r.db.Exec(ctx,
		`SELECT gavya_record_authentication_attempt($1,$2,$3,$4,$5,$6,$7,$8)`,
		a.ID, a.EmailNormalised, a.UserID, a.TenantID,
		a.Succeeded, a.FailureReason, a.IPAddress, a.UserAgent)
	if err != nil {
		return fmt.Errorf("record attempt: %w", err)
	}
	return nil
}

func (r *repo) RecordLoginState(ctx context.Context, userID string, failed int, lockUntil *time.Time, ok bool) error {
	_, err := r.db.Exec(ctx,
		`SELECT gavya_record_login_state($1,$2,$3,$4)`, userID, failed, lockUntil, ok)
	if err != nil {
		return fmt.Errorf("record login state: %w", err)
	}
	return nil
}

// CreateSession runs under the policies. By the time a session is written the
// tenant is settled, so there is no reason for this to be an exception.
func (r *repo) CreateSession(ctx context.Context, s Session) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO auth_sessions
			(id, tenant_id, user_id, service_identity_id, expires_at, ip_address, user_agent,
			 created_by, updated_by)
		VALUES ($1,$2,nullif($3,''),nullif($4,''),$5,nullif($6,''),nullif($7,''),$8,$8)`,
		s.ID, s.TenantID, s.UserID, s.ServiceIdentityID, s.ExpiresAt,
		s.IPAddress, s.UserAgent, s.CreatedBy)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (r *repo) ResolveSession(ctx context.Context, sessionID string) (Resolved, error) {
	var out Resolved
	var reason, tenant, user, service *string
	err := r.db.QueryRow(ctx,
		`SELECT valid, reason, tenant_id, user_id, service_identity_id
		 FROM gavya_resolve_session($1)`, sessionID).
		Scan(&out.Valid, &reason, &tenant, &user, &service)
	if err != nil {
		return Resolved{}, fmt.Errorf("resolve session: %w", err)
	}
	out.Reason, out.TenantID = str(reason), str(tenant)
	out.UserID, out.ServiceIdentityID = str(user), str(service)
	return out, nil
}

// RevokeSession ends one session. Runs under the policies: revoking somebody
// else's tenant's session is not a thing that should be possible.
//
// Already-revoked rows are left alone rather than overwritten, so the record
// keeps the reason and the moment it first stopped being usable.
func (r *repo) RevokeSession(ctx context.Context, sessionID, reason string) (bool, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE auth_sessions
		SET revoked_at = NOW(), revoked_reason = $2, updated_at = NOW()
		WHERE id = $1 AND revoked_at IS NULL`, sessionID, reason)
	if err != nil {
		return false, fmt.Errorf("revoke session: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// RevokeSessionsFor ends every live session a person has in this tenant. What
// "somebody has left" and "the laptop was stolen" both need.
func (r *repo) RevokeSessionsFor(ctx context.Context, userID, reason string) (int64, error) {
	tag, err := r.db.Exec(ctx, `
		UPDATE auth_sessions
		SET revoked_at = NOW(), revoked_reason = $2, updated_at = NOW()
		WHERE user_id = $1 AND revoked_at IS NULL`, userID, reason)
	if err != nil {
		return 0, fmt.Errorf("revoke sessions: %w", err)
	}
	return tag.RowsAffected(), nil
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

var _ = pgx.ErrNoRows
