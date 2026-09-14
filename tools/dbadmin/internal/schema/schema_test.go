package schema

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The runner applies what the init script applies, in the same order.
//
// There are two copies of this sequence and there have to be. deploy/postgres-init
// runs inside the PostgreSQL container, as a shell script, when a data directory
// is first created; there is no Go there. The runner applies the same thing to a
// database that already exists. Neither can call the other.
//
// Two copies of an order this particular will not stay in step on their own, and
// the failure would be quiet: a database migrated in the wrong order comes up,
// answers, and has row-level security missing from whichever table the sweep
// could not see yet. So the order is compared rather than trusted.
//
// What is compared is the sequence of SQL files applied and the mutating
// functions called between them. Not the guards at the end — those are assertions
// about the result rather than steps, and both copies carry them in the form
// their language makes readable.
func TestTheRunnerAndTheInitScriptApplyTheSameThingsInTheSameOrder(t *testing.T) {
	root := repoRoot(t)

	script, err := os.ReadFile(filepath.Join(root, "deploy", "postgres-init", "00-schema-and-isolation.sh"))
	if err != nil {
		t.Fatalf("read the init script, which is the other copy of this order: %v", err)
	}

	// The script says where applying stops and verifying starts, so this does
	// not have to infer it. Calls below that line — the reference enforcement is
	// run again to report what it refused — are not steps.
	const applyEnds = "---- APPLY ENDS HERE ----"
	text := string(script)
	if i := strings.Index(text, applyEnds); i >= 0 {
		text = text[:i]
	} else {
		t.Fatalf("the init script has no %q marker. It says where applying stops and "+
			"verifying starts; without it this test counts a verification as a step "+
			"and fails on an order the two copies actually agree on.", applyEnds)
	}

	want := fromScript(t, text)
	got := fromPlan(Plan())

	if len(want) < 6 {
		t.Fatalf("only %d steps were read out of the init script, and it has more than that. "+
			"The patterns below have stopped matching it, and a comparison that finds "+
			"nothing passes.\nread: %v", len(want), want)
	}

	if strings.Join(want, "\n") != strings.Join(got, "\n") {
		t.Errorf("the runner and deploy/postgres-init no longer apply the same sequence.\n\n"+
			"init script:\n  %s\n\nrunner (internal/schema.Plan):\n  %s\n\n"+
			"A database migrated in the wrong order comes up and answers with row-level "+
			"security missing from whichever table the sweep could not see. Whichever of "+
			"the two is right, make the other match it.",
			strings.Join(want, "\n  "), strings.Join(got, "\n  "))
	}
}

// mutating are the functions that change the database rather than report on it.
//
// The guards at the end of both copies also call gavya_ functions —
// gavya_undecided_references, and a count of grants — and those are assertions
// about the finished state rather than steps in building it. Including them
// would make this test fail whenever one copy phrased a check differently from
// the other, which is not the drift it is here to catch.
var mutating = map[string]bool{
	"gavya_apply_tenant_isolation":        true,
	"gavya_grant_app_access":              true,
	"gavya_make_foreign_keys_tenant_safe": true,
	"gavya_enforce_references":            true,
}

var (
	fileArg   = regexp.MustCompile(`--file\s+/repo/(\S+)`)
	loopGlob  = regexp.MustCompile(`for\s+\w+\s+in\s+/repo/(\S+);`)
	funcCall  = regexp.MustCompile(`(gavya_[a-z_]+)\s*\(`)
	anyOfThem = regexp.MustCompile(`--file\s+/repo/\S+|for\s+\w+\s+in\s+/repo/\S+;|gavya_[a-z_]+\s*\(`)
)

// fromScript reads the sequence out of the shell script.
//
// A loop over a glob is the glob: the line inside it applies "$f", which names
// nothing on its own.
func fromScript(t *testing.T, src string) []string {
	t.Helper()
	var out []string
	seenLoop := false
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || !anyOfThem.MatchString(line) {
			continue
		}
		if m := loopGlob.FindStringSubmatch(line); m != nil {
			out = append(out, "glob "+m[1])
			seenLoop = true
			continue
		}
		if m := fileArg.FindStringSubmatch(line); m != nil {
			out = append(out, "file "+m[1])
			continue
		}
		// The apply line inside a loop names "$f" and is already covered by the
		// glob above it.
		if strings.Contains(line, `--file "$`) {
			if !seenLoop {
				t.Errorf("a loop applies a file and no glob was read before it: %q", line)
			}
			continue
		}
		for _, m := range funcCall.FindAllStringSubmatch(line, -1) {
			if mutating[m[1]] {
				out = append(out, "call "+m[1])
			}
		}
	}
	return out
}

func fromPlan(plan []Step) []string {
	var out []string
	for _, s := range plan {
		switch {
		case s.File != "":
			out = append(out, "file "+s.File)
		case s.Glob != "":
			out = append(out, "glob "+s.Glob)
		}
		for _, m := range funcCall.FindAllStringSubmatch(s.Command, -1) {
			if mutating[m[1]] {
				out = append(out, "call "+m[1])
			}
		}
	}
	return out
}

// Every file the plan names exists, so a typo in a path is a failing test rather
// than a migration that stops halfway through a rollout.
func TestEveryFileThePlanNamesIsThere(t *testing.T) {
	root := repoRoot(t)
	for _, step := range Plan() {
		files, err := step.Files(root)
		if err != nil {
			t.Errorf("%s: %v", step.Describe, err)
			continue
		}
		if step.Glob != "" && !step.Optional && len(files) == 0 {
			t.Errorf("%s matched nothing", step.Glob)
		}
		for _, f := range files {
			if _, err := os.Stat(f); err != nil {
				t.Errorf("%s: %v", step.Describe, err)
			}
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// .../tools/dbadmin/internal/schema -> repository root
	for range 4 {
		wd = filepath.Dir(wd)
	}
	if _, err := os.Stat(filepath.Join(wd, "go.work")); err != nil {
		t.Fatalf("no go.work above the test's working directory: %v", err)
	}
	return wd
}
