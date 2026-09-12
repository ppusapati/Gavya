package authz

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// served is a handler that records whether it was reached.
type served struct{ reached bool }

func (s *served) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.reached = true
	w.WriteHeader(http.StatusOK)
}

func call(t *testing.T, g http.Handler, path string, perms string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
	if perms != "" {
		r.Header.Set(PermissionsHeader, perms)
	}
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	return w
}

// A caller holding the permission gets through; one holding nothing does not.
func TestTheGuardLetsThroughWhatTheRoleAllowsAndNothingElse(t *testing.T) {
	const approve = "/settlement.v1.SettlementService/ApproveCycle"

	inner := &served{}
	if w := call(t, Guard(inner), approve, string(SettlementApprove)); w.Code != http.StatusOK {
		t.Errorf("a caller holding settlement.approve got %d approving a cycle", w.Code)
	}
	if !inner.reached {
		t.Error("the request was allowed and never reached the handler")
	}

	inner = &served{}
	w := call(t, Guard(inner), approve, string(SettlementRead))
	if w.Code != http.StatusForbidden {
		t.Errorf("a caller holding only settlement.read got %d approving a cycle, want 403", w.Code)
	}
	if inner.reached {
		t.Error("the request was refused and the handler ran anyway — a refusal that " +
			"happens after the work is not a refusal")
	}

	inner = &served{}
	if w := call(t, Guard(inner), approve, ""); w.Code != http.StatusForbidden {
		t.Errorf("a caller presenting no permissions at all got %d, want 403", w.Code)
	}
	if inner.reached {
		t.Error("a caller with no permissions reached the handler")
	}
}

// The refusal says which permission was wanted.
func TestARefusedRequestNamesThePermission(t *testing.T) {
	w := call(t, Guard(&served{}), "/settlement.v1.SettlementService/ApproveCycle",
		string(SettlementRead))

	var body struct{ Code, Message string }
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("the refusal is not JSON: %v", err)
	}
	if body.Code != "permission_denied" {
		t.Errorf("code is %q, want permission_denied", body.Code)
	}
	if !strings.Contains(body.Message, "settlement.approve") {
		t.Errorf("message is %q and does not name the permission somebody needs to "+
			"be given", body.Message)
	}
}

// A path shaped like a procedure but declared nowhere is refused, not passed on.
//
// This is the fail-closed property, at the layer that actually serves traffic.
// Passing it through to the mux would leave a route added without a permission
// standing open, which is the whole thing being prevented.
func TestAProcedureNobodyDeclaredIsRefusedRatherThanServed(t *testing.T) {
	inner := &served{}
	w := call(t, Guard(inner), "/invented.v1.InventedService/DoSomething", "")

	if inner.reached {
		t.Fatal("an undeclared procedure reached the handler")
	}
	if w.Code != http.StatusNotImplemented {
		t.Errorf("an undeclared procedure answered %d, want 501 — it is a gap in the "+
			"platform rather than a role that needs widening, and the two lead "+
			"somewhere different", w.Code)
	}
}

// Everything that is not a procedure is left alone.
//
// /healthz has to answer before anybody has signed in, or a deployment cannot
// tell whether the process is alive.
func TestHealthAndAnythingNotAProcedurePassThrough(t *testing.T) {
	for _, path := range []string{"/healthz", "/metrics", "/", "/readyz"} {
		inner := &served{}
		w := call(t, Guard(inner), path, "")
		if !inner.reached {
			t.Errorf("%s was intercepted by the guard; it is not a procedure", path)
		}
		if w.Code != http.StatusOK {
			t.Errorf("%s answered %d", path, w.Code)
		}
	}
}

// The context beats the header.
//
// In the modulith the gateway puts the permissions on the context in the same
// process. Nothing should be able to widen that by also sending a header.
func TestTheContextIsPreferredToTheHeader(t *testing.T) {
	inner := &served{}
	guard := Guard(inner)

	r := httptest.NewRequest(http.MethodPost,
		"/settlement.v1.SettlementService/ApproveCycle", strings.NewReader("{}"))
	// A header claiming everything, and a context holding only a read.
	r.Header.Set(PermissionsHeader, string(SettlementApprove))
	r = r.WithContext(WithSet(r.Context(), NewSet(SettlementRead)))

	w := httptest.NewRecorder()
	guard.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("a header claiming settlement.approve overrode a context holding "+
			"only settlement.read: got %d, want 403", w.Code)
	}
	if inner.reached {
		t.Error("the forged header reached the handler")
	}
}

// Each role can call what it is for, through the guard rather than through
// Decide directly.
//
// The same separations the model test states, exercised over HTTP, because a
// model that is right and a middleware that does not consult it would both pass
// their own tests.
func TestTheSeparationsHoldOverHTTP(t *testing.T) {
	roles := Roles()
	for _, c := range []struct {
		role, path string
		want       int
	}{
		{"collector", "/milk.v1.MilkService/RecordMilk", http.StatusOK},
		{"collector", "/procurement.v1.ProcurementService/CorrectCollection", http.StatusForbidden},
		{"collector", "/settlement.v1.SettlementService/ApproveCycle", http.StatusForbidden},
		{"supervisor", "/procurement.v1.ProcurementService/CorrectCollection", http.StatusOK},
		{"supervisor", "/settlement.v1.SettlementService/ApproveCycle", http.StatusForbidden},
		{"accountant", "/settlement.v1.SettlementService/ApproveCycle", http.StatusOK},
		{"accountant", "/milk.v1.MilkService/RecordMilk", http.StatusForbidden},
		{"auditor", "/settlement.v1.SettlementService/GetCycle", http.StatusOK},
		{"auditor", "/settlement.v1.SettlementService/ApproveCycle", http.StatusForbidden},
		{"admin", "/settlement.v1.SettlementService/ApproveCycle", http.StatusOK},
	} {
		w := call(t, Guard(&served{}), c.path, roles[c.role].Permissions.String())
		if w.Code != c.want {
			t.Errorf("%s calling %s got %d, want %d", c.role, c.path, w.Code, c.want)
		}
	}
}
