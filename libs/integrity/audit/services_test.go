package audit_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Every service either writes to the trail or says why it does not.
//
// # WHAT WAS ACTUALLY THE CASE
//
// The register beside this one covers updates: a function that overwrites a row
// either records what the row said before, or is named with a reason. It cannot
// see a service that writes nothing at all, because a service with no update
// functions has nothing for it to look at.
//
// Four services write no audit entry of any kind, and until this test that was
// not a decision anybody had written down — it was four absences that happened
// to have the same shape. Three of them were defensible and one had never been
// looked at.
//
// # WHAT THIS TEST IS FOR
//
// The same thing the update register is for, one level up. The question "should
// this service be on the trail" is answered once, in writing, and a service that
// changes its mind in either direction fails the build rather than drifting.
//
// It deliberately does not ask how much a service writes. A service that writes
// one entry has made the decision; whether it writes it in the right places is
// what the update register and the service's own tests are for.
//
// RUN WITH -count=1. It reads Go files in other modules, which the test cache
// does not track.
func TestEveryServiceIsOnTheTrailOrSaysWhyNot(t *testing.T) {
	root := repoRoot(t)
	dirs, err := filepath.Glob(filepath.Join(root, "services", "*-service"))
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) < 20 {
		t.Fatalf("found only %d services; the glob has probably stopped matching, "+
			"and a check that finds nothing passes", len(dirs))
	}

	var silent, writing []string
	for _, dir := range dirs {
		name := filepath.Base(dir)
		if writesAuditEntries(t, dir) {
			writing = append(writing, name)
			continue
		}
		silent = append(silent, name)
	}
	sort.Strings(silent)
	sort.Strings(writing)

	for _, name := range silent {
		if _, ok := noAuditTrail[name]; !ok {
			t.Errorf("%s writes no audit entry anywhere and is not in noAuditTrail. "+
				"Either it records the changes somebody could be asked to defend, or "+
				"the reason it does not goes in that list — an absence nobody wrote "+
				"down is the thing this test exists to stop.", name)
		}
	}

	// A register that is never pruned stops describing anything.
	for name, why := range noAuditTrail {
		found := false
		for _, s := range silent {
			if s == name {
				found = true
				break
			}
		}
		if found {
			continue
		}
		if contains(writing, name) {
			t.Errorf("%s is listed as writing no audit entry and now writes one, so "+
				"delete its entry from noAuditTrail (%s)", name, why)
			continue
		}
		t.Errorf("%q is in noAuditTrail and is not a service in this repository (%s)", name, why)
	}

	t.Logf("%d services write to the trail, %d do not and say why", len(writing), len(silent))
}

// noAuditTrail is the services that write nothing, and why that is right.
//
// The reasons are of two kinds, and the difference matters. Three of these have
// nothing to record: the trail itself, a proxy that decides nothing, and one
// person marking their own mail read. The fourth records real things and the
// argument is about what the trail is for.
var noAuditTrail = map[string]string{
	"audit-service": "it is the trail. An entry recording that an entry was " +
		"written is the same fact twice, and the chain is what makes the trail " +
		"tamper-evident rather than a second copy of it.",

	"gateway-service": "it decides nothing and stores nothing. It authorises " +
		"against the procedure it is about to proxy and forwards; the service " +
		"that acts is the one with something to record.",

	"notification-service": "its writes are one recipient's own inbox — marking " +
		"a message read, or all of them. Nobody disputes who read a notification, " +
		"and the message it is about was recorded by the service that sent it.",

	// This one was an open question rather than a decision, and was left open in
	// the roadmap until somebody looked. Looking is what produced the reason.
	"feed-service": "it records what a herd is fed: a feed type, a standing " +
		"ration, and what was actually given. Three properties together make an " +
		"entry a second copy rather than a record. Nothing in its schema is " +
		"money, and no producer is paid on any of it. Nothing outside the service " +
		"reads it, so no other service's answer depends on it. And it has no " +
		"update or delete path at all — three creates, nothing else — so no " +
		"figure it holds can be overwritten or destroyed, which is the thing a " +
		"before-image exists to catch. Every row already carries created_by, and " +
		"a consumption row carries fed_by and fed_at besides. Give this service " +
		"an edit path and this entry stops being true.",
}

// writesAuditEntries reports whether a service calls audit.Write anywhere in its
// own non-test source.
//
// A textual search rather than a parse, deliberately: the question is whether
// the service has taken a position on the trail at all, and the answer is the
// same whether the call is in a repository, a helper or a transition. The
// register beside this one is the one that cares where.
func writesAuditEntries(t *testing.T, dir string) bool {
	t.Helper()
	found := false
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || found {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(src), "audit.Write(") {
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return found
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
