// Package svcclient calls one Go service from another over Connect unary JSON.
//
// It is deliberately not mlclient. A failed ML call is advisory — the caller
// degrades to its deterministic path and carries on. A failed call to another
// Go service is a real failure: if canonicalisation cannot resolve a producer,
// the import that depended on it must not proceed as though it had.
//
// So this client surfaces the callee's Connect code rather than flattening
// everything to "unavailable", and it retries only what is safe to retry.
package svcclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/libs/integrity/authz"
	"github.com/ppusapati/gavya/libs/integrity/connectjson"
	"github.com/ppusapati/gavya/libs/integrity/tracing"
)

const (
	HeaderTenantID  = "X-Tenant-ID"
	HeaderRequestID = "X-Request-ID"
)

// Error is a failure reported by the callee, carrying the Connect code it chose
// so the caller can branch on it exactly as it would on a local call.
type Error struct {
	Code       connect.Code
	Message    string
	Procedure  string
	HTTPStatus int
}

func (e *Error) Error() string {
	return fmt.Sprintf("svcclient: %s: %s (%s)", e.Procedure, e.Message, e.Code)
}

// Retryable reports whether repeating the call could plausibly succeed.
//
// Only conditions that are transient by definition qualify. An invalid argument
// or a not-found never becomes true by asking again, and retrying a call that
// already had an effect risks doing it twice.
func (e *Error) Retryable() bool {
	switch e.Code {
	case connect.CodeUnavailable, connect.CodeResourceExhausted, connect.CodeAborted:
		return true
	default:
		return false
	}
}

// IsNotFound is the common branch: a caller that can proceed without the record.
func IsNotFound(err error) bool { return HasCode(err, connect.CodeNotFound) }

// IsAlreadyExists lets a caller treat a duplicate as success, which is what an
// idempotent import needs.
func IsAlreadyExists(err error) bool { return HasCode(err, connect.CodeAlreadyExists) }

func HasCode(err error, code connect.Code) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

type Config struct {
	BaseURL string
	// Timeout bounds a single attempt, not the whole call including retries.
	Timeout time.Duration
	// MaxAttempts includes the first. Zero means 1 — no retry, because most
	// procedures here have effects and the safe default is to fail loudly.
	MaxAttempts int
}

type Client struct {
	baseURL     string
	http        *http.Client
	maxAttempts int
}

func New(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	attempts := cfg.MaxAttempts
	if attempts <= 0 {
		attempts = 1
	}
	return &Client{
		baseURL:     strings.TrimRight(cfg.BaseURL, "/"),
		maxAttempts: attempts,
		http: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        64,
				MaxIdleConnsPerHost: 16,
				IdleConnTimeout:     90 * time.Second,
				DialContext:         (&net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			},
		},
	}
}

// CallOptions carries the correlation data every service logs against, and who
// the call is being made as.
type CallOptions struct {
	TenantID  string
	RequestID string

	// Tenant and Actor are what the gateway sets after verifying a session, and
	// what the audit trail attributes a change to. A service-to-service caller
	// sets them itself because there is no gateway between two services — and
	// the point of them being headers rather than body fields still holds: the
	// receiving service reads who is acting from the transport, never from the
	// payload it was handed.
	Tenant string
	// Actor is a person. ServiceIdentity is a service. Exactly one should be
	// set: a service borrowing a person's name produces a trail that attributes
	// its actions to somebody who was not there.
	Actor           string
	ServiceIdentity string

	// Permissions is what this caller may do, as authz renders a set: a
	// comma-separated list. The receiving service authorises against it.
	//
	// A service calling another service presents its own, exactly as it presents
	// its own tenant and identity, because there is no gateway between two
	// services to do it for them. Left empty the header is not sent at all, and
	// the receiving service refuses every procedure — which is the right
	// default: a caller that has not said what it may do has not said it may do
	// anything.
	Permissions string
}

