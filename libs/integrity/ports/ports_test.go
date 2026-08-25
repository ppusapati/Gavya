package ports

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The defect this package exists to prevent: five services claimed a port
// another service already had, so on a developer's machine only one of each
// pair could start, and which one depended on the order they were launched in.
func TestNoTwoServicesClaimTheSamePort(t *testing.T) {
	byPort := map[int][]string{}
	for name, p := range All {
		byPort[p] = append(byPort[p], name)
	}
	for p, names := range byPort {
		if len(names) > 1 {
			t.Errorf("port %d is claimed by %v; only one of them could ever start", p, names)
		}
	}
}

// Ports below 1024 need privileges nobody should be granting a dairy service,
// and above 65535 is not a port.
func TestEveryPortIsUsable(t *testing.T) {
	for name, p := range All {
		if p < 1024 || p > 65535 {
			t.Errorf("%s is assigned %d, which is not a port a service can take unprivileged", name, p)
		}
	}
}

func TestAddrAndURLAgree(t *testing.T) {
	if got := Addr(Tenant); got != ":8080" {
		t.Errorf("Addr(Tenant) = %q, want \":8080\"", got)
	}
	if got := LocalURL(Tenant); got != "http://localhost:8080" {
		t.Errorf("LocalURL(Tenant) = %q", got)
	}
}

// The integrity services are deployed and reasoned about as one unit, so their
// block stays contiguous. A new service dropped into the middle of it would be
// a surprise to whoever reads the allocation next.
func TestTheIntegrityBlockIsContiguous(t *testing.T) {
	block := map[string]int{
		"shadow-settlement": ShadowSettlement,
		"ingestion":         Ingestion,
		"observation":       Observation,
		"canonical":         Canonical,
		"pooling":           Pooling,
		"balance":           Balance,
	}
	low, high := ShadowSettlement, Balance
	if high-low+1 != len(block) {
		t.Errorf("the integrity block spans %d ports for %d services, so it is not contiguous",
			high-low+1, len(block))
	}
	for name, p := range All {
		if _, inBlock := block[name]; inBlock {
			continue
		}
		if p >= low && p <= high {
			t.Errorf("%s sits on %d, inside the integrity block %d-%d", name, p, low, high)
		}
	}
}

// Every service in the repository should have an entry, or its default is
// nobody's decision.
// TestTheTableCoversTheKnownServices reads the services off disk rather than
// from a list here.
//
// A list is a thing somebody has to remember to add a new service to, and
// forgetting means the service has no port, falls back to whatever its own
// default is, and collides with something — which is the failure this package
// was written to end. Deriving it means a directory appearing under services/
// is enough.
func TestTheTableCoversTheKnownServices(t *testing.T) {
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "services"))
	if err != nil {
		t.Skipf("no services directory at %s: %v", root, err)
	}

	var checked int
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Directories are named "<thing>-service"; the table is keyed by the
		// thing.
		name := strings.TrimSuffix(e.Name(), "-service")
		if name == e.Name() {
			continue
		}
		checked++
		if _, ok := All[name]; !ok {
			t.Errorf("services/%s has no port in this table, so it will fall back to its own "+
				"default and collide with something", e.Name())
		}
	}
	if checked == 0 {
		t.Fatal("no services were found, so this test would pass vacuously")
	}
	t.Logf("%d services on disk, %d ports assigned", checked, len(All))
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.work above the working directory")
		}
		dir = parent
	}
}
