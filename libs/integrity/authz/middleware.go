package authz

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
)

// PermissionsHeader carries what the caller may do, set by the gateway from a
// verified session.
//
// It sits beside X-Gavya-Tenant and is stripped from inbound requests by the
// same code, for the same reason: a client that sends it is not making a claim,
// it is attempting one.
const PermissionsHeader = "X-Gavya-Permissions"

// RoleHeader carries the name of the role those permissions came from.
//
// Nothing decides on it — every decision is made on the permissions — but an
// audit entry reading "approved by a supervisor" is worth more to somebody
// reading it in two years than one reading "approved by a caller holding
// settlement.approve".
const RoleHeader = "X-Gavya-Role"

type setKey struct{}

// WithSet puts a caller's permissions on the context.
//
// This is how the modulith passes them: the gateway middleware and the service
// handlers are the same process, so nothing has to be serialised into a header
// that something else could forge.
func WithSet(ctx context.Context, s Set) context.Context {
	return context.WithValue(ctx, setKey{}, s)
}

// FromContext reads the permissions a caller holds, or nil.
func FromContext(ctx context.Context) Set {
	s, _ := ctx.Value(setKey{}).(Set)
	return s
}

// procedureShape matches a Connect unary path: /<package>.<version>.<Service>/<Method>.
//
// Guard uses it to tell a procedure from the other things a service serves.
// Anything shaped like a procedure must have a declared permission or it is
// refused; anything that is not — /healthz, and whatever is added beside it —
// is left alone. The alternative, passing undeclared paths through to the mux,
// would mean a route added without a permission stands open, which is the
// failure this package exists to prevent.
var procedureShape = regexp.MustCompile(`^/[a-zA-Z0-9_.]+\.[A-Za-z0-9]+Service/[A-Za-z0-9]+$`)

// Guard refuses a request whose caller lacks the permission its procedure needs.
//
// It wraps a service's whole mux, so every procedure that service serves is
// covered by construction — there is no per-handler step to forget. That is the
// same reason scopeToTenant lives in connectjson rather than in each handler.
//
// Where the permissions come from, and what that costs:
//
// The context is preferred. In the modulith the gateway's own middleware runs
// first in the same process and puts them there, so no header is involved and
// there is nothing to forge.
//
// The header is the fallback, for a deployment where the services are separate
// processes behind the gateway. It is only as trustworthy as the network: a
// service reachable directly can be sent any permissions at all. That is not a
// hole this middleware can close — it is closed by the gateway being the only
// reachable door, which is what the modulith arranges and what a NetworkPolicy
// would arrange in a cluster. Written down here because a reader is entitled to
// know which of these two paths they are on.
func Guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !procedureShape.MatchString(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		held := FromContext(r.Context())
		if held == nil {
			held = ParseSet(r.Header.Get(PermissionsHeader))
		}

		if err := Decide(r.URL.Path, held); err != nil {
			refuse(w, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// refuse answers in the shape everything else in this platform answers errors
// in, so one client decoder handles it.
func refuse(w http.ResponseWriter, err error) {
	code, status := "permission_denied", http.StatusForbidden
	if _, unknown := err.(*ErrUnknownProcedure); unknown {
		// Not the caller's fault: nobody decided who may call this. Reported as
		// unimplemented rather than forbidden so it reads as a gap in the
		// platform, which is what it is, and so it is not mistaken for a role
		// that needs widening.
		code, status = "unimplemented", http.StatusNotImplemented
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: code, Message: err.Error()})
}
