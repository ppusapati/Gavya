package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func mux(t *testing.T) *http.ServeMux {
	t.Helper()
	m := http.NewServeMux()
	New(nil).Register(m)
	return m
}

func TestEveryProcedureIsRouted(t *testing.T) {
	procedures := []string{
		"CreateWarehouse",
		"GetWarehouse",
		"ListWarehouses",
		"AdjustStock",
		"ListStockMovements",
		"CreateBatch",
		"GetBatch",
		"ListExpiringBatches",
	}

	m := mux(t)
	for _, p := range procedures {
		path := "/" + ServiceName + "/" + p
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")

		func() {
			defer func() { _ = recover() }()
			m.ServeHTTP(httptest.NewRecorder(), req)
		}()

		// A handler is free to answer 404 on its own terms, so routing is
		// judged by whether the mux matched a pattern rather than by status.
		if _, pattern := m.Handler(req); pattern == "" {
			t.Errorf("%s is not routed", path)
		}
	}
}

func TestMalformedJSONIsInvalidArgumentNotInternal(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/"+ServiceName+"/GetWarehouse",
		strings.NewReader(`{not json`))
	req.Header.Set("Content-Type", "application/json")
	mux(t).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; a 5xx would be retried by clients", rec.Code)
	}

	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body is not JSON: %v", err)
	}
	if body.Code != "invalid_argument" {
		t.Errorf("code = %q, want invalid_argument", body.Code)
	}
	if body.Message == "" {
		t.Error("error body carries no message")
	}
}

func TestGetOnAProcedureIsRejected(t *testing.T) {
	rec := httptest.NewRecorder()
	mux(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+ServiceName+"/GetWarehouse", nil))

	if rec.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501 for a non-POST on a unary procedure", rec.Code)
	}
}

func TestHealthzStillWorks(t *testing.T) {
	rec := httptest.NewRecorder()
	mux(t).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestUnknownProcedureIsNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	mux(t).ServeHTTP(rec, httptest.NewRequest(http.MethodPost,
		"/"+ServiceName+"/NoSuchMethod", strings.NewReader(`{}`)))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
