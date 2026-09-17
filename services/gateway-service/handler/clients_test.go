package handler

import (
	"bytes"
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

// The procedure names are gated above. The payloads were not.
//
// A renamed json tag is invisible to everything: the Go side moves together,
// because the struct, its handler and the end-to-end call are all written beside
// each other. The console reads `undefined` where a number should be and draws a
// blank cell. The bench is worse — it reads a closed vocabulary off the reply and
// treats anything outside it as unrecognised, which is its word for "not
// delivered", so a renamed `outcome` leaves a morning's collections sitting in an
// outbox that will never empty.
//
// # WHAT THIS COMPARES, AND WHAT IT DOES NOT
//
// Every field name the clients use, against the json tags of the services they
// call. Not field-by-procedure: doing that properly means resolving the type of
// every nested message across two languages, and the failure actually worth
// catching — a tag renamed on the Go side — is caught by the coarser comparison
// just as surely. A field moved from one message to another inside the same
// service would pass this, and that is the stated limit.

// jsonTagsOf reads the wire field names one service answers with.
func jsonTagsOf(t *testing.T, service string) map[string]bool {
	t.Helper()
	dir := filepath.Join(repoRoot(t), "services", service, "internal", "handler")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	tag := regexp.MustCompile(`json:"([a-z0-9_]+)`)
	out := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		for _, m := range tag.FindAllSubmatch(b, -1) {
			out[string(m[1])] = true
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s's handler package declares no json tags, so the comparison below "+
			"would pass whatever the clients send", service)
	}
	return out
}

// consoleServices are the four the console talks to, and it is read out of the
// console rather than listed: a fifth added there has to be added here too, and
// this is what makes that happen at build time.
func consoleServices(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, c := range webProcedures(t) {
		out[c.procedure[:strings.LastIndex(c.procedure, "/")]] = true
	}
	return out
}

// directoryOf maps a Connect service name to the service that serves it.
//
// Read out of each service's own ServiceName constant rather than guessed from
// the package name, which would have to know that shadowsettlement.v1 is served
// by shadow-settlement-service.
func directoryOf(t *testing.T, serviceName string) string {
	t.Helper()
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "services"))
	if err != nil {
		t.Fatal(err)
	}
	want := []byte(`ServiceName = "` + serviceName + `"`)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, "services", e.Name(), "internal", "handler")
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			b, err := os.ReadFile(filepath.Join(dir, f.Name()))
			if err == nil && bytes.Contains(b, want) {
				return e.Name()
			}
		}
	}
	t.Fatalf("no service declares ServiceName = %q", serviceName)
	return ""
}

// webField matches a field in a TypeScript interface: `window_id: string;`.
var webField = regexp.MustCompile(`(?m)^\s+([a-z][a-z0-9_]*)\??:`)

// webInterface matches one interface body.
var webInterface = regexp.MustCompile(`(?s)export interface \w+ \{(.*?)\n\}`)

func TestEveryFieldTheConsoleDeclaresIsOneTheServicesSend(t *testing.T) {
	tags := map[string]bool{}
	for service := range consoleServices(t) {
		for tag := range jsonTagsOf(t, directoryOf(t, service)) {
			tags[tag] = true
		}
	}

	types := readRepoFile(t, filepath.Join("web", "src", "lib", "api", "types.ts"))
	bodies := webInterface.FindAllStringSubmatch(types, -1)
	if len(bodies) == 0 {
		t.Fatal("web/src/lib/api/types.ts declares no interfaces; the parser is reading nothing")
	}

	fields := map[string]bool{}
	for _, body := range bodies {
		for _, m := range webField.FindAllStringSubmatch(body[1], -1) {
			fields[m[1]] = true
		}
	}
	if len(fields) < 50 {
		t.Fatalf("read only %d field names out of the console's wire types; it declares "+
			"more than that, so the pattern is not matching", len(fields))
	}

	for field := range fields {
		if !tags[field] {
			t.Errorf("the console expects a field %q and no service it calls sends one. "+
				"The screen that reads it draws a blank where a figure should be.", field)
		}
	}
}

// The bench, against ingestion-service alone — the only service it speaks to.
// Tighter than the console's check for that reason: a field it uses must be one
// that service sends, not one somebody sends.
func TestEveryFieldTheBenchUsesIsOneIngestionSends(t *testing.T) {
	tags := jsonTagsOf(t, "ingestion-service")

	// The wire layer only. The capture and outbox code has keys of its own for
	// what it stores on the device, which are nobody else's business.
	wire := readRepoFile(t, filepath.Join("mobile", "lib", "src", "api", "ingestion.dart"))

	// A map key being written, and a reply being read.
	written := regexp.MustCompile(`'([a-z][a-z0-9_]*)'\s*:`)
	read := regexp.MustCompile(`\['([a-z][a-z0-9_]*)'\]`)

	fields := map[string]bool{}
	for _, re := range []*regexp.Regexp{written, read} {
		for _, m := range re.FindAllStringSubmatch(wire, -1) {
			fields[m[1]] = true
		}
	}
	if len(fields) < 15 {
		t.Fatalf("read only %d field names out of the bench's wire layer; it uses more "+
			"than that, so the patterns are not matching", len(fields))
	}

	for field := range fields {
		if !tags[field] {
			t.Errorf("the bench sends or reads %q and ingestion-service has no such "+
				"field. On a reply that means the value arrives null; on a request it "+
				"means the server never sees what the device sent.", field)
		}
	}
}

// The bench's error vocabulary, against the platform's.
//
// Not a field but the same kind of contract, and the consequence is sharper. The
// outbox decides whether a record may be sent again from the code that came
// back, and a code it does not recognise is one it will not act on. Rename one
// on the Go side and the device stops distinguishing "the server is down, try
// later" from "this record can never be accepted".
func TestTheBenchKnowsEveryCodeThePlatformSends(t *testing.T) {
	platform := readRepoFile(t, filepath.Join("libs", "integrity", "svcclient", "client.go"))
	table := regexp.MustCompile(`"([a-z_]+)":\s+connect\.Code`).FindAllStringSubmatch(platform, -1)
	if len(table) == 0 {
		t.Fatal("read no Connect code names out of svcclient; the pattern no longer matches")
	}

	bench := readRepoFile(t, filepath.Join("mobile", "lib", "src", "api", "connect.dart"))

	// "unknown" has no case of its own and needs none: it is what the default
	// arm answers, so the wire name and the enum value already agree. That the
	// default is unknown rather than something else is the thing to check, and
	// it is checked — a default of, say, unavailable would quietly make every
	// unrecognised refusal look retryable, which for a device holding a day's
	// collections is the difference between a retry and a double count.
	const fallback = "unknown"
	if !strings.Contains(bench, "default:\n        return ConnectCode."+fallback+";") {
		t.Errorf("the bench's code parser does not fall back to %s. An unrecognised "+
			"code has to be the one the outbox will not act on.", fallback)
	}

	for _, m := range table {
		if m[1] == fallback {
			continue
		}
		if !strings.Contains(bench, `case '`+m[1]+`':`) {
			t.Errorf("the platform sends the code %q and the bench does not parse it, so "+
				"it arrives as unknown and the outbox cannot tell a retry from a "+
				"refusal", m[1])
		}
	}
}
