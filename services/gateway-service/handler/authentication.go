package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/authz"
	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/libs/integrity/ratelimit"
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
	authz.PermissionsHeader,
	authz.RoleHeader,
	ratelimit.ClientHeader,
	// The two a client would otherwise use to choose its own address. They are
	// read by the sign-in log and by the rate limiter, and a value the caller
	// picks is no use to either: an attacker rotates X-Forwarded-For and every
	// per-address limit becomes a per-request limit, which is none.
	//
	// A deployment with a real proxy in front of this gateway would need to trust
	// that proxy's X-Forwarded-For, by configuration, naming it. Stripping is the
	// right default until somebody says otherwise: an inherited header is trusted
	// by accident, and a configured one is trusted on purpose.
	"X-Forwarded-For",
	"X-Real-Ip",
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
	// Permissions is what the session's role may do. Empty is meaningful and is
	// not an error: a member with no role holds nothing, and every procedure
	// then refuses them, which is the right answer rather than a reason to fail
	// the sign-in.
	Permissions authz.Set
	// RoleName is carried for the audit trail. Nothing decides on it.
	RoleName string
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
		TenantID          string   `json:"tenant_id"`
		UserID            string   `json:"user_id"`
		ServiceIdentityID string   `json:"service_identity_id"`
		RoleName          string   `json:"role_name"`
		Permissions       []string `json:"permissions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Identity{}, fmt.Errorf("identity service replied with something unreadable: %w", err)
	}
	held := authz.Set{}
	for _, p := range out.Permissions {
		held[authz.Permission(p)] = struct{}{}
	}
	return Identity{
		TenantID:          out.TenantID,
		UserID:            out.UserID,
		ServiceIdentityID: out.ServiceIdentityID,
		RoleName:          out.RoleName,
		Permissions:       held,
	}, nil
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

// unauthenticated is the set of paths that must work without a session, because
// they are how a session is obtained or how the platform is watched.
//
// A list rather than a catch-all on the identity service: everything under the
// identity package would include the administrative procedures, and those need a
// session like anything else.
//
// # WHY /readyz AND /metrics ARE HERE
//
// They were not, and in the separate-process deployment it did not show. Each
// service serves its own probes and there is no gateway in front of them; this
// middleware only ever saw proxied procedure calls.
//
// The modulith puts this middleware in front of the whole mux, probes included —
// which is the point of that shape, one door — and so it answered /readyz with
// 401. A Kubernetes readiness probe never succeeds, the pod never joins the
// Service, and the rollout hangs while the process sits there perfectly healthy;
// Prometheus scrapes 401 and the platform is unwatched in the one shape that
// ships. Nothing caught it because nothing had ever started the modulith: the
// five tests it had all asked whether this middleware was wired in the right
// order, and a wiring test cannot tell a middleware that refuses the probes from
// one that lets them through.
//
// Adding them is not a new exposure. Every service in the other shape already
// serves both without a session — that is how the cluster probes them and how
// the scrape configuration reads them — so this makes the two shapes agree
// rather than widening either. Neither reveals a tenant's data: /readyz names
// the modules that cannot reach their database, and /metrics is procedure names
// and counts.
var unauthenticated = []string{
	"/healthz",
	"/readyz",
	"/metrics",
	"/gavya.identity.v1.IdentityService/SignIn",
	"/gavya.identity.v1.IdentityService/SignInService",
	// The step that checks a session cannot itself require one.
	//
	// In the separate-process deployment the gateway calls identity-service at
	// its own address and never passes through itself, so this did not arise. In
	// the modulith IDENTITY_SERVICE_URL is this process — deliberately, so that
	// session verification has one implementation rather than a second path only
	// one shape uses — and the verifier's own call arrived back at this
	// middleware, which refused it. The verifier reads that 401 as "not signed
	// in" and answers the caller the same way, so every authenticated request in
	// the shape that ships was refused with the one message that sends somebody
	// to check their password.
	//
	// No new exposure: whoever can present a session id can already use it for
	// everything it grants, and this answers with nothing more than that.
	"/gavya.identity.v1.IdentityService/VerifySession",
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

	// And the address it is actually connected from, which is the one thing
	// about a caller that cannot be chosen by the caller. Set before the
	// unauthenticated paths are let through, because sign-in is exactly where it
	// is needed.
	r.Header.Set(ratelimit.ClientHeader, ratelimit.PeerAddress(r))

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
	// Set even when empty, so what reads the header downstream reads this
	// gateway's answer rather than the absence of one. strip removed whatever
	// the client sent, and an absent header and an empty one would then mean the
	// same thing to a reader and different things to a careless one.
	r.Header.Set(authz.PermissionsHeader, id.Permissions.String())
	// Set unconditionally, so this header always carries this gateway's answer
	// rather than sometimes carrying nothing.
	//
	// It is not what keeps a client's own role claim out — strip does that, and
	// measured: restoring the `if id.RoleName != ""` guard breaks no test,
	// because by this point the client's value is already gone. What this buys
	// is that the header stops depending on strip alone. Worth having for a
	// value nothing decides on but an audit entry is written from: a forged one
	// would put "approved by a supervisor" against something a supervisor did
	// not do, which is harder to notice than a wrong figure.
	r.Header.Set(authz.RoleHeader, id.RoleName)
	return true
}

// authorise refuses a request whose session may not call the procedure.
//
// The services check this too, and deliberately: this one fails fast at the
// edge, before a request crosses the network to an upstream that would only
// refuse it, and the one inside the service is the one that still holds if
// anything ever reaches it without passing through here.
//
// It reads the header this gateway has just set rather than the Identity it set
// it from, so there is a single answer to "what may this caller do" and both
// layers are reading the same one. A second copy of that fact is a second thing
// that can be wrong.
func (h *Handler) authorise(w http.ResponseWriter, r *http.Request) bool {
	// The paths that work without a session are not procedures and have no
	// permission to decide about.
	//
	// authenticate already lets them past and this did not, which was harmless
	// in the separate-process deployment — this middleware only ever saw proxied
	// procedure calls there — and broke the modulith outright. /healthz answered
	// 501, so a Kubernetes liveness probe fails, and a failing liveness probe
	// kills the pod: the one process holding the whole platform in a restart
	// loop, with the readiness probe answering 501 beside it so nothing said
	// why.
	//
	// authz.Guard, one layer further in, already gets this right — it decides
	// only about paths shaped like a procedure and leaves everything else alone.
	// This is the same rule, in the one place that did not have it.
	if isUnauthenticated(r.URL.Path) {
		return true
	}

	err := authz.Decide(r.URL.Path, authz.ParseSet(r.Header.Get(authz.PermissionsHeader)))
	if err == nil {
		return true
	}
	var unknown *authz.ErrUnknownProcedure
	if errors.As(err, &unknown) {
		// Nobody declared who may call this. Not the caller's fault, and not a
		// role that wants widening.
		h.log.Errorf("refusing %s: no permission is declared for it", r.URL.Path)
		writeStatus(w, http.StatusNotImplemented, "unimplemented", err.Error())
		return false
	}
	writeStatus(w, http.StatusForbidden, "permission_denied", err.Error())
	return false
}

func writeUnauthorised(w http.ResponseWriter, message string) {
	writeStatus(w, http.StatusUnauthorized, "unauthenticated", message)
}

func writeStatus(w http.ResponseWriter, code int, connectCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": connectCode, "message": message})
}
