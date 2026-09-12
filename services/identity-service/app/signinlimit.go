package app

import (
	"net/http"
	"strings"

	"github.com/ppusapati/gavya/libs/integrity/ratelimit"
	"github.com/ppusapati/gavya/services/identity-service/internal/handler"
)

// The sign-in budget, per address.
//
// Failures, not attempts. A co-operative signing thirty people in at six in the
// morning must never meet this; somebody working through a password list must
// meet it almost immediately. Counting attempts cannot tell those apart and
// would throttle the first to bound the second.
//
// Ten failures, refilling at one every thirty seconds. A person who has
// forgotten their password gets several tries and then a wait; a machine gets
// 2,880 guesses a day from one address, against a platform where the account
// itself locks long before that. The two controls cover different attacks and
// neither substitutes for the other: the lock protects one account against many
// passwords, and this protects many accounts against one password.
const (
	SignInFailuresBurst  = 10
	SignInFailuresPerSec = 1.0 / 30.0
	SignInTrackedClients = 4096
)

// signInPaths are the two procedures worth defending this way.
//
// Both exchange a secret for a session, which is the only place in the platform
// where guessing gets somebody in.
var signInPaths = map[string]bool{
	"/" + handler.ServiceName + "/SignIn":        true,
	"/" + handler.ServiceName + "/SignInService": true,
}

// LimitSignIn wraps a mux so repeated sign-in failures from one address slow
// down.
//
// In app rather than internal/handler because the modulith mounts this service
// as a module of another Go module, and internal does not cross that line. The
// limit has to be reachable from both shapes: it is the deployment shape this
// platform actually ships that would otherwise be the one without it.
//
// Everything else passes through untouched: the general limit in
// libs/integrity/serve already applies to it, and a second one here would be two
// numbers to reason about where one will do.
func LimitSignIn(next http.Handler) http.Handler {
	limiter := ratelimit.New(SignInFailuresPerSec, SignInFailuresBurst, SignInTrackedClients)
	guarded := ratelimit.OnFailure(limiter, signInKey, failedSignIn)(next)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if signInPaths[r.URL.Path] {
			guarded.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// signInKey is the address, and the address only.
//
// Not the email. Keying on the address bounds the attacker; keying on the
// account would let anybody lock a colleague out by failing against their
// address deliberately, which turns a defence into a way to do harm.
func signInKey(r *http.Request) string {
	return ratelimit.ByClient(r)
}

// failedSignIn says which answers count against the budget.
//
// Unauthenticated is the wrong-credential answer. A malformed request is the
// caller's mistake and not a guess, and a 500 is this platform's fault — neither
// should spend somebody's budget.
func failedSignIn(status int) bool {
	return status == http.StatusUnauthorized
}

// ClientAddress is the address a sign-in is recorded against.
//
// The gateway's assertion first, because it is the peer of the connection and
// cannot be chosen by the caller. The forwarding headers are the fallback for a
// service reached without a gateway, and they are a claim rather than a fact:
// kept because a wrong address in the log is better than none, and never used
// for a decision.
func ClientAddress(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get(ratelimit.ClientHeader)); v != "" {
		return v
	}
	return ratelimit.PeerAddress(r)
}
