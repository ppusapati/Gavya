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

	"connectrpc.com/connect"
)

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

		resp, err := fn(r.Context(), req)
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
