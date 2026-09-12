package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/authz"
	"github.com/ppusapati/gavya/libs/integrity/credential"
	"github.com/ppusapati/gavya/services/identity-service/internal/repository"
)

// ErrInvalid is a request that is wrong rather than refused.
var ErrInvalid = errors.New("invalid argument")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// RoleSummary is one role as an administrator chooses it.
type RoleSummary struct {
	Name        string
	Description string
	Permissions []string
}

// ListRoles is the roles a member may be given.
//
// Read from authz rather than from the database, so what an administrator is
// offered is what will actually be enforced. Offering the roles table would mean
// a UI listing something the authorisation layer does not recognise, and a
// member assigned it holding nothing — a role that exists to be chosen and does
// nothing when chosen.
func (s *Service) ListRoles() []RoleSummary {
	out := make([]RoleSummary, 0, len(authz.Roles()))
	for name, r := range authz.Roles() {
		perms := make([]string, 0, len(r.Permissions))
		for p := range r.Permissions {
			perms = append(perms, string(p))
		}
		sort.Strings(perms)
		out = append(out, RoleSummary{Name: name, Description: r.Description, Permissions: perms})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// AddMemberInput is somebody being given a job in a co-operative.
type AddMemberInput struct {
	TenantID string
	Email    string
	FullName string
	Role     string
	// Password is optional. Left empty the person exists and cannot sign in yet,
	// which is a real state — somebody enrolled before they have been handed a
	// credential — and distinct from an empty password, which would compare
	// against nothing and look like a valid failed attempt.
	Password string
	Actor    string
}

// AddedMember is what came of adding one.
type AddedMember struct {
	UserID string
	// Created says whether this made a new person or attached one who already
	// had an account. An address that already exists joins the existing person:
	// two accounts for one address is how a revoked login stays usable.
	Created bool
}

// AddMember enrols somebody in a tenant with a role.
func (s *Service) AddMember(ctx context.Context, in AddMemberInput) (*AddedMember, error) {
	switch {
	case in.TenantID == "":
		return nil, invalid("tenant_id is required")
	case strings.TrimSpace(in.Email) == "":
		return nil, invalid("email is required")
	case !strings.Contains(in.Email, "@"):
		return nil, invalid("%q is not an email address", in.Email)
	case strings.TrimSpace(in.FullName) == "":
		return nil, invalid("a member must have a name, so a trail says who acted")
	case in.Actor == "":
		return nil, invalid("actor is required")
	}
	if _, ok := authz.Roles()[in.Role]; !ok {
		return nil, invalid("role %q is not one this platform defines; see ListRoles", in.Role)
	}

	var hash string
	if in.Password != "" {
		if err := checkPasswordStrength(in.Password); err != nil {
			return nil, err
		}
		h, err := credential.Hash(in.Password)
		if err != nil {
			return nil, fmt.Errorf("hash password: %w", err)
		}
		hash = h
	}

	// The tenant's own role rows, which memberships reference by
	// (tenant_id, role_id). Done on the way in rather than at tenant creation so
	// a co-operative provisioned before this existed is not stranded without
	// them.
	if err := s.repo.EnsureBuiltinRoles(ctx, in.TenantID, in.Actor); err != nil {
		return nil, err
	}
	roleID, err := s.repo.RoleIDFor(ctx, in.TenantID, in.Role)
	if err != nil {
		return nil, err
	}

	userID, created, err := s.repo.AddMember(ctx, in.TenantID,
		strings.TrimSpace(in.Email), strings.TrimSpace(in.FullName), hash, roleID,
		in.Actor, s.ids.New(), s.ids.New())
	if err != nil {
		return nil, err
	}
	s.log.Infof("tenant %s: %s is now a %s", in.TenantID, in.Email, in.Role)
	return &AddedMember{UserID: userID, Created: created}, nil
}

// ListMembers is everybody in one tenant.
func (s *Service) ListMembers(ctx context.Context, tenantID string) ([]repository.Member, error) {
	if tenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	return s.repo.ListMembers(ctx, tenantID)
}

// SetMemberRole changes what somebody may do.
func (s *Service) SetMemberRole(ctx context.Context, tenantID, userID, role, actor string) error {
	switch {
	case tenantID == "" || userID == "":
		return invalid("tenant_id and user_id are required")
	case actor == "":
		return invalid("actor is required")
	}
	if _, ok := authz.Roles()[role]; !ok {
		return invalid("role %q is not one this platform defines; see ListRoles", role)
	}
	if err := s.repo.EnsureBuiltinRoles(ctx, tenantID, actor); err != nil {
		return err
	}
	roleID, err := s.repo.RoleIDFor(ctx, tenantID, role)
	if err != nil {
		return err
	}
	if err := s.repo.SetMemberRole(ctx, tenantID, userID, roleID, actor); err != nil {
		return err
	}
	s.log.Infof("tenant %s: %s is now a %s", tenantID, userID, role)
	return nil
}

// SetMemberStatus suspends or restores somebody.
//
// Sessions they already hold are revoked when they are suspended. Leaving them
// would mean a suspension takes effect at the next sign-in, which is the one
// thing a suspended person will not do.
func (s *Service) SetMemberStatus(ctx context.Context, tenantID, userID, status, actor string) error {
	switch {
	case tenantID == "" || userID == "":
		return invalid("tenant_id and user_id are required")
	case actor == "":
		return invalid("actor is required")
	case status != "active" && status != "suspended":
		return invalid("status must be active or suspended, not %q", status)
	}
	if err := s.repo.SetMemberStatus(ctx, tenantID, userID, status, actor); err != nil {
		return err
	}
	if status == "suspended" {
		if n, err := s.repo.RevokeSessionsFor(ctx, userID, "membership suspended"); err != nil {
			// The suspension stands. Reported rather than rolled back: a person
			// who is suspended with a session still open is a smaller problem
			// than one who was not suspended at all because the tidying failed.
			s.log.Errorf("suspended %s but could not revoke their sessions: %v", userID, err)
		} else if n > 0 {
			s.log.Infof("suspended %s and ended %d session(s)", userID, n)
		}
	}
	return nil
}

// ChangePassword is somebody changing their own.
//
// The current password is the authorisation: this is the one administration
// route that needs no permission, because a person with no role at all must
// still be able to change the credential they were given.
func (s *Service) ChangePassword(ctx context.Context, userID, current, next string) error {
	if userID == "" {
		return invalid("user_id is required")
	}
	stored, err := s.repo.PasswordHashFor(ctx, userID)
	if err != nil {
		return err
	}
	if stored == "" {
		// Nothing to verify against. Refused rather than treated as "no password
		// means any password": an account with no credential is one somebody has
		// not been handed yet, and letting it be claimed by whoever asks first is
		// the opposite of what that state is for.
		return invalid("this account has no password set; an administrator must set one")
	}
	if err := credential.Verify(current, stored); err != nil {
		return invalid("the current password is wrong")
	}
	if err := checkPasswordStrength(next); err != nil {
		return err
	}
	hash, err := credential.Hash(next)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := s.repo.SetPassword(ctx, userID, hash, userID); err != nil {
		return err
	}
	// Every other session ends. A password is changed because the old one may be
	// known to somebody else, and a change that leaves their session open changes
	// nothing that matters.
	if n, err := s.repo.RevokeSessionsFor(ctx, userID, "password changed"); err != nil {
		s.log.Errorf("changed the password for %s but could not end their sessions: %v", userID, err)
	} else if n > 0 {
		s.log.Infof("%s changed their password; %d session(s) ended", userID, n)
	}
	return nil
}

// SetMemberPassword is an administrator setting somebody else's.
//
// For the person who has forgotten theirs, and for the first credential somebody
// is handed. The policy on users is what stops this reaching another
// co-operative's people: it permits updating somebody who is already a member of
// the calling tenant, and refuses the rest without this function having to
// check.
func (s *Service) SetMemberPassword(ctx context.Context, tenantID, userID, password, actor string) error {
	switch {
	case tenantID == "" || userID == "":
		return invalid("tenant_id and user_id are required")
	case actor == "":
		return invalid("actor is required")
	}
	if err := checkPasswordStrength(password); err != nil {
		return err
	}
	hash, err := credential.Hash(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := s.repo.SetPassword(ctx, userID, hash, actor); err != nil {
		return err
	}
	if n, err := s.repo.RevokeSessionsFor(ctx, userID, "password reset by an administrator"); err != nil {
		s.log.Errorf("reset the password for %s but could not end their sessions: %v", userID, err)
	} else if n > 0 {
		s.log.Infof("%s reset the password for %s; %d session(s) ended", actor, userID, n)
	}
	return nil
}

// IssuedServiceIdentity is a machine credential, and the only moment its secret
// exists outside a hash.
type IssuedServiceIdentity struct {
	ID   string
	Name string
	// Secret is returned once, here, and never again. The platform stores a hash
	// and cannot show it later; a credential that could be read back is one that
	// can be read back by whoever reaches the database.
	Secret    string
	ExpiresAt time.Time
}

// IssueServiceIdentity creates a credential for a device or a recomputation.
func (s *Service) IssueServiceIdentity(ctx context.Context, tenantID, name string, expiresAt time.Time, actor string) (*IssuedServiceIdentity, error) {
	switch {
	case tenantID == "":
		return nil, invalid("tenant_id is required")
	case strings.TrimSpace(name) == "":
		return nil, invalid("a service identity must be named, so a trail says which machine acted")
	case actor == "":
		return nil, invalid("actor is required")
	case expiresAt.IsZero():
		// No default. A credential with no end date is one nobody ever rotates,
		// and choosing a year here would be this platform deciding a
		// co-operative's rotation policy on its behalf.
		return nil, invalid("expires_at is required; a credential with no end date is one nobody rotates")
	case !expiresAt.After(s.clock.Now()):
		return nil, invalid("expires_at must be in the future")
	}

	secret, err := credential.NewSecret()
	if err != nil {
		return nil, fmt.Errorf("generate secret: %w", err)
	}
	hash, err := credential.Hash(secret)
	if err != nil {
		return nil, fmt.Errorf("hash secret: %w", err)
	}
	id := s.ids.New()
	if err := s.repo.CreateServiceIdentity(ctx, id, tenantID, strings.TrimSpace(name), hash, expiresAt, actor); err != nil {
		return nil, err
	}
	s.log.Infof("tenant %s: issued service identity %q until %s", tenantID, name, expiresAt.Format(time.RFC3339))
	return &IssuedServiceIdentity{ID: id, Name: name, Secret: secret, ExpiresAt: expiresAt}, nil
}

// RevokeServiceIdentity stops a machine credential being usable.
func (s *Service) RevokeServiceIdentity(ctx context.Context, tenantID, id, actor string) error {
	switch {
	case tenantID == "" || id == "":
		return invalid("tenant_id and id are required")
	case actor == "":
		return invalid("actor is required")
	}
	return s.repo.RevokeServiceIdentity(ctx, tenantID, id, actor)
}

// ListServiceIdentities is every machine credential a tenant holds.
func (s *Service) ListServiceIdentities(ctx context.Context, tenantID string) ([]repository.ServiceIdentityRow, error) {
	if tenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	return s.repo.ListServiceIdentities(ctx, tenantID)
}

// checkPasswordStrength is deliberately a length floor and nothing else.
//
// Composition rules — a digit, a capital, a symbol — measurably produce worse
// passwords, because people satisfy them the same way every time and the result
// is shorter. Length is the property that matters. Twelve is a floor rather than
// a target; what actually protects an account here is that the hash is Argon2id
// and that repeated failures lock it.
const minimumPasswordLength = 12

func checkPasswordStrength(p string) error {
	if len([]rune(p)) < minimumPasswordLength {
		return invalid("a password must be at least %d characters", minimumPasswordLength)
	}
	return nil
}
