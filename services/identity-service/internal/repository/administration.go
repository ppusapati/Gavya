package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ppusapati/gavya/libs/integrity/authz"
)

// Member is a person's place in one tenant.
type Member struct {
	UserID      string
	Email       string
	FullName    string
	RoleID      string
	RoleName    string
	Status      string
	IsDefault   bool
	HasPassword bool
	LastLoginAt *time.Time
}

// ServiceIdentityRow is a machine credential as an administrator sees it.
//
// It carries no secret and no hash. There is nothing to be gained by showing
// either, and a listing that includes a hash is a listing somebody will paste
// into a support ticket.
type ServiceIdentityRow struct {
	ID         string
	Name       string
	Status     string
	ExpiresAt  time.Time
	LastUsedAt *time.Time
	CreatedAt  time.Time
}

// EnsureBuiltinRoles gives a tenant a row for each role the platform defines.
//
// Memberships reference a role by (tenant_id, id), so every tenant needs its own
// rows — a shared role table would let a membership in one tenant point at a
// role defined in another, which is the thing that foreign key exists to stop.
//
// The permissions column is written from authz and never read back. Authorisation
// resolves a role by name, in code, where the seven roles are reviewed in a diff
// and covered by tests that fail when one quietly widens. The column is a
// projection so that a row means something to somebody reading the database
// directly; refreshed on every call, so it cannot drift for long. It is not a
// second source of truth and nothing decides on it.
func (r *repo) EnsureBuiltinRoles(ctx context.Context, tenantID, actor string) error {
	if tenantID == "" {
		return fmt.Errorf("ensure roles: no tenant")
	}
	batch := &pgx.Batch{}
	for name, role := range authz.Roles() {
		perms := make([]string, 0, len(role.Permissions))
		for p := range role.Permissions {
			perms = append(perms, string(p))
		}
		batch.Queue(`
			INSERT INTO roles (id, tenant_id, name, description, permissions, is_builtin,
			                   created_by, updated_by)
			VALUES ($1, $2, $3, $4, $5, TRUE, $6, $6)
			ON CONFLICT (tenant_id, name) DO UPDATE
				SET description = EXCLUDED.description,
				    permissions = EXCLUDED.permissions,
				    is_builtin  = TRUE,
				    updated_at  = NOW(),
				    updated_by  = EXCLUDED.updated_by`,
			roleID(tenantID, name), tenantID, name, role.Description, perms, actor)
	}
	res := r.db.SendBatch(ctx, batch)
	defer res.Close()
	for range authz.Roles() {
		if _, err := res.Exec(); err != nil {
			return fmt.Errorf("ensure builtin roles: %w", err)
		}
	}
	return nil
}

// RoleIDFor is the id of one builtin role in one tenant.
func (r *repo) RoleIDFor(ctx context.Context, tenantID, name string) (string, error) {
	var id string
	err := r.db.QueryRow(ctx,
		`SELECT id FROM roles WHERE tenant_id = $1 AND name = $2 AND deleted_at IS NULL`,
		tenantID, name).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("role %q: %w", name, err)
	}
	return id, nil
}

// AddMember creates a person, or attaches one who already exists, and gives them
// a role in this tenant.
//
// Through the definer-rights function, because creating a person is not a
// tenant-scoped act: the membership that would authorise it under the policy on
// users is the thing being created.
func (r *repo) AddMember(ctx context.Context, tenantID, email, fullName, passwordHash, roleID, actor, membershipID, userID string) (string, bool, error) {
	var id string
	var created bool
	err := r.db.QueryRow(ctx,
		`SELECT out_user_id, out_created FROM gavya_add_member($1,$2,$3,$4,$5,$6,$7,$8)`,
		tenantID, email, fullName, passwordHash, roleID, actor, membershipID, userID).
		Scan(&id, &created)
	if err != nil {
		return "", false, fmt.Errorf("add member: %w", err)
	}
	return id, created, nil
}

