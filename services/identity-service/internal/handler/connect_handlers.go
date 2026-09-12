package handler

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"

	"github.com/ppusapati/gavya/services/identity-service/internal/domain"
	"github.com/ppusapati/gavya/services/identity-service/internal/service"
)

const ServiceName = "gavya.identity.v1.IdentityService"

type Handler struct{ svc *service.Service }

func New(svc *service.Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	route := func(method string, handler http.HandlerFunc) {
		mux.HandleFunc(connectjson.Procedure(ServiceName, method), handler)
	}
	route("SignIn", connectjson.Unary(h.SignIn))
	route("SignInService", connectjson.Unary(h.SignInService))
	route("VerifySession", connectjson.Unary(h.VerifySession))
	route("SignOut", connectjson.Unary(h.SignOut))
	route("SignOutEverywhere", connectjson.Unary(h.SignOutEverywhere))

	h.RegisterAdministration(mux)
}

type SignInRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	// TenantID is optional. Omitted, the tenant is taken from the person's
	// memberships when that is unambiguous, and the request is refused with the
	// list when it is not.
	TenantID string `json:"tenant_id,omitempty"`
}

type SignInResponse struct {
	SessionID string `json:"session_id"`
	TenantID  string `json:"tenant_id"`
	UserID    string `json:"user_id"`
	RoleID    string `json:"role_id,omitempty"`
	RoleName  string `json:"role_name,omitempty"`
	ExpiresAt string `json:"expires_at"`
}

// SignIn exchanges a password for a session.
//
// The request carries a tenant_id field like every other procedure in the
// platform, and here it means something different: not "act as this tenant" but
// "sign me in to this one of my tenants". connectjson puts it on the context as
// a scope, which is harmless — nothing this procedure touches before the tenant
// is settled goes through the policies.
func (h *Handler) SignIn(ctx context.Context, req *connect.Request[SignInRequest]) (*connect.Response[SignInResponse], error) {
	m := req.Msg
	res, err := h.svc.SignIn(ctx, service.SignInRequest{
		Email:     m.Email,
		Password:  m.Password,
		TenantID:  m.TenantID,
		IPAddress: clientIP(req.Header()),
		UserAgent: req.Header().Get("User-Agent"),
	})
	if err != nil {
		return nil, signInError(err)
	}
	return connect.NewResponse(&SignInResponse{
		SessionID: res.SessionID,
		TenantID:  res.TenantID,
		UserID:    res.UserID,
		RoleID:    res.RoleID,
		RoleName:  res.RoleName,
		ExpiresAt: res.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}), nil
}

type SignInServiceRequest struct {
	Name   string `json:"name"`
	Secret string `json:"secret"`
}

func (h *Handler) SignInService(ctx context.Context, req *connect.Request[SignInServiceRequest]) (*connect.Response[SignInResponse], error) {
	res, err := h.svc.SignInService(ctx, req.Msg.Name, req.Msg.Secret, clientIP(req.Header()))
	if err != nil {
		return nil, signInError(err)
	}
	return connect.NewResponse(&SignInResponse{
		SessionID: res.SessionID,
		TenantID:  res.TenantID,
		UserID:    res.UserID,
		ExpiresAt: res.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}), nil
}

type VerifySessionRequest struct {
	SessionID string `json:"session_id"`
}

type VerifySessionResponse struct {
	TenantID          string `json:"tenant_id"`
	UserID            string `json:"user_id,omitempty"`
	ServiceIdentityID string `json:"service_identity_id,omitempty"`
	// RoleName and Permissions are what the gateway turns into the headers every
	// service authorises against. Permissions is always present, empty included:
	// a reader should be able to tell "holds nothing" from "this reply is from a
	// version that did not say".
	RoleName    string   `json:"role_name,omitempty"`
	Permissions []string `json:"permissions"`
}

// VerifySession is what the gateway calls on every request. It is the step that
// turns the tenant from something the caller asserted into something that was
// checked.
func (h *Handler) VerifySession(ctx context.Context, req *connect.Request[VerifySessionRequest]) (*connect.Response[VerifySessionResponse], error) {
	v, err := h.svc.Verify(ctx, req.Msg.SessionID)
	if err != nil {
		if errors.Is(err, service.ErrNotSignedIn) {
			return nil, connect.NewError(connect.CodeUnauthenticated, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	perms := v.Permissions
	if perms == nil {
		perms = []string{}
	}
	return connect.NewResponse(&VerifySessionResponse{
		TenantID:          v.TenantID,
		UserID:            v.UserID,
		ServiceIdentityID: v.ServiceIdentityID,
		RoleName:          v.RoleName,
		Permissions:       perms,
	}), nil
}

type SignOutRequest struct {
	SessionID string `json:"session_id"`
	Reason    string `json:"reason,omitempty"`
}

type SignOutResponse struct{}

func (h *Handler) SignOut(ctx context.Context, req *connect.Request[SignOutRequest]) (*connect.Response[SignOutResponse], error) {
	if err := h.svc.SignOut(ctx, req.Msg.SessionID, req.Msg.Reason); err != nil {
		if errors.Is(err, service.ErrNotSignedIn) {
			return nil, connect.NewError(connect.CodeUnauthenticated, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&SignOutResponse{}), nil
}

type SignOutEverywhereRequest struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
	Reason   string `json:"reason,omitempty"`
}

type SignOutEverywhereResponse struct {
	SessionsEnded int64 `json:"sessions_ended"`
}

func (h *Handler) SignOutEverywhere(ctx context.Context, req *connect.Request[SignOutEverywhereRequest]) (*connect.Response[SignOutEverywhereResponse], error) {
	n, err := h.svc.SignOutEverywhere(ctx, req.Msg.TenantID, req.Msg.UserID, req.Msg.Reason)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return connect.NewResponse(&SignOutEverywhereResponse{SessionsEnded: n}), nil
}

// signInError maps a refusal to a status without widening what it says.
//
// Everything that could distinguish a real address from an unknown one comes
// back as the same Unauthenticated with the same message. The tenant errors are
// the exception and deliberately so: by the time they can happen the password
// has already been proved, so naming the tenants tells the caller nothing they
// did not just establish, and refusing without saying why would leave somebody
// who belongs to three societies with no way to sign in to any of them.
func signInError(err error) error {
	var amb *domain.AmbiguousTenantError
	switch {
	case errors.As(err, &amb):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, domain.ErrNotAMember), errors.Is(err, domain.ErrNoTenant):
		return connect.NewError(connect.CodePermissionDenied, err)
	case errors.Is(err, domain.ErrSignInFailed):
		return connect.NewError(connect.CodeUnauthenticated, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}

// clientIP prefers what a proxy said, because in every real deployment the
// connection this service sees comes from the gateway.
func clientIP(h http.Header) string {
	if v := h.Get("X-Forwarded-For"); v != "" {
		// The first entry is the original client; the rest are proxies.
		for i := 0; i < len(v); i++ {
			if v[i] == ',' {
				return trimSpace(v[:i])
			}
		}
		return trimSpace(v)
	}
	return h.Get("X-Real-Ip")
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
