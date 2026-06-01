package timeout

import (
	"context"
	"time"

	"p9e.in/samavaya/packages/api/v1/config"

	"google.golang.org/protobuf/types/known/durationpb"
)

// WithTimeout is a generic configuration function for setting timeouts
// It can be used to configure default and long query timeouts for various configurations
func WithTimeout(defaultTimeout, longQueryTimeout time.Duration) func(*config.Data) {
	return func(cfg *config.Data) {
		cfg.ContextConfig.DefaultTimeout = durationpb.New(defaultTimeout)
		cfg.ContextConfig.LongQueryTimeout = durationpb.New(longQueryTimeout)
	}
}

// ApplyTimeout is a generic function to apply timeout to a context
// It works with any configuration that has a ContextConfig with DefaultTimeout and LongQueryTimeout
type TimeoutProvider struct {
	cfg *config.Data
}

func NewTimeoutProvider(cfg *config.Data) *TimeoutProvider {
	return &TimeoutProvider{
		cfg: cfg,
	}
}

// ApplyTimeout ensures a timeout is set on the context
func (c *TimeoutProvider) ApplyTimeout(ctx context.Context, isLongQuery bool) (context.Context, context.CancelFunc) {
	// Check if context already has a deadline
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}

	// Select timeout based on query type
	var timeout *durationpb.Duration

	// Use default timeouts if cfg is nil or specific timeout is not set
	if c.cfg == nil {
		defaultTimeout := 30 * time.Second
		if isLongQuery {
			defaultTimeout = 5 * time.Minute
		}
		timeout = durationpb.New(defaultTimeout)
	} else {
		if isLongQuery {
			timeout = c.cfg.ContextConfig.LongQueryTimeout
		} else {
			timeout = c.cfg.ContextConfig.DefaultTimeout
		}

		// Fallback to default if specific timeout is not set
		if timeout == nil {
			defaultTimeout := 30 * time.Second
			if isLongQuery {
				defaultTimeout = 5 * time.Minute
			}
			timeout = durationpb.New(defaultTimeout)
		}
	}

	// Create context with timeout
	return context.WithTimeout(ctx, timeout.AsDuration())
}
