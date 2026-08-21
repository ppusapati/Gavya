package svcclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/connectjson"
)

// These drive the real serving adapter through the real client. Each side has
// its own idea of how a Connect code crosses the wire, and only a round trip
// proves the two agree.

type echoRequest struct {
	Value  string `json:"value"`
	Fail   string `json:"fail,omitempty"`
	Tenant string `json:"tenant,omitempty"`
}

type echoResponse struct {
	Value     string `json:"value"`
	SawTenant string `json:"saw_tenant"`
}

const testProcedure = "test.v1.EchoService/Echo"

func serve(t *testing.T, handler func(context.Context, *connect.Request[echoRequest]) (*connect.Response[echoResponse], error)) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/"+testProcedure, connectjson.Unary(handler))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return New(Config{BaseURL: srv.URL, Timeout: 5 * time.Second})
}

func echo(ctx context.Context, req *connect.Request[echoRequest]) (*connect.Response[echoResponse], error) {
	if code := req.Msg.Fail; code != "" {
		return nil, connect.NewError(codeByName[code], errors.New("deliberate failure: "+code))
	}
	return connect.NewResponse(&echoResponse{
		Value:     req.Msg.Value,
		SawTenant: req.Header().Get(HeaderTenantID),
	}), nil
}

func TestRoundTrip(t *testing.T) {
	c := serve(t, echo)

	out, err := Call[echoRequest, echoResponse](context.Background(), c, testProcedure,
		echoRequest{Value: "hello"}, CallOptions{TenantID: "tnt-1"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out.Value != "hello" {
		t.Errorf("value = %q, want hello", out.Value)
	}
}

// The tenant must reach the handler: every service logs and scopes against it.
func TestTenantHeaderReachesTheHandler(t *testing.T) {
	c := serve(t, echo)

	out, err := Call[echoRequest, echoResponse](context.Background(), c, testProcedure,
		echoRequest{Value: "x"}, CallOptions{TenantID: "tnt-42"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if out.SawTenant != "tnt-42" {
		t.Errorf("handler saw tenant %q, want tnt-42", out.SawTenant)
	}
}

// A code chosen by the callee must arrive as that same code, or callers branch
// on the wrong thing.
func TestEveryCodeSurvivesTheRoundTrip(t *testing.T) {
	c := serve(t, echo)

	cases := []struct {
		name string
		code connect.Code
	}{
		{"invalid_argument", connect.CodeInvalidArgument},
		{"not_found", connect.CodeNotFound},
		{"already_exists", connect.CodeAlreadyExists},
		{"permission_denied", connect.CodePermissionDenied},
		{"failed_precondition", connect.CodeFailedPrecondition},
		{"resource_exhausted", connect.CodeResourceExhausted},
		{"unimplemented", connect.CodeUnimplemented},
		{"internal", connect.CodeInternal},
		{"unavailable", connect.CodeUnavailable},
	}

	for _, tc := range cases {
		_, err := Call[echoRequest, echoResponse](context.Background(), c, testProcedure,
			echoRequest{Fail: tc.name}, CallOptions{TenantID: "tnt-1"})
		if err == nil {
			t.Errorf("%s: expected an error", tc.name)
			continue
		}
		var svcErr *Error
		if !errors.As(err, &svcErr) {
			t.Errorf("%s: got %v, want a structured *Error", tc.name, err)
			continue
		}
		if svcErr.Code != tc.code {
			t.Errorf("%s: code = %s, want %s", tc.name, svcErr.Code, tc.code)
		}
		if svcErr.Message == "" {
			t.Errorf("%s: message was lost", tc.name)
		}
	}
}

func TestNotFoundAndAlreadyExistsHaveHelpers(t *testing.T) {
	c := serve(t, echo)

	_, err := Call[echoRequest, echoResponse](context.Background(), c, testProcedure,
		echoRequest{Fail: "not_found"}, CallOptions{TenantID: "t"})
	if !IsNotFound(err) {
		t.Errorf("IsNotFound = false for %v", err)
	}
	if IsAlreadyExists(err) {
		t.Error("IsAlreadyExists matched a not-found")
	}

	_, err = Call[echoRequest, echoResponse](context.Background(), c, testProcedure,
		echoRequest{Fail: "already_exists"}, CallOptions{TenantID: "t"})
	if !IsAlreadyExists(err) {
		t.Errorf("IsAlreadyExists = false for %v", err)
	}
}

// Retrying a procedure that already had an effect risks doing it twice, so only
// transient codes are retried — and by default nothing is.
func TestOnlyTransientFailuresRetry(t *testing.T) {
	var attempts atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/"+testProcedure, connectjson.Unary(
		func(ctx context.Context, req *connect.Request[echoRequest]) (*connect.Response[echoResponse], error) {
			attempts.Add(1)
			return nil, connect.NewError(codeByName[req.Msg.Fail], errors.New("failing"))
		}))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Timeout: 2 * time.Second, MaxAttempts: 3})

	attempts.Store(0)
	_, _ = Call[echoRequest, echoResponse](context.Background(), c, testProcedure,
		echoRequest{Fail: "invalid_argument"}, CallOptions{TenantID: "t"})
	if got := attempts.Load(); got != 1 {
		t.Errorf("invalid_argument was attempted %d times, want 1", got)
	}

	attempts.Store(0)
	_, _ = Call[echoRequest, echoResponse](context.Background(), c, testProcedure,
		echoRequest{Fail: "unavailable"}, CallOptions{TenantID: "t"})
	if got := attempts.Load(); got != 3 {
		t.Errorf("unavailable was attempted %d times, want 3", got)
	}
}

