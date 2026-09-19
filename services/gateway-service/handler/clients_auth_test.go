package handler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Whether either client can get through the front door at all.
//
// clients_test.go beside this one compares three lists and passes: every
// procedure the clients call is served, every package is routed, every field
// name matches a json tag. All true, and all of it about what happens after a
// request is authorised. Neither client was sending a credential, so no request
// ever got that far.
//
// What they sent was `X-Tenant-ID` and nothing else. The gateway reads the
// session from `Authorization` or a cookie, asserts the tenant itself from what
// that session proves, and strips the header it asserts from anything arriving —
// and the header the clients sent is not even the one it reads. So every
// procedure either client called returned 401 from the day authorisation was
// added, and the checks next door went on passing because they compared names.
//
// It had been written down and then overtaken: web/README.md said "Phase-1 has
// no sign-in, so the tenant decides what is visible". That was true when the
// console was built. Authorisation arrived later — reversing an earlier decision
// to defer it — and nothing came back to the clients.
//
// A check is only as good as the thing it compares against, and three checks
// comparing names against names cannot see a missing credential. These compare
// the clients against the gateway's own rule.
//
// RUN THESE WITH -count=1, for the reason at the top of clients_test.go: the
// files read here are outside this module and Go's test cache does not track
// them.

// clientTransport is a client's HTTP layer: the one file that decides what
// headers every call carries.
type clientTransport struct {
	name string
	path string
	// signIn is the procedure that client uses to obtain a session. Named here
	// rather than derived, because a client that stopped calling it would
	// otherwise read as a client that never needed one.
	signIn string
}

func clientTransports() []clientTransport {
	return []clientTransport{
		{
			name:   "the supervisor's console",
			path:   filepath.Join("web", "src", "lib", "api", "client.ts"),
			signIn: "gavya.identity.v1.IdentityService/SignIn",
		},
		{
			name:   "the collection bench",
			path:   filepath.Join("mobile", "lib", "src", "api", "connect.dart"),
			signIn: "gavya.identity.v1.IdentityService/SignInService",
		},
	}
}

// Each client sends a credential.
//
// The gateway's rule, in one sentence: everything outside the unauthenticated
// list needs a session on Authorization or in a cookie. A client that sends
// neither is a client whose every screen answers 401, which is what both of
// these were.
func TestEachClientSendsACredential(t *testing.T) {
	root := repoRoot(t)

	for _, c := range clientTransports() {
		t.Run(c.name, func(t *testing.T) {
			// Code only. The first version of this check read the whole file, and
			// a mutation that deleted the line setting the header left the test
			// passing — because the comment above it explains what the header is
			// for and says the word. A check satisfied by a comment about the
			// thing it is checking is the shape this repository keeps finding.
			src := codeOf(readClientFile(t, root, c.path))
			if !strings.Contains(src, "Authorization") {
				t.Errorf("%s never mentions Authorization in %s.\n\n"+
					"The gateway reads the session from that header or from a cookie, and "+
					"refuses everything outside its unauthenticated list without one. A "+
					"client that sends no credential gets 401 on every procedure it calls, "+
					"and the three checks in clients_test.go cannot see it: they compare "+
					"procedure names, package prefixes and field names, all of which are "+
					"about what happens after a request is let in.", c.name, c.path)
			}
			if !strings.Contains(src, "Bearer") {
				t.Errorf("%s sets Authorization in %s without a Bearer scheme. "+
					"gateway-service/handler/authentication.go trims the \"Bearer \" "+
					"prefix and treats the rest as the session id.", c.name, c.path)
			}
		})
	}
}

// No client claims a tenant.
//
// The gateway decides which tenant a caller acts for from their session and
// asserts it downstream under its own header, which it strips from anything
// arriving. A tenant sent from a client is therefore never a routing fact — at
// best it is ignored, and what it looks like is an attempt.
//
// Both clients used to send one. It was the only header they sent.
func TestNoClientClaimsATenant(t *testing.T) {
	root := repoRoot(t)

	for _, c := range clientTransports() {
		t.Run(c.name, func(t *testing.T) {
			// Code only, for the reason above: both READMEs and both transports
			// now explain in prose why a tenant is not sent, and prose saying so
			// must not read as doing so.
			for _, line := range strings.Split(codeOf(readClientFile(t, root, c.path)), "\n") {
				for _, claim := range []string{"'X-Tenant-ID'", `"X-Tenant-ID"`, tenantHeader} {
					if strings.Contains(line, claim) {
						t.Errorf("%s sets %s in %s:\n    %s\n\n"+
							"The gateway asserts the tenant from the session and strips the "+
							"header it asserts. A tenant from a client is not a routing fact.",
							c.name, claim, c.path, strings.TrimSpace(line))
					}
				}
			}
		})
	}
}

// Each client can obtain the session it sends.
//
// Sending a credential is half of it. A client that never calls a sign-in
// procedure has no way to get one, and would be holding a token somebody pasted
// in — which is the arrangement the console had before it had any token at all.
//
// The two clients sign in differently and that is deliberate: a person types a
// password into the console, and nobody types a password into a tablet left on a
// bench at half past five in the morning, so the bench authenticates as a
// service identity.
func TestEachClientCanObtainASession(t *testing.T) {
	for _, c := range clientTransports() {
		t.Run(c.name, func(t *testing.T) {
			// The procedure it signs in with has to be one the gateway lets
			// through unauthenticated, or sign-in itself needs a session.
			if !isUnauthenticated("/" + c.signIn) {
				t.Fatalf("%s signs in with %s, which the gateway does not exempt from "+
					"authorisation. Sign-in cannot require a session.", c.name, c.signIn)
			}

			calls := clientProcedures(t)
			for _, call := range calls {
				if call.procedure == c.signIn {
					return
				}
			}
			t.Errorf("%s calls no sign-in procedure.\n\n"+
				"It sends a credential and has no way to obtain one, so the token it "+
				"sends is whatever somebody pasted into it. It should call %s.",
				c.name, c.signIn)
		})
	}
}

// codeOf drops the comments, so that a check for a header cannot be satisfied
// by a sentence about that header.
//
// Line comments in both languages start with //, and both use /* */ for blocks;
// a run of them is tracked rather than stripped per line, because the sentence
// that satisfied the first version of this check sat inside one.
func codeOf(src string) string {
	var out strings.Builder
	inBlock := false
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case inBlock:
			if strings.Contains(trimmed, "*/") {
				inBlock = false
			}
			continue
		case strings.HasPrefix(trimmed, "//"):
			continue
		case strings.HasPrefix(trimmed, "/*"):
			if !strings.Contains(trimmed, "*/") {
				inBlock = true
			}
			continue
		case strings.HasPrefix(trimmed, "*"):
			// A continuation line of a doc comment in either language.
			continue
		}
		out.WriteString(line)
		out.WriteString("\n")
	}
	return out.String()
}

func readClientFile(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("read %s: %v\n\nThis check exists because nothing else in the "+
			"repository reads the clients' transport at all.", rel, err)
	}
	return string(b)
}