// Call performs one unary procedure call.
func Call[Req any, Resp any](ctx context.Context, c *Client, procedure string, in Req, opts CallOptions) (*Resp, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("svcclient: encode %s: %w", procedure, err)
	}
	url := c.baseURL + "/" + strings.TrimLeft(procedure, "/")

	var lastErr error
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		if attempt > 1 {
			backoff := time.Duration(1<<uint(attempt-2)) * 200 * time.Millisecond
			if backoff > 3*time.Second {
				backoff = 3 * time.Second
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		raw, err := c.attempt(ctx, url, procedure, body, opts)
		if err == nil {
			var out Resp
			if err := json.Unmarshal(raw, &out); err != nil {
				return nil, fmt.Errorf("svcclient: decode %s: %w", procedure, err)
			}
			return &out, nil
		}
		lastErr = err

		var svcErr *Error
		if errors.As(err, &svcErr) && !svcErr.Retryable() {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

func (c *Client) attempt(ctx context.Context, url, procedure string, body []byte, opts CallOptions) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("svcclient: build %s: %w", procedure, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if opts.Tenant != "" {
		req.Header.Set(connectjson.TenantHeader, opts.Tenant)
	}
	if opts.Actor != "" {
		req.Header.Set(connectjson.UserHeader, opts.Actor)
	}
	if opts.ServiceIdentity != "" {
		req.Header.Set(connectjson.ServiceIdentityHeader, opts.ServiceIdentity)
	}
	if opts.Permissions != "" {
		req.Header.Set(authz.PermissionsHeader, opts.Permissions)
	}
	if opts.TenantID != "" {
		req.Header.Set(HeaderTenantID, opts.TenantID)
	}
	if opts.RequestID != "" {
		req.Header.Set(HeaderRequestID, opts.RequestID)
	}
	// The trace, continued rather than restarted.
	//
	// X-Request-Id above is not this and never was: every caller generates its
	// own, so a settlement that reaches procurement, canonical and notification
	// had four unrelated ids and nothing tied them together. This sends a child
	// of the span this service is currently serving, which is what makes the
	// chain reconstructable from either end.
	tracing.Inject(ctx, req.Header)

	resp, err := c.http.Do(req)
	if err != nil {
		// A transport failure is indistinguishable from an overloaded peer, so
		// it is reported as unavailable and is therefore retryable.
		return nil, &Error{
			Code:      connect.CodeUnavailable,
			Message:   err.Error(),
			Procedure: procedure,
		}
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, &Error{Code: connect.CodeUnavailable, Message: err.Error(), Procedure: procedure}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, decodeError(raw, resp.StatusCode, procedure)
	}
	return raw, nil
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func decodeError(raw []byte, status int, procedure string) error {
	out := &Error{Procedure: procedure, HTTPStatus: status, Code: codeForStatus(status)}

	var body errorBody
	if json.Unmarshal(raw, &body) == nil && body.Message != "" {
		out.Message = body.Message
		// The callee's own code is more precise than anything inferred from the
		// status, so it wins when present.
		if code, ok := codeByName[body.Code]; ok {
			out.Code = code
		}
	} else {
		out.Message = strings.TrimSpace(string(raw))
	}
	if out.Message == "" {
		out.Message = http.StatusText(status)
	}
	return out
}

var codeByName = map[string]connect.Code{
	"canceled":            connect.CodeCanceled,
	"unknown":             connect.CodeUnknown,
	"invalid_argument":    connect.CodeInvalidArgument,
	"deadline_exceeded":   connect.CodeDeadlineExceeded,
	"not_found":           connect.CodeNotFound,
	"already_exists":      connect.CodeAlreadyExists,
	"permission_denied":   connect.CodePermissionDenied,
	"resource_exhausted":  connect.CodeResourceExhausted,
	"failed_precondition": connect.CodeFailedPrecondition,
	"aborted":             connect.CodeAborted,
	"out_of_range":        connect.CodeOutOfRange,
	"unimplemented":       connect.CodeUnimplemented,
	"internal":            connect.CodeInternal,
	"unavailable":         connect.CodeUnavailable,
	"data_loss":           connect.CodeDataLoss,
	"unauthenticated":     connect.CodeUnauthenticated,
}

func codeForStatus(status int) connect.Code {
	switch status {
	case http.StatusBadRequest:
		return connect.CodeInvalidArgument
	case http.StatusNotFound:
		return connect.CodeNotFound
	case http.StatusConflict:
		return connect.CodeAlreadyExists
	case http.StatusForbidden:
		return connect.CodePermissionDenied
	case http.StatusUnauthorized:
		return connect.CodeUnauthenticated
	case http.StatusTooManyRequests:
		return connect.CodeResourceExhausted
	case http.StatusPreconditionFailed:
		return connect.CodeFailedPrecondition
	case http.StatusNotImplemented:
		return connect.CodeUnimplemented
	case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout:
		return connect.CodeUnavailable
	default:
		return connect.CodeInternal
	}
}

// Health probes a service's readiness endpoint.
func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return &Error{Code: connect.CodeUnavailable, Message: err.Error(), Procedure: "healthz"}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return &Error{
			Code:       connect.CodeUnavailable,
			Message:    fmt.Sprintf("health status %d", resp.StatusCode),
			Procedure:  "healthz",
			HTTPStatus: resp.StatusCode,
		}
	}
	return nil
}
