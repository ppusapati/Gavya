package audit_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every function that updates a row records what the row said before.
//
// # WHAT WAS ACTUALLY THE CASE
//
// Seventy functions in this repository run an UPDATE. Twenty-nine wrote an audit
// entry and twenty-one of those carried a before-image — so for forty-one
// changes there was no record that anything had happened at all, and for eight
// more there was a record of what the row became and none of what it had been.
//
// The row afterwards carries updated_by and updated_at, which is what makes this
// invisible: every table looks like it remembers. It remembers who touched it
// last. "Who gave this person the approver role, and what did they have before"
// had no answer from the service whose whole subject is who may do what.
//
// # WHAT THIS TEST IS FOR
//
// Not to reach zero. Some updates genuinely need no before-image and the list
// below says which and why. It is for the next one: an update added without a
// trail is a decision somebody should have to write down, and today it is a
// decision nobody notices making.
//
// So every update function is either audited with a before-image, or named here
// with a reason. Both lists are visible, which is the whole of the improvement:
// the forty-one were not a decision, they were an absence.
//
// RUN WITH -count=1. It parses Go files in other modules, which the test cache
// does not track.
func TestEveryUpdateRecordsWhatItOverwrote(t *testing.T) {
	root := repoRoot(t)
	dirs, err := filepath.Glob(filepath.Join(root, "services", "*-service"))
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) < 20 {
		t.Fatalf("found only %d services; the glob has probably stopped matching, "+
			"and a check that finds nothing passes", len(dirs))
	}

	found := updateFunctions(t, dirs)
	// A trail written in a shared helper counts for the functions that call it.
	//
	// Not an indulgence: the four identity-service updates that this work added a
	// before-image to share one helper, because the part that is easy to get
	// wrong — read the before under the lock, write both inside the transaction
	// that makes the change — is identical in all four. A detector that only
	// looks inside one function body reports all four as unaudited, which is how
	// a check ends up arguing for worse code.
	spreadThroughHelpers(t, dirs, found)
	if len(found) < 50 {
		t.Fatalf("found only %d update functions, and there were seventy. The "+
			"detector has probably stopped matching, and a check that finds "+
			"nothing passes", len(found))
	}

	var unrecorded, staleExemption []string
	seen := map[string]bool{}
	for _, u := range found {
		key := u.service + "." + u.function
		seen[key] = true
		if u.audited && u.before {
			// Recorded. If it is also exempted, the exemption is out of date and
			// saying so keeps the list from accumulating entries nobody rechecks.
			if _, exempt := noBeforeImage[key]; exempt {
				staleExemption = append(staleExemption,
					key+" is exempted and does carry a before-image")
			}
			continue
		}
		if _, exempt := noBeforeImage[key]; exempt {
			continue
		}
		what := "writes no audit entry at all"
		if u.audited {
			what = "records what the row became and not what it was"
		}
		unrecorded = append(unrecorded, key+" ("+u.tables+") "+what)
	}

	for key, reason := range noBeforeImage {
		if !seen[key] {
			staleExemption = append(staleExemption,
				key+" is exempted and no longer exists")
		}
		// An exemption is a reason. "OPEN" was how this list carried the ones
		// with no reason yet, and it worked — sixteen were closed — so it is now
		// refused: a gap goes in the code or in a real reason, not in a list.
		if strings.HasPrefix(strings.TrimSpace(reason), "OPEN") {
			staleExemption = append(staleExemption,
				key+" is marked OPEN, which is a to-do and not a reason")
		}
	}

	sort.Strings(unrecorded)
	sort.Strings(staleExemption)

	if len(unrecorded) > 0 {
		t.Errorf("%d update functions overwrite a row with no record of what it "+
			"said:\n  %s\n"+
			"The row afterwards names whoever touched it last, which is what makes "+
			"this invisible — every table looks like it remembers. Either write the "+
			"trail, or add it to noBeforeImage with the reason it does not need one.",
			len(unrecorded), strings.Join(unrecorded, "\n  "))
	}
	if len(staleExemption) > 0 {
		t.Errorf("%d exemptions are out of date:\n  %s\n"+
			"An exemption nobody rechecks is how a list like this stops meaning "+
			"anything.", len(staleExemption), strings.Join(staleExemption, "\n  "))
	}

	audited := 0
	for _, u := range found {
		if u.audited && u.before {
			audited++
		}
	}
	t.Logf("%d update functions, %d carry a before-image, %d exempted",
		len(found), audited, len(noBeforeImage))
}

