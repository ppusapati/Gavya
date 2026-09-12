package handler

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/libs/integrity/tenantctx"
	"github.com/ppusapati/gavya/services/identity-service/internal/repository"
	"github.com/ppusapati/gavya/services/identity-service/internal/service"
)

// RegisterAdministration adds the routes that let a co-operative be set up.
//
// Without these, authorisation was enforced against roles nobody could be
// given: there was no route anywhere in the platform that created a person, set
// a password, assigned a role, or issued a machine credential. Every one of them
// was a row somebody typed into a database console, which is not a thing an
// operator of a village society is going to do.
func (h *Handler) RegisterAdministration(mux *http.ServeMux) {
	route := func(method string, handler http.HandlerFunc) {
		mux.HandleFunc(connectjson.Procedure(ServiceName, method), handler)
	}

	route("ListRoles", connectjson.Unary(h.ListRoles))
	route("AddMember", connectjson.Unary(h.AddMember))
	route("ListMembers", connectjson.Unary(h.ListMembers))
	route("SetMemberRole", connectjson.Unary(h.SetMemberRole))
	route("SetMemberStatus", connectjson.Unary(h.SetMemberStatus))
	route("ChangePassword", connectjson.Unary(h.ChangePassword))
	route("SetMemberPassword", connectjson.Unary(h.SetMemberPassword))
	route("IssueServiceIdentity", connectjson.Unary(h.IssueServiceIdentity))
	route("RevokeServiceIdentity", connectjson.Unary(h.RevokeServiceIdentity))
	route("ListServiceIdentities", connectjson.Unary(h.ListServiceIdentities))
}

// classifyAdmin turns a service failure into the code that describes it.
//
// An invalid argument and a missing row are different answers and a caller acts
// on them differently: one is a request to correct, the other is a thing that is
// not there. Reporting both as Internal tells a client to retry a call that
// cannot succeed.
func classifyAdmin(err error) error {
	switch {
	case errors.Is(err, service.ErrInvalid):
		return connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, repository.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}

// ListRolesRequest carries nothing.
//
// It had a tenant_id, and the reachability check caught that nothing read it:
// the roles this platform defines are the same for every co-operative, so a
// tenant here would be a field a caller supplies, is told nothing about, and
// which changes no answer. The permission still scopes the call — a caller needs
// tenant.read to ask — and that is carried by the session rather than the body.
type ListRolesRequest struct{}

type RoleProto struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

type ListRolesResponse struct {
	Roles []RoleProto `json:"roles"`
}

// ListRoles is what an administrator may give somebody.
//
// The permissions come with each role rather than being left to a separate
// lookup, because "what does a supervisor actually get" is the question somebody
// assigning one is really asking.
func (h *Handler) ListRoles(ctx context.Context, req *connect.Request[ListRolesRequest]) (*connect.Response[ListRolesResponse], error) {
	out := make([]RoleProto, 0)
	for _, r := range h.svc.ListRoles() {
		out = append(out, RoleProto{Name: r.Name, Description: r.Description, Permissions: r.Permissions})
	}
	return connect.NewResponse(&ListRolesResponse{Roles: out}), nil
}

type AddMemberRequest struct {
	TenantID string `json:"tenant_id"`
	Email    string `json:"email"`
	FullName string `json:"full_name"`
	Role     string `json:"role"`
	// Password may be omitted. The person then exists and cannot sign in until an
	// administrator sets one, which is a real state and not an error.
	Password string `json:"password,omitempty"`
	Actor    string `json:"actor"`
}

type AddMemberResponse struct {
	UserID string `json:"user_id"`
	// Created is false when the address already had an account and this attached
	// it to the tenant instead of making a second one.
	Created bool `json:"created"`
}

func (h *Handler) AddMember(ctx context.Context, req *connect.Request[AddMemberRequest]) (*connect.Response[AddMemberResponse], error) {
	m := req.Msg
	out, err := h.svc.AddMember(ctx, service.AddMemberInput{
		TenantID: m.TenantID, Email: m.Email, FullName: m.FullName,
		Role: m.Role, Password: m.Password, Actor: m.Actor,
	})
	if err != nil {
		return nil, classifyAdmin(err)
	}
	return connect.NewResponse(&AddMemberResponse{UserID: out.UserID, Created: out.Created}), nil
}

type ListMembersRequest struct {
	TenantID string `json:"tenant_id"`
}

type MemberProto struct {
	UserID   string `json:"user_id"`
	Email    string `json:"email"`
	FullName string `json:"full_name"`
	Role     string `json:"role"`
	Status   string `json:"status"`
	// HasPassword rather than the hash, or any part of it. A listing that carries
	// a hash is a listing somebody pastes into a support ticket.
	HasPassword bool   `json:"has_password"`
	LastLoginAt string `json:"last_login_at,omitempty"`
}

type ListMembersResponse struct {
	Members []MemberProto `json:"members"`
}

func (h *Handler) ListMembers(ctx context.Context, req *connect.Request[ListMembersRequest]) (*connect.Response[ListMembersResponse], error) {
	list, err := h.svc.ListMembers(ctx, req.Msg.TenantID)
	if err != nil {
		return nil, classifyAdmin(err)
	}
	out := make([]MemberProto, 0, len(list))
	for _, m := range list {
		p := MemberProto{
			UserID: m.UserID, Email: m.Email, FullName: m.FullName,
			Role: m.RoleName, Status: m.Status, HasPassword: m.HasPassword,
		}
		if m.LastLoginAt != nil {
			p.LastLoginAt = m.LastLoginAt.UTC().Format(time.RFC3339)
		}
		out = append(out, p)
	}
	return connect.NewResponse(&ListMembersResponse{Members: out}), nil
}

type SetMemberRoleRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
	Role     string `json:"role"`
	Actor    string `json:"actor"`
}

