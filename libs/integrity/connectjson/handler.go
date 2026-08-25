// Package connectjson serves handwritten Connect handlers over unary JSON.
//
// The services in this repository define their request and response types as
// plain Go structs rather than generated protobuf messages. Connect's built-in
// codecs require proto.Message, so the generated handler constructors cannot be
// used — which is why the handler methods existed but nothing routed to them.
//
// This adapts a handler method to an ordinary HTTP endpoint speaking the
// Connect unary JSON protocol: POST to /<fully.qualified.Service>/<Method> with
// a JSON body and a JSON reply. That is the same protocol the Rust ML tier
// serves and libs/integrity/mlclient already speaks, so both directions of the
// platform's service-to-service traffic look identical on the wire.
package connectjson

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/tenantctx"
)

// TenantHeader carries a tenant established by something that verified it.
const TenantHeader = "X-Gavya-Tenant"

// MaxRequestBytes bounds a single request. Generous for a settlement batch,
// small enough that a misbehaving peer cannot exhaust memory.
const MaxRequestBytes = 8 << 20

// errorBody is the wire shape of a failure. It matches what the Rust ML tier
// emits and what mlclient decodes, so one client shape works against both.
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Unary adapts one handler method to an HTTP endpoint.
func Unary[Req any, Resp any](
	fn func(context.Context, *connect.Request[Req]) (*connect.Response[Resp], error),
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			// Connect unary is POST only; anything else is a client mistake
			// worth naming rather than a bare 404.
			writeError(w, connect.NewError(connect.CodeUnimplemented,
				fmt.Errorf("%s is not supported; Connect unary procedures are POST", r.Method)))
			return
		}

		var msg Req
		body := http.MaxBytesReader(w, r.Body, MaxRequestBytes)
		if err := json.NewDecoder(body).Decode(&msg); err != nil {
			writeError(w, connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("request body is not valid JSON for this procedure: %w", err)))
			return
		}

		req := connect.NewRequest(&msg)
		// Headers carry the tenant and request id that logging and tracing
		// depend on, so they are passed through rather than dropped.
		for k, v := range r.Header {
			req.Header()[k] = v
		}

		ctx, err := scopeToTenant(r.Context(), r.Header.Get(TenantHeader), &msg)
		if err != nil {
			writeError(w, err)
			return
		}

		resp, err := fn(ctx, req)
		if err != nil {
			writeError(w, err)
			return
		}

		for k, v := range resp.Header() {
			w.Header()[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp.Msg)
	}
}

func writeError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(HTTPStatus(connect.CodeOf(err)))
	_ = json.NewEncoder(w).Encode(errorBody{
		Code:    connect.CodeOf(err).String(),
		Message: message(err),
	})
}

// message returns the reason alone, leaving the code to its own field rather
// than repeating it in the text.
func message(err error) string {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Message()
	}
	return err.Error()
}

// HTTPStatus maps a Connect code onto the status the protocol prescribes.
//
// The mapping matters beyond tidiness: clients retry 5xx and give up on 4xx, so
// a validation failure returned as 500 would be retried several times before
// failing, and an overloaded service returning 400 would never be retried.
func HTTPStatus(code connect.Code) int {
	switch code {
	case connect.CodeCanceled:
		return 499
	case connect.CodeInvalidArgument, connect.CodeOutOfRange:
		return http.StatusBadRequest
	case connect.CodeDeadlineExceeded:
		return http.StatusGatewayTimeout
	case connect.CodeNotFound:
		return http.StatusNotFound
	case connect.CodeAlreadyExists, connect.CodeAborted:
		return http.StatusConflict
	case connect.CodePermissionDenied:
		return http.StatusForbidden
	case connect.CodeUnauthenticated:
		return http.StatusUnauthorized
	case connect.CodeResourceExhausted:
		return http.StatusTooManyRequests
	case connect.CodeFailedPrecondition:
		return http.StatusPreconditionFailed
	case connect.CodeUnimplemented:
		return http.StatusNotImplemented
	case connect.CodeUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// Procedure builds the path a Connect procedure is addressed at.
func Procedure(service, method string) string { return "/" + service + "/" + method }

// scopeToTenant puts the request's tenant on the context, where the database
// layer reads it to scope the connection.
//
// This is the one place every request in every service passes through, which is
// why it is here rather than in each handler: a handler that forgot would be
// refused by the policies, but only after somebody noticed the endpoint had
// stopped working.
//
// Two sources, and the difference between them matters:
//
// The header is set by something that verified who is calling. The body field is
// the client's own claim about which tenant it is. Today there is no
// authentication in front of these services, so the body is all there is, and
// that is worth being precise about: scoping connections from the body closes
// the class of defect where a query forgets its tenant and reads across the
// boundary — which has already happened twice in this codebase — and does not
// close the case of a caller that asks for another tenant on purpose. That one
// closes when the header arrives from a verified token.
//
// When both are present they must agree. A request that presents a verified
// tenant and then asks for a different one in its body is refused rather than
// resolved, because either answer would be a decision about whose data somebody
// gets.
func scopeToTenant(ctx context.Context, header string, msg any) (context.Context, error) {
	body := tenantFromBody(msg)

	switch {
	case header != "" && body != "" && header != body:
		return nil, connect.NewError(connect.CodePermissionDenied,
			fmt.Errorf("this request is authenticated for tenant %q but asks for tenant %q", header, body))
	case header != "":
		return withChecked(ctx, header)
	case body != "":
		return withChecked(ctx, body)
	default:
		// No tenant anywhere. Left off the context deliberately: the database
		// refuses an unscoped connection, so a procedure that genuinely has no
		// tenant fails loudly at the first query rather than reading everything.
		return ctx, nil
	}
}

func withChecked(ctx context.Context, tenant string) (context.Context, error) {
	if err := tenantctx.Check(tenant); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	return tenantctx.With(ctx, tenant), nil
}

// tenantFromBody reads the tenant out of a decoded request.
//
// By the JSON name rather than the Go field name, because that is the name the
// wire format fixes; the Go spelling varies across these services (TenantID,
// TenantId) and matching on it would silently miss whichever spelling was not
// thought of.
func tenantFromBody(msg any) string {
	v := reflect.ValueOf(msg)
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return ""
	}
	idx, ok := tenantFieldOf(v.Type())
	if !ok {
		return ""
	}
	f := v.Field(idx)
	if f.Kind() != reflect.String {
		return ""
	}
	return f.String()
}

// tenantFields caches the lookup, so the reflection happens once per request
// type rather than once per request.
var tenantFields sync.Map // reflect.Type -> int, or -1 for none

func tenantFieldOf(t reflect.Type) (int, bool) {
	if cached, ok := tenantFields.Load(t); ok {
		i := cached.(int)
		return i, i >= 0
	}
	found := -1
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name == "tenant_id" {
			found = i
			break
		}
	}
	tenantFields.Store(t, found)
	return found, found >= 0
}
