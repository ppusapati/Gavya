package p9context

import (
	"context"
)

type (
	trustKey struct{}
)

// TrustedContextValidator validate whether the communication is behind authed gateway or server to server communication
type TrustedContextValidator interface {
	Trusted(ctx context.Context) (bool, error)
}

type ClientTrustedContextValidator struct {
}

func NewClientTrustedContextValidator() TrustedContextValidator {
	return &ClientTrustedContextValidator{}
}

// NewTrustedContext create a trusted (or not) context without propaganda to other services
func NewTrustedContext(ctx context.Context, trust ...bool) context.Context {
	t := true
	if len(trust) > 0 {
		t = trust[0]
	}
	return context.WithValue(ctx, trustKey{}, t)
}

func FromTrustedContext(ctx context.Context) (bool, bool) {
	v, ok := ctx.Value(trustKey{}).(bool)
	if ok {
		return v, ok
	}
	return false, false
}

// Trusted validates whether the context is trusted.
// Currently checks explicit trust markers set via NewTrustedContext.
// Future enhancement: JWT-based server-to-server trust validation.
func (c *ClientTrustedContextValidator) Trusted(ctx context.Context) (bool, error) {
	if v, ok := FromTrustedContext(ctx); ok {
		return v, nil
	}
	return false, nil
}

var _ TrustedContextValidator = (*ClientTrustedContextValidator)(nil)
