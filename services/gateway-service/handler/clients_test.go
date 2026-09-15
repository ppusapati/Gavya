package handler

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/ppusapati/gavya/libs/integrity/authz"
)

// The two clients are the requirement, written down in a form that runs.
//
// web/ is the supervisor's console — quarantine, identity mapping, mass balance,
// divergences — and mobile/ is the collection bench. Everything either of them
// calls is something somebody was promised. Nothing in the repository compared
// what they call against what the platform serves, and the two are in different
// languages in different modules, so nothing was going to notice.
//
// The failure that would have followed is quiet in the worst way. Rename a
// procedure on the Go side and every Go test still passes: the route table, the
// permission table and the end-to-end suite all move together, because they are
// all generated from or written beside the Go. The console gets not_found on a
// screen a supervisor opens once a fortnight, and the bench — which holds a
// morning's collections in an outbox and only lets go of them when the server
// says it has them — gets not_found on DeliverRecord and keeps every record.
// Nothing is lost, and nothing is counted either, until somebody telephones.
//
// So: two lists of the same thing, compared. The clients' procedure calls
// against authz.Table, which is the platform's exhaustive register of routes and
// is itself gate-tested against the routes each service actually registers.
//
// RUN THESE WITH -count=1, for the reason at the top of deployment_test.go: the
// files read here sit outside this module and Go's test cache does not track
// them. Edit a .dart file and `go test ./...` reports a cached pass.

// webCall matches a procedure in the TypeScript facade: `${T.CANONICAL}/MapIdentity`.
var webCall = regexp.MustCompile(`\$\{T\.([A-Z_]+)\}/([A-Za-z][A-Za-z0-9]*)`)

// webConst matches the service-name constants those refer to.
var webConst = regexp.MustCompile(`export const ([A-Z_]+) = '([a-z]+\.v[0-9]+\.[A-Za-z]+)'`)

// dartCall matches a procedure in the Dart client: '$ingestionService/OpenSession'.
var dartCall = regexp.MustCompile(`\$([a-zA-Z][a-zA-Z0-9]*)/([A-Za-z][A-Za-z0-9]*)`)

// dartConst matches the service-name constants those refer to.
var dartConst = regexp.MustCompile(`const ([a-zA-Z][a-zA-Z0-9]*) = '([a-z]+\.v[0-9]+\.[A-Za-z]+)'`)

// clientCall is one procedure a client calls, and where it calls it from.
type clientCall struct {
	procedure string
	where     string
}

// webProcedures reads the console's API facade.
func webProcedures(t *testing.T) []clientCall {
	t.Helper()
	types := readRepoFile(t, filepath.Join("web", "src", "lib", "api", "types.ts"))
	facade := readRepoFile(t, filepath.Join("web", "src", "lib", "api", "index.ts"))

	names := map[string]string{}
	for _, m := range webConst.FindAllStringSubmatch(types, -1) {
		names[m[1]] = m[2]
	}
	if len(names) == 0 {
		t.Fatal("web/src/lib/api/types.ts declared no service constants; the parser below is reading nothing")
	}

	var out []clientCall
	for _, m := range webCall.FindAllStringSubmatch(facade, -1) {
		service, ok := names[m[1]]
		if !ok {
			t.Errorf("web/src/lib/api/index.ts calls ${T.%s}/%s and types.ts declares no such constant", m[1], m[2])
			continue
		}
		out = append(out, clientCall{procedure: service + "/" + m[2], where: "web"})
	}
	return out
}

// mobileProcedures reads every Dart file under the bench's api directory, rather
// than a list of them: a second client file is a thing somebody adds, and a list
// here would go on passing without it.
func mobileProcedures(t *testing.T) []clientCall {
	t.Helper()
	dir := filepath.Join(repoRoot(t), "mobile", "lib", "src", "api")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	names := map[string]string{}
	var sources []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".dart") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		src := string(b)
		sources = append(sources, src)
		for _, m := range dartConst.FindAllStringSubmatch(src, -1) {
			names[m[1]] = m[2]
		}
	}
	if len(names) == 0 {
		t.Fatal("mobile/lib/src/api declared no service constants; the parser below is reading nothing")
	}

	var out []clientCall
	for _, src := range sources {
		for _, m := range dartCall.FindAllStringSubmatch(src, -1) {
			service, ok := names[m[1]]
			if !ok {
				// Not every $identifier/word in Dart is a procedure — a string
				// interpolation followed by a slash could be anything. Only the
				// ones naming a declared service constant are calls.
				continue
			}
			out = append(out, clientCall{procedure: service + "/" + m[2], where: "mobile"})
		}
	}
	return out
}

func clientProcedures(t *testing.T) []clientCall {
	t.Helper()
	calls := append(webProcedures(t), mobileProcedures(t)...)
	sort.Slice(calls, func(i, j int) bool { return calls[i].procedure < calls[j].procedure })
	return calls
}

// A procedure a client calls and the platform does not serve is a screen that
// cannot work.
//
// authz.Table is the comparison rather than a list of handlers because that
// table is already known to match the routes: its own exhaustive test fails the
// build if a route exists without an entry or an entry names a route that does
// not exist. Comparing against it therefore compares against what is served, and
// adds the second thing a client needs — that somebody can be granted permission
// to make the call at all.
func TestEveryProcedureTheClientsCallIsServed(t *testing.T) {
	table := authz.Table()
	calls := clientProcedures(t)
	if len(calls) < 20 {
		t.Fatalf("found only %d client calls; the console alone makes more than that, so the parsers are not reading what they should", len(calls))
	}

	seen := map[string]bool{}
	for _, c := range calls {
		if seen[c.procedure] {
			continue
		}
		seen[c.procedure] = true
		if _, ok := table[c.procedure]; !ok {
			t.Errorf("%s calls %s and the platform serves no such procedure", c.where, c.procedure)
		}
	}
}

// The clients reach the platform through the gateway and nothing else. A
// procedure in a package the gateway has no prefix for is one it answers with
// its catch-all, whatever the service behind it does.
//
// This is a different failure from the one above and is invisible to it: the
// procedure exists, the service serves it, the permission is granted, and the
// request never arrives.
func TestEveryPackageTheClientsCallIsRoutedByTheGateway(t *testing.T) {
	src := readRepoFile(t, filepath.Join("services", "gateway-service", "handler", "connect_handlers.go"))
	prefixes := regexp.MustCompile(`\{"(/[a-z.0-9]+\.v[0-9]+\.)",`).FindAllStringSubmatch(src, -1)
	if len(prefixes) == 0 {
		t.Fatal("read no upstream prefixes out of the gateway's route table; the pattern above no longer matches it")
	}
	routed := map[string]bool{}
	for _, m := range prefixes {
		// The gateway's prefixes carry a leading and a trailing dot —
		// "/ingestion.v1." — so that a package cannot match one that merely
		// begins with its name. A procedure's package has neither.
		routed[strings.Trim(m[1], "/.")] = true
	}

	for _, c := range clientProcedures(t) {
		pkg := c.procedure[:strings.LastIndex(c.procedure, ".")]
		if !routed[pkg] {
			t.Errorf("%s calls %s and the gateway has no route for %s.", c.where, c.procedure, pkg)
		}
	}
}
