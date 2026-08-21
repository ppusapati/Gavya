// Package mlclient is the Go side of the boundary to the Rust ML tier.
//
// The ML services are separate processes reached over the network and nothing
// else: no shared memory, no cgo, no in-process model loading. They speak the
// Connect unary JSON protocol, so a procedure is a plain HTTP POST to
// /<fully.qualified.Service>/<Method> with a JSON body and a JSON reply.
//
// Every ML call is advisory. A scoring or estimation failure degrades the
// caller to its deterministic path; it must never fail a settlement.
package mlclient

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
)

const (
	HeaderTenantID  = "X-Tenant-ID"
	HeaderRequestID = "X-Request-ID"
	// HeaderModelPin lets a caller demand a specific model version so a shadow
	// settlement can be replayed against the model that originally scored it.
	HeaderModelPin = "X-Model-Version"
)

// ErrUnavailable means the ML tier could not be reached or did not answer in
// time. Callers should treat it as "no advisory signal", not as a failure.
var ErrUnavailable = errors.New("mlclient: ml service unavailable")

// Error is a structured error returned by an ML service.
type Error struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"-"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("mlclient: %s (%s, http %d)", e.Message, e.Code, e.HTTPStatus)
}

// Retryable reports whether repeating the call could succeed. ML procedures are
// pure functions of their input, so retrying is always safe when it is useful.
func (e *Error) Retryable() bool {
	return e.HTTPStatus == http.StatusTooManyRequests || e.HTTPStatus >= 500
}

// Config describes one Rust ML service endpoint.
type Config struct {
	BaseURL string
	// Timeout bounds a single attempt, not the whole call including retries.
	Timeout time.Duration
	// MaxAttempts includes the first attempt. Zero means 3.
	MaxAttempts int
	// ModelVersion pins the model, when the caller needs a reproducible score.
	ModelVersion string
}

// Client holds the connection pool for one ML service.
type Client struct {
	baseURL     string
	http        *http.Client
	maxAttempts int
	modelPin    string
}

func New(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	attempts := cfg.MaxAttempts
	if attempts <= 0 {
		attempts = 3
	}
	return &Client{
		baseURL:     strings.TrimRight(cfg.BaseURL, "/"),
		maxAttempts: attempts,
		modelPin:    cfg.ModelVersion,
		http: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        64,
				MaxIdleConnsPerHost: 16,
				IdleConnTimeout:     90 * time.Second,
				DialContext:         (&net.Dialer{Timeout: 2 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			},
		},
	}
}

// CallOptions carries the per-request correlation data the ML tier needs.
type CallOptions struct {
	TenantID  string
	RequestID string
}

// Invoke performs one unary Connect-JSON procedure call.
//
// It is generic over request and response so each ML procedure gets a typed
// wrapper in its own file rather than a map[string]any at the call site.
func Invoke[Req any, Resp any](ctx context.Context, c *Client, procedure string, in Req, opts CallOptions) (*Resp, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("mlclient: encode %s: %w", procedure, err)
	}
	url := c.baseURL + "/" + strings.TrimLeft(procedure, "/")

	var lastErr error
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		if attempt > 1 {
			// 100ms, 200ms, 400ms ... capped, honouring caller cancellation.
			backoff := time.Duration(1<<uint(attempt-2)) * 100 * time.Millisecond
			if backoff > 2*time.Second {
				backoff = 2 * time.Second
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		out, err := c.attempt(ctx, url, procedure, body, opts)
		if err == nil {
			return decode[Resp](out, procedure)
		}
		lastErr = err

		var mlErr *Error
		if errors.As(err, &mlErr) && !mlErr.Retryable() {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, fmt.Errorf("%w: %s: %v", ErrUnavailable, procedure, lastErr)
}

func (c *Client) attempt(ctx context.Context, url, procedure string, body []byte, opts CallOptions) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("mlclient: build %s: %w", procedure, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if opts.TenantID != "" {
		req.Header.Set(HeaderTenantID, opts.TenantID)
	}
	if opts.RequestID != "" {
		req.Header.Set(HeaderRequestID, opts.RequestID)
	}
	if c.modelPin != "" {
		req.Header.Set(HeaderModelPin, c.modelPin)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 1 MiB is far above any legitimate ML reply and bounds a misbehaving peer.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		mlErr := &Error{HTTPStatus: resp.StatusCode}
		if json.Unmarshal(raw, mlErr) != nil || mlErr.Message == "" {
			mlErr.Code = "unknown"
			mlErr.Message = strings.TrimSpace(string(raw))
		}
		return nil, mlErr
	}
	return raw, nil
}

func decode[Resp any](raw []byte, procedure string) (*Resp, error) {
	var out Resp
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("mlclient: decode %s: %w", procedure, err)
	}
	return &out, nil
}

// Health probes the ML service readiness endpoint.
func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: health status %d", ErrUnavailable, resp.StatusCode)
	}
	return nil
}

// asMLError is errors.As specialised to *Error, so callers can branch on the
// structured code without importing errors at every call site.
func asMLError(err error, target **Error) bool { return errors.As(err, target) }
