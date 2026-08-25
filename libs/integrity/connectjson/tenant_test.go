package connectjson

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/tenantctx"
)

// The two spellings that actually occur across these services. Matching on the
// Go field name would find one and miss the other.
type reqTenantID struct {
	TenantID string `json:"tenant_id"`
	Note     string `json:"note"`
}

type reqTenantId struct {
	TenantId string `json:"tenant_id"`
}

type reqNoTenant struct {
	Note string `json:"note"`
}

type reply struct {
	Tenant string `json:"tenant"`
}

// serve runs one request through the real handler and reports the tenant the
// procedure saw on its context.
func serve[Req any](t *testing.T, body string, headers map[string]string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	var seen string
	h := Unary(func(ctx context.Context, _ *connect.Request[Req]) (*connect.Response[reply], error) {
		if v, err := tenantctx.From(ctx); err == nil {
			seen = v
		}
		return connect.NewResponse(&reply{Tenant: seen}), nil
	})
	r := httptest.NewRequest(http.MethodPost, "/svc/Method", strings.NewReader(body))
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h(w, r)
	return w, seen
}

func TestTheTenantInTheBodyReachesTheContext(t *testing.T) {
	w, seen := serve[reqTenantID](t, `{"tenant_id":"T_ALPHA","note":"x"}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if seen != "T_ALPHA" {
		t.Errorf("the procedure saw tenant %q, want T_ALPHA", seen)
	}
}

// The Go spelling of the field varies across these services and the JSON name
// does not. Matching on the Go name would scope one service and silently leave
// the other unscoped.
func TestTheOtherGoSpellingIsFoundToo(t *testing.T) {
	_, seen := serve[reqTenantId](t, `{"tenant_id":"T_BETA"}`, nil)
	if seen != "T_BETA" {
		t.Errorf("the procedure saw tenant %q, want T_BETA", seen)
	}
}

// A verified tenant, once there is something in front of these services to
// verify it, outranks whatever the client wrote in its own body.
func TestAVerifiedTenantOutranksTheBody(t *testing.T) {
	_, seen := serve[reqTenantID](t, `{"tenant_id":"T_ALPHA"}`,
		map[string]string{TenantHeader: "T_ALPHA"})
	if seen != "T_ALPHA" {
		t.Errorf("the procedure saw %q", seen)
	}
}

// A caller that is authenticated as one tenant and asks for another is refused,
// not quietly resolved in either direction. Picking the header would be silently
// ignoring what the client asked for; picking the body would be handing over
// somebody else's data.
func TestARequestThatAsksForADifferentTenantThanItProvesIsRefused(t *testing.T) {
	w, seen := serve[reqTenantID](t, `{"tenant_id":"T_BETA"}`,
		map[string]string{TenantHeader: "T_ALPHA"})

	if w.Code != http.StatusForbidden {
		t.Errorf("status %d, want 403", w.Code)
	}
	if seen != "" {
		t.Errorf("the procedure ran anyway, scoped to %q", seen)
	}
	var body errorBody
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if !strings.Contains(body.Message, "T_ALPHA") || !strings.Contains(body.Message, "T_BETA") {
		t.Errorf("the refusal does not name both tenants: %q", body.Message)
	}
}

// A request with no tenant anywhere is not refused here. It reaches the
// procedure on an unscoped context and the database refuses the first query —
// which is the right place, because a procedure that genuinely has no tenant
// (health, readiness) must still work.
func TestARequestWithNoTenantIsLeftForTheDatabaseToRefuse(t *testing.T) {
	w, seen := serve[reqNoTenant](t, `{"note":"x"}`, nil)
	if w.Code != http.StatusOK {
		t.Errorf("status %d, want the request to reach the procedure", w.Code)
	}
	if seen != "" {
		t.Errorf("a tenant appeared from nowhere: %q", seen)
	}
}

// An empty tenant in the body is the same as none, and must not be put on the
// context — where it would read as a scoped connection carrying no scope.
func TestAnEmptyTenantInTheBodyIsNotATenant(t *testing.T) {
	_, seen := serve[reqTenantID](t, `{"tenant_id":""}`, nil)
	if seen != "" {
		t.Errorf("an empty tenant reached the context as %q", seen)
	}
}

func TestATenantThatCannotBeOneIsRejectedAtTheEdge(t *testing.T) {
	w, seen := serve[reqTenantID](t, `{"tenant_id":"T_\nALPHA"}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400", w.Code)
	}
	if seen != "" {
		t.Errorf("the procedure ran scoped to %q", seen)
	}
}