func TestDefaultIsNoRetry(t *testing.T) {
	var attempts atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/"+testProcedure, connectjson.Unary(
		func(ctx context.Context, req *connect.Request[echoRequest]) (*connect.Response[echoResponse], error) {
			attempts.Add(1)
			return nil, connect.NewError(connect.CodeUnavailable, errors.New("down"))
		}))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL, Timeout: 2 * time.Second})
	_, _ = Call[echoRequest, echoResponse](context.Background(), c, testProcedure,
		echoRequest{}, CallOptions{TenantID: "t"})

	if got := attempts.Load(); got != 1 {
		t.Errorf("default client attempted %d times, want 1", got)
	}
}

// A service that is not listening is unavailable, which is retryable — a
// distinct outcome from the callee deliberately refusing.
func TestUnreachableServiceIsUnavailable(t *testing.T) {
	c := New(Config{BaseURL: "http://127.0.0.1:1", Timeout: time.Second})

	_, err := Call[echoRequest, echoResponse](context.Background(), c, testProcedure,
		echoRequest{}, CallOptions{TenantID: "t"})
	if err == nil {
		t.Fatal("expected an error from an unreachable service")
	}
	var svcErr *Error
	if !errors.As(err, &svcErr) {
		t.Fatalf("got %v, want a structured *Error", err)
	}
	if svcErr.Code != connect.CodeUnavailable {
		t.Errorf("code = %s, want unavailable", svcErr.Code)
	}
	if !svcErr.Retryable() {
		t.Error("an unreachable service should be retryable")
	}
}

func TestHealth(t *testing.T) {
	c := serve(t, echo)
	if err := c.Health(context.Background()); err != nil {
		t.Errorf("Health: %v", err)
	}

	down := New(Config{BaseURL: "http://127.0.0.1:1", Timeout: time.Second})
	if err := down.Health(context.Background()); err == nil {
		t.Error("Health reported a dead service as healthy")
	}
}

func TestUnknownProcedureIsNotFound(t *testing.T) {
	c := serve(t, echo)

	_, err := Call[echoRequest, echoResponse](context.Background(), c, "test.v1.EchoService/Missing",
		echoRequest{}, CallOptions{TenantID: "t"})
	if !IsNotFound(err) {
		t.Errorf("got %v, want not found", err)
	}
}

func TestContextCancellationStopsTheCall(t *testing.T) {
	c := serve(t, echo)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Call[echoRequest, echoResponse](ctx, c, testProcedure,
		echoRequest{Value: "x"}, CallOptions{TenantID: "t"})
	if err == nil {
		t.Fatal("a cancelled context still completed the call")
	}
}