// noBeforeImage is every update that does not record what it overwrote, and why.
//
// Three kinds of entry, and the difference matters:
//
//   - "nothing to record" — the update fills in a field that was empty, or
//     writes machine state nobody disputes. A before-image of null is noise.
//   - "recorded elsewhere" — the change is written to a table of its own that
//     keeps both states, so an audit entry would be a second copy.
//
// There is no "OPEN" kind any more. There was: twenty-one updates that should
// have had a before-image and did not, listed so they were countable rather than
// invisible. Fourteen now record one and the rest were read and given a reason,
// and the test below refuses an entry whose reason begins with OPEN, so the list
// cannot quietly become a to-do list again.
var noBeforeImage = map[string]string{
	// Nothing to record.
	"ingestion-service.advanceSession":           "machine state: a capture session moving through its own lifecycle, driven by the device rather than by a person",
	"ingestion-service.quarantineSession":        "machine state: the session is being quarantined because something already failed, and the failure is the record",
	"ingestion-service.CloseSession":             "machine state: a session ends when the device stops sending, which nobody decides",
	"ingestion-service.RollGeneration":           "machine state: a device's key generation advances on its own schedule",
	"observation-service.AttachAnomaly":          "fills in a field that was empty: an ML estimate arriving beside a reading the platform already recorded deterministically",
	"observation-service.AttachUncertainty":      "fills in a field that was empty, as above",
	"shadow-settlement-service.AttachHypotheses": "fills in a field that was empty: hypotheses attached to a divergence already recorded",
	"pooling-service.SaveValuation":              "the first write of a valuation for a pool; there is no previous one to record",
	"settlement-service.Gather":                  "gathering populates an empty cycle from the collections; the before is empty by construction and the collections are the record",
	"settlement-service.NotificationDelivered":   "machine state: the delivery bookkeeping of a queued message, which nobody decides",
	"settlement-service.NotificationFailed":      "machine state: one failed try at delivering a queued message, and why",

	// Recorded elsewhere.
	"shadow-settlement-service.SupersedeComputation":  "both states are rows: superseding keeps the old computation and writes a new one, and the pair is the record",
	"shadow-settlement-service.SupersedeAssertion":    "both states are rows, as above",
	"observation-service.SupersedeObservation":        "both states are rows: the superseded observation stays readable and the replacement points at it",
	"laboratory-service.BreakSeal":                    "the seal's own chain of custody records the state either side; a second copy would be a second thing to keep in step",
	"ingestion-service.ResolveQuarantine":             "the quarantined record itself is the before, and it is kept",
	"canonical-service.SupersedeIdentity":             "the row keeps superseded_at and superseded_by and stays in the table, and GetIdentityHistory returns it — which it did not until this work, and the exemption was not true before that",
	"canonical-service.applyDecision":                 "both states are rows: a slot keeps every claim made against it and the decision that resolved them",
	"balance-service.CreateRun":                       "fills in a field that was empty: a window is claimed by the run that is about to reconcile it",
	"breeding-service.RecordInseminationInCycle":      "fills in a field that was empty: a cycle recording the event that has just happened to it",
	"breeding-service.ConfirmPregnancyForCycle":       "fills in a field that was empty, as above",
	"breeding-service.RecordCalvingAndClosePregnancy": "fills in a field that was empty, as above",

	// Decided not to record, on reading each one.
	//
	// These were the twenty-one "OPEN" entries — updates that should have had a
	// before-image and did not. Fourteen now record one. These five were read
	// and found not to be a decision anybody would ask about afterwards, and
	// two more were found to be a consequence of a decision that is already
	// recorded with its before-image.
	"notification-service.MarkNotificationRead":     "a reader marking their own mail read; the before is unread by definition and nobody asks who read a notification",
	"notification-service.MarkAllNotificationsRead": "as above, for all of one recipient's mail at once",
	"notification-service.UpdateNotificationStatus": "the inbox's own bookkeeping of a message's state, not a change to anything the message is about",
	"reporting-service.UpdateReportStatus":          "machine state: a report job moving queued → running → done, driven by the worker rather than by a person",
	"reporting-service.UpdateScheduleActive":        "a boolean toggle whose before is the negation of its after, so the after alone is the whole record",
	"identity-service.RevokeSession":                "the row keeps revoked_at and revoked_reason and names its own user; this runs on the sign-out path, where an audit write refused for want of an actor would stop a person signing out — a worse failure than the gap",
	"identity-service.RevokeSessionsFor":            "a consequence of a suspension or a password change, each of which is recorded with its before-image; the rows keep when and why",
}

