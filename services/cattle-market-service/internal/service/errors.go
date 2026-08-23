package service

import (
	"errors"
	"fmt"
)

// ErrInvalidArgument marks a caller mistake.
//
// Without it the handler cannot tell "you did not supply a tenant_id" from "the
// query failed", and reports both as internal — which tells a client to retry a
// call that will never succeed, however many times they make it.
var ErrInvalidArgument = errors.New("invalid argument")

// invalidArgument carries the reason alone. The marker is matched through Is,
// so errors.Is finds it while the message stays free of a prefix the error code
// already conveys.
type invalidArgument struct{ reason string }

func (e *invalidArgument) Error() string { return e.reason }

func (e *invalidArgument) Is(target error) bool { return target == ErrInvalidArgument }

func invalid(format string, args ...any) error {
	if len(args) == 0 {
		return &invalidArgument{reason: format}
	}
	return &invalidArgument{reason: fmt.Sprintf(format, args...)}
}
