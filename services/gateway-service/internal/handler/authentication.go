package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
)

// The header the platform's services read the verified tenant from.
//
// It is set here and only here. Anything arriving on an inbound request under
// this name is removed before the request is forwarded — see strip below, which
// is the single most important line in this file.
const tenantHeader = connectjson.TenantHeader

// Headers this gateway asserts on behalf of a verified caller. A client that
// sends any of them is not making a claim, it is attempting one.
var assertedHeaders = []string{
	tenantHeader,
	"X-Gavya-User",
	"X-Gavya-Service-Identity",
}

// Verifier turns a session into the tenant it acts for.
type Verifier interface {
	Verify(ctx context.Context, sessionID string) (Identity, error)
}

// Identity is what a session proved.
type Identity struct {
	TenantID          string
	UserID            string
	ServiceIdentityID string
}

// identityVerifier asks the identity service, over the same Connect JSON
// protocol everything else in the platform speaks.
//
// Deliberately not cached. A cache would mean a session stays usable for the
// length of its time-to-live after being revoked, which gives back exactly the
// property that made sessions rows rather than signatures — dismissal, a stolen
// credential and a lost laptop all become unhandleable again, for a window
// somebody chose for performance reasons. If the extra hop becomes a problem the
// answer is to move the check closer to the database, not to remember its
// answer.
type identityVerifier struct {
	url    string
	client *http.Client
}

func newIdentityVerifier(url string) *identityVerifier {
	return &identityVerifier{
		url: strings.TrimRight(url, "/"),
		// Short. A gateway that blocks on identity is a gateway that is down.
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (v *identityVerifier) Verify(ctx context.Context, sessionID string) (Identity, error) {
	body, _ := json.Marshal(map[string]string{"session_id": sessionID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		v.url+"/gavya.identity.v1.IdentityService/VerifySession", bytes.NewReader(body))
	if err != nil {
		return Identity{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := v.client.Do(req)
	if err != nil {
		return Identity{}, fmt.Errorf("identity service unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return Identity{}, errNotSignedIn
	}
	if resp.StatusCode != http.StatusOK {
		return Identity{}, fmt.Errorf("identity service returned %d", resp.StatusCode)
	}

	var out struct {
		TenantID          string `json:"tenant_id"`
		UserID            string `json:"user_id"`
		ServiceIdentityID string `json:"service_identity_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Identity{}, fmt.Errorf("identity service replied with something unreadable: %w", err)
	}
	return Identity(out), nil
}

var errNotSignedIn = fmt.Errorf("not signed in")

// strip removes the headers this gateway asserts, from every inbound request,
// before anything else looks at them.
//
// This is what makes the rest of it worth anything. Downstream services trust
// the tenant header because the gateway sets it from a verified session; if a
// client could send it themselves, the header would be exactly as trustworthy
// as the request body it replaced, and the whole chain — sessions, policies,
// membership — would be decoration around a value the caller chose.
//
// Done unconditionally, including on the paths that need no authentication, so
// there is no route through this gateway on which a client-supplied value
// survives. That is not belt-and-braces, it is the only defence on those paths:
// nothing downstream of here sets a header on a request that was never
// authenticated.
//
// On the authenticated paths the tenant header would be overwritten anyway,
// since it is always Set from the verified session. The other two would not: a
// service identity is only set when the session names one, so a client that sent
// X-Gavya-Service-Identity on a user's session would have it forwarded intact
// and be read downstream as that service. Removing all three unconditionally
// means no such asymmetry has to be reasoned about again.
func strip(r *http.Request) {
	for _, h := range assertedHeaders {
		r.Header.Del(h)
	}
}

// sessionFrom reads the session out of a request.
//
// Both a bearer token and a cookie, because the browser client and the
// service-to-service callers want different things and neither should have to
// pretend to be the other.
func sessionFrom(r *http.Request) string {
	if a := r.Header.Get("Authorization"); a != "" {
		const bearer = "bearer "
		if len(a) > len(bearer) && strings.EqualFold(a[:len(bearer)], bearer) {
			return strings.TrimSpace(a[len(bearer):])
		}
	}
	if c, err := r.Cookie("gavya_session"); err == nil {
		return c.Value
	}
	return ""
}

// unauthenticated is the set of path prefixes that must work without a session,
// because they are how a session is obtained or how the gateway is monitored.
//
// A prefix list rather than a catch-all on the identity service: everything
// under the identity package would include the administrative procedures, and
// those need a session like anything else.
var unauthenticated = []string{
	"/healthz",
	"/gavya.identity.v1.IdentityService/SignIn",
	"/gavya.identity.v1.IdentityService/SignInService",
}

func isUnauthenticated(path string) bool {
	for _, p := range unauthenticated {
		if path == p {
			return true
		}
	}
	return false
}

// authenticate resolves the caller and stamps the request, or refuses it.
//
// Returns false when the request has been answered and must not be forwarded.
func (h *Handler) authenticate(w http.ResponseWriter, r *http.Request) bool {
	// First, always: whatever the client sent under these names is gone.
	strip(r)

	if isUnauthenticated(r.URL.Path) {
		return true
	}
	if h.verifier == nil {
		// No identity service configured. Refusing is the only safe reading:
		// forwarding unauthenticated would mean every service falls back to
		// taking the tenant from the request body, which is the arrangement this
		// replaced.
		h.log.Errorf("refusing %s: no identity service is configured", r.URL.Path)
		writeUnauthorised(w, "this gateway has no identity service configured")
		return false
	}

	session := sessionFrom(r)
	if session == "" {
		writeUnauthorised(w, "not signed in")
		return false
	}

	id, err := h.verifier.Verify(r.Context(), session)
	if err != nil {
		if err == errNotSignedIn {
			writeUnauthorised(w, "not signed in")
			return false
		}
		// The identity service being unreachable is not the caller's fault and
		// must not be reported as a failed sign-in — that would send everybody
		// to reset a password during an outage.
		h.log.Errorf("could not verify a session: %v", err)
		writeStatus(w, http.StatusServiceUnavailable, "unavailable",
			"the identity service could not be reached")
		return false
	}

	if id.TenantID == "" {
		// A session that verified but named no tenant would be forwarded with an
		// empty header, which downstream reads as an unscoped connection and the
		// policies refuse — correct, and unreadable at the far end.
		//
		// Checked here rather than inside the verifier, because here is where
		// every verifier passes: a check in one implementation is a check the
		// next implementation does not have.
		h.log.Errorf("a session verified but named no tenant; refusing %s", r.URL.Path)
		writeStatus(w, http.StatusInternalServerError, "internal",
			"the session could not be scoped to a tenant")
		return false
	}

	r.Header.Set(tenantHeader, id.TenantID)
	if id.UserID != "" {
		r.Header.Set("X-Gavya-User", id.UserID)
	}
	if id.ServiceIdentityID != "" {
		r.Header.Set("X-Gavya-Service-Identity", id.ServiceIdentityID)
	}
	return true
}

func writeUnauthorised(w http.ResponseWriter, message string) {
	writeStatus(w, http.StatusUnauthorized, "unauthenticated", message)
}

func writeStatus(w http.ResponseWriter, code int, connectCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": connectCode, "message": message})
}