// spreadThroughHelpers marks an update as audited when a function it calls does
// the auditing.
//
// One level of propagation, repeated until nothing changes, within a service.
// Deliberately not across services: an audit entry written by a different
// service is a different service's trail.
func spreadThroughHelpers(t *testing.T, dirs []string, found []update) {
	t.Helper()
	// Every function in the repository that audits with a before-image, whether
	// or not it runs an UPDATE itself — which is the case for a helper.
	auditing := map[string]bool{}
	for _, dir := range dirs {
		service := filepath.Base(dir)
		for _, layer := range []string{
			"internal/repository", "internal/service", "internal/handler",
			"repository", "service", "handler",
		} {
			fset := token.NewFileSet()
			pkgs, err := parser.ParseDir(fset, filepath.Join(dir, layer),
				func(fi fs.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
			if err != nil {
				continue
			}
			for _, pkg := range pkgs {
				for _, file := range pkg.Files {
					for _, decl := range file.Decls {
						fn, ok := decl.(*ast.FuncDecl)
						if !ok || fn.Body == nil {
							continue
						}
						var audited, before bool
						ast.Inspect(fn.Body, func(n ast.Node) bool {
							switch node := n.(type) {
							case *ast.SelectorExpr:
								if node.Sel.Name == "Write" {
									if id, ok := node.X.(*ast.Ident); ok && id.Name == "audit" {
										audited = true
									}
								}
							case *ast.CompositeLit:
								if sel, ok := node.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "Entry" {
									for _, e := range node.Elts {
										if kv, ok := e.(*ast.KeyValueExpr); ok {
											if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Before" {
												before = true
											}
										}
									}
								}
							}
							return true
						})
						if audited && before {
							auditing[service+"."+fn.Name.Name] = true
						}
					}
				}
			}
		}
	}

	for changed := true; changed; {
		changed = false
		for i := range found {
			u := &found[i]
			if u.audited && u.before {
				continue
			}
			for _, callee := range u.calls {
				if auditing[u.service+"."+callee] {
					u.audited, u.before, changed = true, true, true
					auditing[u.service+"."+u.function] = true
					break
				}
			}
		}
	}
}

// update is one function that runs an UPDATE.
type update struct {
	service, function, tables string
	audited, before           bool
	// calls names the functions and methods this one invokes, so a trail
	// written in a shared helper counts for the functions that use it.
	calls []string
}

var updateStatement = regexp.MustCompile(`(?is)\bUPDATE\s+([a-z_][a-z_0-9]*)\s+SET\b`)

// updateFunctions finds every function containing an UPDATE and reports whether
// it writes an audit entry and whether that entry carries a before-image.
//
// Parsed rather than grepped, because the question is per function: a file with
// an UPDATE somewhere and an audit.Write somewhere else tells you nothing, and
// that is exactly the shape a grep would call covered.
func updateFunctions(t *testing.T, dirs []string) []update {
	t.Helper()
	var found []update
	for _, dir := range dirs {
		service := filepath.Base(dir)
		for _, layer := range []string{
			"internal/repository", "internal/service", "internal/handler",
			"repository", "service", "handler",
		} {
			fset := token.NewFileSet()
			pkgs, err := parser.ParseDir(fset, filepath.Join(dir, layer),
				func(fi fs.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
			if err != nil {
				continue
			}
			for _, pkg := range pkgs {
				for _, file := range pkg.Files {
					for _, decl := range file.Decls {
						fn, ok := decl.(*ast.FuncDecl)
						if !ok || fn.Body == nil {
							continue
						}
						if u, ok := inspectFunc(service, fn); ok {
							found = append(found, u)
						}
					}
				}
			}
		}
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].service != found[j].service {
			return found[i].service < found[j].service
		}
		return found[i].function < found[j].function
	})
	return found
}

func inspectFunc(service string, fn *ast.FuncDecl) (update, bool) {
	u := update{service: service, function: fn.Name.Name}
	tables := map[string]bool{}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.BasicLit:
			if node.Kind == token.STRING {
				for _, m := range updateStatement.FindAllStringSubmatch(node.Value, -1) {
					tables[m[1]] = true
				}
			}
		case *ast.SelectorExpr:
			if node.Sel.Name == "Write" {
				if id, ok := node.X.(*ast.Ident); ok && id.Name == "audit" {
					u.audited = true
				}
			}
		case *ast.CallExpr:
			// r.recordedUpdate(...) or plain helper(...). Recorded by name only:
			// resolving the receiver's type needs type information this does not
			// load, and a name collision inside one package is not a thing Go
			// allows anyway.
			switch fun := node.Fun.(type) {
			case *ast.Ident:
				u.calls = append(u.calls, fun.Name)
			case *ast.SelectorExpr:
				u.calls = append(u.calls, fun.Sel.Name)
			}
		case *ast.CompositeLit:
			// audit.Entry{...} with a Before field. The field being present is
			// the claim; whether the value is meaningful is not something a
			// parser can judge, and the tests that drive these paths are.
			if sel, ok := node.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "Entry" {
				for _, e := range node.Elts {
					if kv, ok := e.(*ast.KeyValueExpr); ok {
						if k, ok := kv.Key.(*ast.Ident); ok && k.Name == "Before" {
							u.before = true
						}
					}
				}
			}
		}
		return true
	})

	if len(tables) == 0 {
		return update{}, false
	}
	names := make([]string, 0, len(tables))
	for name := range tables {
		names = append(names, name)
	}
	sort.Strings(names)
	u.tables = strings.Join(names, ",")
	return u, true
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// .../libs/integrity/audit -> repository root
	return filepath.Dir(filepath.Dir(filepath.Dir(wd)))
}
