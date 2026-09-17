package handler

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ppusapati/gavya/libs/integrity/tenantdb"
)

// Two defaults, neither of them chosen, multiplying.
//
// pgxpool sizes a pool at the greater of four and the number of CPUs unless told
// otherwise, and nothing told it otherwise. PostgreSQL allows 100 connections
// unless told otherwise, and nothing told it otherwise. Twenty-eight services
// open a pool against the same database, so on the four-core machine the load
// test ran on the platform's ceiling was 112 against a limit of 100, and on an
// eight-core host 224.
//
// Nothing was wrong with either default on its own, and nothing anywhere put the
// two numbers on the same page. That is what this test is: the page.
//
// It does not check that the numbers are good ones — it cannot, and a test that
// claimed to would be inventing a capacity model. It checks that they add up,
// which is the property that stopped holding without anybody doing anything.
//
// # WHAT IT DOES NOT COVER
//
// Kubernetes. The manifests under services/*/deployments/k8s point every service
// at postgres:5432 and this repository does not deploy that PostgreSQL — it is a
// managed instance or somebody else's StatefulSet, and its max_connections is
// set where that lives. The arithmetic is the same and the answer is on this
// page: twenty-eight pools of tenantdb.MaxConns, plus tenantdb.Headroom. It is
// left unchecked rather than checked against a number this repository invented,
// because a test comparing a manifest to a limit nobody here controls would pass
// while the cluster ran out of connections.
//
// RUN WITH -count=1, as with every check in this package: the compose files sit
// outside this module and Go's test cache does not track them.
func TestThePoolsFitTheDatabase(t *testing.T) {
	root := repoRoot(t)
	pools := servicesThatOpenAPool(t, root)

	// Sanity on the count itself. A regex that matched nothing would make the
	// arithmetic below trivially true, which is the shape of a check that
	// reports success while doing nothing.
	if pools < 20 {
		t.Fatalf("found only %d services opening a pool; there are twenty-eight, "+
			"so the scan below is not reading what it should", pools)
	}

	want := pools*int(tenantdb.MaxConns) + tenantdb.Headroom
	for _, file := range []string{"docker-compose.yaml", "docker-compose.modulith.yaml"} {
		limit := maxConnectionsIn(t, root, file)
		if limit < want {
			t.Errorf("%s allows %d connections and the platform can ask for %d "+
				"(%d pools x %d, plus %d headroom). Under load the services that "+
				"ask last are refused with \"sorry, too many clients already\".",
				file, limit, want, pools, tenantdb.MaxConns, tenantdb.Headroom)
		}
	}
}

// The modulith does not merge the databases and so it does not merge the pools:
// each of its modules opens its own, exactly as it does when it runs alone. One
// container is not a smaller number of connections, and its compose file has to
// say the same thing the other one does.
//
// Checked separately from the arithmetic above because the two failures are
// different. Above is "the limit is too low"; this is "somebody raised the limit
// in the file they were looking at and not in the other one", which is how these
// two files have drifted before.
func TestBothShapesDeclareTheSameLimit(t *testing.T) {
	root := repoRoot(t)
	separate := maxConnectionsIn(t, root, "docker-compose.yaml")
	modulith := maxConnectionsIn(t, root, "docker-compose.modulith.yaml")
	if separate != modulith {
		t.Errorf("docker-compose.yaml allows %d connections and "+
			"docker-compose.modulith.yaml allows %d. The modulith opens one pool "+
			"per module, so it needs the same number, not fewer.",
			separate, modulith)
	}
}

// servicesThatOpenAPool counts the services that actually hold one.
//
// Counted from the code rather than from a compose file, because the code is
// what opens the connection. The gateway is the reason the distinction matters:
// it is a service, it is in both compose files, and it proxies without ever
// touching a database.
func servicesThatOpenAPool(t *testing.T, root string) int {
	t.Helper()
	// NewPool and NewPoolFor both. Twenty-seven services open a pool in their
	// own schema and one — audit-service, whose table is the shared audit trail
	// — opens one in public; all twenty-eight hold a pool and count here.
	opens := regexp.MustCompile(`tenantdb\.NewPool(For)?\(`)

	found := map[string]bool{}
	err := filepath.WalkDir(filepath.Join(root, "services"), func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") {
			return err
		}
		// The modulith imports every service's app package and runs all of them
		// in one process. Counting it as well would count every pool twice.
		rel, relErr := filepath.Rel(filepath.Join(root, "services"), path)
		if relErr != nil {
			return relErr
		}
		service := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		if service == "modulith" {
			return nil
		}

		b, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if opens.Match(b) {
			found[service] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return len(found)
}

// maxConnectionsIn reads the limit out of the postgres service's command line.
func maxConnectionsIn(t *testing.T, root, file string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, file))
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	m := regexp.MustCompile(`max_connections=(\d+)`).FindSubmatch(b)
	if m == nil {
		t.Fatalf("%s does not set max_connections on its PostgreSQL, so the limit "+
			"is PostgreSQL's default of 100 and the platform can ask for more "+
			"than that", file)
	}
	n, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatalf("%s: max_connections=%s is not a number", file, m[1])
	}
	return n
}
