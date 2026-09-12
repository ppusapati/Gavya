package handler

import (
	"context"
	"errors"
)

// PingIdentity reports whether this gateway can verify a session.
//
// The gateway's readiness is not a database — it has none — it is whether the
// one service it cannot work without is answering. A gateway that is up and
// cannot reach identity refuses every request with "the identity service could
// not be reached", and a readiness probe that said yes to that would keep it in
// rotation doing exactly that.
//
// An unknown session id is the probe: a verifier that answers "not signed in"
// has been reached, which is the question being asked. Only a transport failure
// counts as unready.
func (h *Handler) PingIdentity(ctx context.Context) error {
	if h.verifier == nil {
		return errors.New("no identity service is configured")
	}
	_, err := h.verifier.Verify(ctx, "SE_READINESS_PROBE_0000000")
	if err == nil || errors.Is(err, errNotSignedIn) {
		return nil
	}
	return err
}
