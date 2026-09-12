package handler

import (
	"net/http"

	"github.com/ppusapati/gavya/libs/integrity/authz"
)

// Middleware is this gateway's front door, without the proxying behind it.
//
// The modulith serves every service from one process, so there is nothing to
// proxy to — but everything the gateway does before proxying still has to
// happen, and has to happen the same way. Sharing the implementation is the
// point: two copies of "what makes a request trustworthy" is one copy that gets
// a fix and one that does not.
//
// It differs from the proxy path in exactly one way, and in the modulith's
// favour. The permissions go onto the request context as well as into a header.
// The header is there because connectjson reads the tenant and the actor from
// headers, and downstream code should not care which shape it is running in; the
// context is there because authz.Guard prefers it, and a value passed in memory
// between two halves of one process cannot be forged by anybody, which is the
// whole reason this deployment shape closes the gap that publishing
// twenty-nine ports opened.
func (h *Handler) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.authenticate(w, r) {
			return
		}
		if !h.authorise(w, r) {
			return
		}
		held := authz.ParseSet(r.Header.Get(authz.PermissionsHeader))
		next.ServeHTTP(w, r.WithContext(authz.WithSet(r.Context(), held)))
	})
}