type SetMemberRoleResponse struct{}

func (h *Handler) SetMemberRole(ctx context.Context, req *connect.Request[SetMemberRoleRequest]) (*connect.Response[SetMemberRoleResponse], error) {
	m := req.Msg
	if err := h.svc.SetMemberRole(ctx, m.TenantID, m.UserID, m.Role, m.Actor); err != nil {
		return nil, classifyAdmin(err)
	}
	return connect.NewResponse(&SetMemberRoleResponse{}), nil
}

type SetMemberStatusRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
	Status   string `json:"status"`
	Actor    string `json:"actor"`
}

type SetMemberStatusResponse struct{}

func (h *Handler) SetMemberStatus(ctx context.Context, req *connect.Request[SetMemberStatusRequest]) (*connect.Response[SetMemberStatusResponse], error) {
	m := req.Msg
	if err := h.svc.SetMemberStatus(ctx, m.TenantID, m.UserID, m.Status, m.Actor); err != nil {
		return nil, classifyAdmin(err)
	}
	return connect.NewResponse(&SetMemberStatusResponse{}), nil
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type ChangePasswordResponse struct{}

// ChangePassword is somebody changing their own.
//
// The only route here needing no permission: a person holding no role must still
// be able to change the credential they were handed, and the current password is
// what authorises the change.
//
// Whose password is read from the verified actor, never from the body. Taking a
// user_id from the request would mean a route with no permission requirement
// that names its own subject — anybody signed in could change anybody else's
// password, with the current one as the only obstacle. Read from the header
// there is no subject to choose: you can change yours.
func (h *Handler) ChangePassword(ctx context.Context, req *connect.Request[ChangePasswordRequest]) (*connect.Response[ChangePasswordResponse], error) {
	actor, err := tenantctx.ActorFrom(ctx)
	if err != nil || actor.ID == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated,
			errors.New("only a signed-in person can change their own password"))
	}
	if err := h.svc.ChangePassword(ctx, actor.ID, req.Msg.CurrentPassword, req.Msg.NewPassword); err != nil {
		return nil, classifyAdmin(err)
	}
	return connect.NewResponse(&ChangePasswordResponse{}), nil
}

type SetMemberPasswordRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
	Password string `json:"password"`
	Actor    string `json:"actor"`
}

type SetMemberPasswordResponse struct{}

func (h *Handler) SetMemberPassword(ctx context.Context, req *connect.Request[SetMemberPasswordRequest]) (*connect.Response[SetMemberPasswordResponse], error) {
	m := req.Msg
	if err := h.svc.SetMemberPassword(ctx, m.TenantID, m.UserID, m.Password, m.Actor); err != nil {
		return nil, classifyAdmin(err)
	}
	return connect.NewResponse(&SetMemberPasswordResponse{}), nil
}

type IssueServiceIdentityRequest struct {
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`
	// ExpiresAt is required. A credential with no end date is one nobody rotates,
	// and a default chosen here would be this platform setting a co-operative's
	// rotation policy without being asked.
	ExpiresAt string `json:"expires_at"`
	Actor     string `json:"actor"`
}

type IssueServiceIdentityResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Secret is shown once. The platform keeps a hash and cannot show it again —
	// a credential that can be read back is one that can be read back by whoever
	// reaches the database.
	Secret    string `json:"secret"`
	ExpiresAt string `json:"expires_at"`
}

func (h *Handler) IssueServiceIdentity(ctx context.Context, req *connect.Request[IssueServiceIdentityRequest]) (*connect.Response[IssueServiceIdentityResponse], error) {
	m := req.Msg
	var expires time.Time
	if m.ExpiresAt != "" {
		parsed, err := time.Parse(time.RFC3339, m.ExpiresAt)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				errors.New("expires_at must be an RFC3339 timestamp"))
		}
		expires = parsed
	}
	out, err := h.svc.IssueServiceIdentity(ctx, m.TenantID, m.Name, expires, m.Actor)
	if err != nil {
		return nil, classifyAdmin(err)
	}
	return connect.NewResponse(&IssueServiceIdentityResponse{
		ID: out.ID, Name: out.Name, Secret: out.Secret,
		ExpiresAt: out.ExpiresAt.UTC().Format(time.RFC3339),
	}), nil
}

type RevokeServiceIdentityRequest struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
	Actor    string `json:"actor"`
}

type RevokeServiceIdentityResponse struct{}

func (h *Handler) RevokeServiceIdentity(ctx context.Context, req *connect.Request[RevokeServiceIdentityRequest]) (*connect.Response[RevokeServiceIdentityResponse], error) {
	m := req.Msg
	if err := h.svc.RevokeServiceIdentity(ctx, m.TenantID, m.ID, m.Actor); err != nil {
		return nil, classifyAdmin(err)
	}
	return connect.NewResponse(&RevokeServiceIdentityResponse{}), nil
}

type ListServiceIdentitiesRequest struct {
	TenantID string `json:"tenant_id"`
}

type ServiceIdentityProto struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	ExpiresAt  string `json:"expires_at"`
	LastUsedAt string `json:"last_used_at,omitempty"`
}

type ListServiceIdentitiesResponse struct {
	ServiceIdentities []ServiceIdentityProto `json:"service_identities"`
}

func (h *Handler) ListServiceIdentities(ctx context.Context, req *connect.Request[ListServiceIdentitiesRequest]) (*connect.Response[ListServiceIdentitiesResponse], error) {
	list, err := h.svc.ListServiceIdentities(ctx, req.Msg.TenantID)
	if err != nil {
		return nil, classifyAdmin(err)
	}
	out := make([]ServiceIdentityProto, 0, len(list))
	for _, s := range list {
		p := ServiceIdentityProto{
			ID: s.ID, Name: s.Name, Status: s.Status,
			ExpiresAt: s.ExpiresAt.UTC().Format(time.RFC3339),
		}
		if s.LastUsedAt != nil {
			p.LastUsedAt = s.LastUsedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, p)
	}
	return connect.NewResponse(&ListServiceIdentitiesResponse{ServiceIdentities: out}), nil
}
