package handler

import (
	"net/http"
	"strconv"
	"strings"
)

// The browser workspaces are served from their own origin — a dev server during
// development, a static host in production — while every procedure they call is
// on the gateway. A cross-origin POST carrying Content-Type: application/json
// and X-Tenant-ID is not a simple request, so without a preflight answer the
// browser never sends it at all and the workspace sees only a network failure.
//
// Origins are matched exactly against a configured list. Nothing is reflected
// back unchecked, and Access-Control-Allow-Credentials is never sent: the
// gateway carries no cookies or credentials in Phase-1, so granting them would
// widen the browser's trust for nothing in return.

// corsHeaders are the request headers a workspace actually sends. Listing them
// rather than echoing Access-Control-Request-Headers keeps the answer a
// statement about this gateway instead of an echo of whatever was asked for.
var corsHeaders = strings.Join([]string{
	"Content-Type",
	"X-Tenant-ID",
	"X-Request-ID",
	"Connect-Protocol-Version",
	"Connect-Timeout-Ms",
}, ", ")

const corsMaxAge = 600 // seconds

// CORS wraps next so that browsers on a configured origin may call it.
//
// With no origins configured the wrapper is transparent: no header is added and
// the gateway stays same-origin only, which is what a deployment that serves the
// workspace from the gateway itself wants.
func (h *Handler) CORS(next http.Handler) http.Handler {
	allowed := make(map[string]bool, len(h.cfg.CORSAllowedOrigins))
	for _, o := range h.cfg.CORSAllowedOrigins {
		if o = strings.TrimSpace(o); o != "" {
			allowed[o] = true
		}
	}
	if len(allowed) == 0 {
		return next
	}
	h.log.Infof("gateway accepting browser calls from %d origin(s)", len(allowed))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Vary is set whenever an Origin was offered, allowed or not: the reply
		// depends on it, and a cache that missed that would serve one origin's
		// answer to another.
		if origin != "" {
			w.Header().Add("Vary", "Origin")
		}

		if origin == "" || !allowed[origin] {
			// An unapproved origin gets no CORS header, so the browser blocks it.
			// A preflight still ends here rather than reaching a procedure.
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", origin)

		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", corsHeaders)
			w.Header().Set("Access-Control-Max-Age", strconv.Itoa(corsMaxAge))
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