// ListMembers is everybody in one tenant.
func (r *repo) ListMembers(ctx context.Context, tenantID string) ([]Member, error) {
	rows, err := r.db.Query(ctx, `
		SELECT u.id, u.email, u.full_name, m.role_id, r.name, m.status, m.is_default,
		       u.password_hash IS NOT NULL, u.last_login_at
		FROM tenant_memberships m
		JOIN users u ON u.id = m.user_id
		JOIN roles r ON r.tenant_id = m.tenant_id AND r.id = m.role_id
		WHERE m.tenant_id = $1 AND m.deleted_at IS NULL
		ORDER BY u.full_name, u.email`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	defer rows.Close()

	var out []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Email, &m.FullName, &m.RoleID, &m.RoleName,
			&m.Status, &m.IsDefault, &m.HasPassword, &m.LastLoginAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetMemberRole changes what somebody may do.
func (r *repo) SetMemberRole(ctx context.Context, tenantID, userID, roleID, actor string) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE tenant_memberships
		SET role_id = $3, updated_at = NOW(), updated_by = $4
		WHERE tenant_id = $1 AND user_id = $2 AND deleted_at IS NULL`,
		tenantID, userID, roleID, actor)
	if err != nil {
		return fmt.Errorf("set member role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetMemberStatus suspends or restores somebody without removing the record of
// their having been here.
func (r *repo) SetMemberStatus(ctx context.Context, tenantID, userID, status, actor string) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE tenant_memberships
		SET status = $3, updated_at = NOW(), updated_by = $4
		WHERE tenant_id = $1 AND user_id = $2 AND deleted_at IS NULL`,
		tenantID, userID, status, actor)
	if err != nil {
		return fmt.Errorf("set member status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetPassword replaces somebody's password hash.
//
// Runs under the policy on users, which permits updating a person who is already
// a member of the calling tenant — so an administrator cannot set the password
// of somebody who belongs to another co-operative, and the check is the
// database's rather than this function's.
func (r *repo) SetPassword(ctx context.Context, userID, hash, actor string) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE users
		SET password_hash = $2, failed_attempts = 0, locked_until = NULL,
		    updated_at = NOW(), updated_by = $3
		WHERE id = $1`, userID, hash, actor)
	if err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PasswordHashFor reads one person's stored hash, for the change-your-own-password
// path that has to check the current one.
func (r *repo) PasswordHashFor(ctx context.Context, userID string) (string, error) {
	var hash *string
	err := r.db.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, userID).Scan(&hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("password hash: %w", err)
	}
	return str(hash), nil
}

// CreateServiceIdentity issues a machine credential.
//
// The secret is hashed by the caller and arrives hashed. Nothing here has ever
// seen the plaintext, which is the same arrangement as a password and for the
// same reason: a credential nobody rotates by noticing is worse to store
// recoverably, not better.
func (r *repo) CreateServiceIdentity(ctx context.Context, id, tenantID, name, secretHash string, expiresAt time.Time, actor string) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO service_identities
			(id, tenant_id, name, secret_hash, status, expires_at, created_by, updated_by)
		VALUES ($1, $2, $3, $4, 'active', $5, $6, $6)`,
		id, tenantID, name, secretHash, expiresAt, actor)
	if err != nil {
		return fmt.Errorf("create service identity: %w", err)
	}
	return nil
}

// RevokeServiceIdentity stops a machine credential being usable.
//
// Revoked rather than deleted: a credential that was used has a history, and a
// row that is gone answers no question about what it did before it went.
func (r *repo) RevokeServiceIdentity(ctx context.Context, tenantID, id, actor string) error {
	tag, err := r.db.Exec(ctx, `
		UPDATE service_identities
		SET status = 'revoked', updated_at = NOW(), updated_by = $3
		WHERE tenant_id = $1 AND id = $2 AND deleted_at IS NULL AND status <> 'revoked'`,
		tenantID, id, actor)
	if err != nil {
		return fmt.Errorf("revoke service identity: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListServiceIdentities is every machine credential this tenant holds.
func (r *repo) ListServiceIdentities(ctx context.Context, tenantID string) ([]ServiceIdentityRow, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, name, status, expires_at, last_used_at, created_at
		FROM service_identities
		WHERE tenant_id = $1 AND deleted_at IS NULL
		ORDER BY name`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list service identities: %w", err)
	}
	defer rows.Close()

	var out []ServiceIdentityRow
	for rows.Next() {
		var s ServiceIdentityRow
		if err := rows.Scan(&s.ID, &s.Name, &s.Status, &s.ExpiresAt, &s.LastUsedAt, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// roleID is a builtin role's id: deterministic, so running EnsureBuiltinRoles
// twice cannot produce two rows for one role, and so a role's id is the same
// wherever a tenant's data is restored.
//
// Derived from the tenant as well as the name, and that is not cosmetic: id is
// the primary key of roles, shared across tenants, so a name-only id would give
// the second co-operative to be provisioned a duplicate-key failure on every one
// of the seven — or, if the insert had been written to ignore conflicts, one
// tenant's roles silently standing in for another's.
func roleID(tenantID, name string) string {
	sum := sha256.Sum256([]byte(tenantID + "\x1e" + name))
	return "RL" + strings.ToUpper(hex.EncodeToString(sum[:]))[:24]
}

// ErrNotFound is returned when an administration call names something this
// tenant does not have.
//
// Its own value in this package rather than a shared one, and returned rather
// than a bare "0 rows updated", so a handler can answer not_found instead of
// reporting success for a change it did not make.
var ErrNotFound = errors.New("not found")
