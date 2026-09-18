package handler

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/ppusapati/gavya/libs/integrity/ports"
)

// RUN THESE WITH -count=1.
//
// Every file read here — the Dockerfiles, go.work, the workflow — sits outside
// this module, and Go's test cache does not track them. Edit a Dockerfile
// without touching a Go file and `go test ./...` reports a cached pass, which
// is the failure these were written to prevent, arriving through the test that
// prevents it. scripts/check-all.sh passes -count=1.

// The image is built from the workspace the gate tested, not from the module.
//
// # WHAT THIS IS ABOUT
//
// Every service Dockerfile used to copy pkg, libs and its own directory, and
// build the module on its own. That works, in the sense that a binary comes out.
// The binary is linked against different dependencies from the one every test in
// this repository ran against.
//
// Measured, on gateway-service, the morning this was written:
//
//	                        module alone   workspace (what the gate tests)
//	github.com/jackc/pgx/v5    v5.7.6            v5.10.0
//	golang.org/x/crypto        v0.38.0           v0.56.0
//	connectrpc.com/connect     v1.19.1           v1.20.0
//	golang.org/x/net           v0.40.0           v0.58.0
//
// That is the database driver, the library that hashes every password, the
// transport every procedure is served over, and the HTTP/2 stack. Minimal
// version selection resolves across all the modules in a workspace together, so
// the workspace lands higher; a module resolved alone takes only what its own
// go.mod requires. Both are valid module graphs. Only one of them has ever had a
// test run against it, and it was not the one in the images.
//
// So every Dockerfile copies go.work, go.work.sum, and enough of every module
// go.work lists for the graph to load — sources for what it builds, a bare
// go.mod for the rest.
//
// # THE ONE THAT COULD NOT BUILD
//
// services/modulith/Dockerfile already copied go.work and copied none of ./e2e,
// ./tools/amcu or ./tools/dbadmin. The first `go build` stopped at "cannot load
// module ../../e2e listed in go.work file: no such file or directory". The shape
// that ships was the one image in the repository that could not be built, and
// nothing said so because nothing built any of them.
//
// This is that failure written down: a module added to go.work now fails this
// test until every Dockerfile carries it.
func TestEveryImageIsBuiltFromTheWorkspaceThatWasTested(t *testing.T) {
	root := repoRoot(t)
	modules := workspaceModules(t, root)
	if len(modules) < 30 {
		t.Fatalf("go.work lists %d modules, which is too few to be the whole workspace; "+
			"this test is reading the wrong file", len(modules))
	}

	for _, df := range dockerfiles(t, root) {
		name := filepath.Base(filepath.Dir(df))
		body, err := os.ReadFile(df)
		if err != nil {
			t.Fatalf("read %s: %v", df, err)
		}
		copied := copiedPaths(string(body))

		for _, want := range []string{"go.work", "go.work.sum"} {
			if !copiesFile(copied, want) {
				t.Errorf("%s does not copy %s, so the build resolves this module on its "+
					"own and links dependency versions no test in this repository has "+
					"been run against.", name, want)
			}
		}

		for _, mod := range modules {
			if !copiesModule(copied, mod) {
				t.Errorf("%s copies go.work but not %s, which go.work lists. The build "+
					"stops before it compiles anything, with \"cannot load module\". "+
					"Copy the whole directory if the image needs its code, or just "+
					"%s/go.mod if it only has to be in the module graph.", name, mod, mod)
			}
		}
	}
}

// The comparison above notices a Dockerfile that stopped carrying a module.
//
// Without this, TestEveryImageIsBuiltFromTheWorkspaceThatWasTested could be
// satisfied by a coverage check that says yes to everything, and it would go on
// passing while the images went back to being unbuildable. That is not
// hypothetical: it is exactly the state the modulith was in.
func TestTheCoverageCheckNoticesAMissingModule(t *testing.T) {
	copied := []string{"go.work", "go.work.sum", "pkg/", "libs/", "services/"}

	for _, mod := range []string{"pkg", "libs/integrity", "services/milk-service"} {
		if !copiesModule(copied, mod) {
			t.Errorf("%q is inside a directory that is copied and was not recognised", mod)
		}
	}
	for _, mod := range []string{"e2e", "tools/amcu", "tools/dbadmin"} {
		if copiesModule(copied, mod) {
			t.Errorf("%q is not copied by any of %v and was accepted anyway, which is "+
				"the modulith's defect passing its own test", mod, copied)
		}
	}
	// A bare go.mod is enough to be in the graph, and is what the images use for
	// the three above.
	if !copiesModule([]string{"e2e/go.mod"}, "e2e") {
		t.Error("copying e2e/go.mod was not accepted as putting e2e in the module graph")
	}
	// But it is not enough for a module the image compiles, and nothing here can
	// tell the difference — which is why the build in CI is the other half of
	// this and not an optional extra.
}

// EXPOSE says what the image listens on, and libs/integrity/ports decides that.
//
// EXPOSE is documentation, and in one respect it is not: `docker run -P`
// publishes what the image declares. Three of these said 8103 — laboratory,
// material and settlement, all copied from procurement-service — and the
// modulith said 8080 while the binary defaults to the gateway's 8000. Run any of
// those four with -P and the daemon publishes a port nothing is listening on.
//
// Both deployments set SERVER_ADDR and are free to. This is about the image run
// with nothing set, which is the only thing EXPOSE can be about.
func TestEveryImageDeclaresThePortItListensOn(t *testing.T) {
	root := repoRoot(t)

	for _, df := range dockerfiles(t, root) {
		name := filepath.Base(filepath.Dir(df))
		body, err := os.ReadFile(df)
		if err != nil {
			t.Fatalf("read %s: %v", df, err)
		}

		// The modulith is the gateway with every other module behind it in one
		// process: it reads the gateway's configuration, so it defaults where the
		// gateway defaults.
		key := strings.TrimSuffix(name, "-service")
		if name == "modulith" {
			key = "gateway"
		}
		want, ok := ports.All[key]
		if !ok {
			t.Errorf("%s has an image and no entry in ports.All, so nothing says where "+
				"it listens when it is run outside a container", name)
			continue
		}

		got, found := exposedPort(string(body))
		if !found {
			t.Errorf("%s/Dockerfile declares no EXPOSE, so `docker run -P` publishes "+
				"nothing and a person reading the image cannot see where it answers", name)
			continue
		}
		if got != want {
			t.Errorf("%s/Dockerfile says EXPOSE %d and the binary listens on %d when "+
				"nothing overrides SERVER_ADDR (libs/integrity/ports). One of the two "+
				"is wrong and the image is the one people read.", name, got, want)
		}
	}
}

// Continuous integration builds the images, and builds all of them.
//
// The gate compiles every package and that is a different claim from "the image
// builds": an image also has to say which files it needs and where they sit, and
// the modulith's got that wrong from the day it was written to the day somebody
// built it. Nothing in this repository can build an image — there is no daemon
// on a developer's machine by assumption — so CI is the only place this is ever
// answered, which is why the workflow is checked here rather than trusted.
//
// The step iterates a glob. A list of thirty service names in YAML is a list to
// forget, and the thing forgotten is the image added last.
func TestContinuousIntegrationBuildsEveryImage(t *testing.T) {
	root := repoRoot(t)
	b, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "check.yml"))
	if err != nil {
		t.Fatalf("read the workflow: %v", err)
	}

	var wf struct {
		Jobs map[string]struct {
			Steps []struct {
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(b, &wf); err != nil {
		t.Fatalf("the workflow is not valid YAML, so it does not run at all: %v", err)
	}

	var buildsServices, buildsML bool
	for _, job := range wf.Jobs {
		for _, step := range job.Steps {
			if !strings.Contains(step.Run, "docker build") {
				continue
			}
			if strings.Contains(step.Run, "services/*/Dockerfile") {
				buildsServices = true
			}
			if strings.Contains(step.Run, "ml/Dockerfile") {
				buildsML = true
			}
		}
	}

	if !buildsServices {
		t.Error("no step runs `docker build` over services/*/Dockerfile. Nowhere else " +
			"builds an image: the gate compiles packages, which is not the same claim, " +
			"and the modulith's Dockerfile was broken from the day it was written " +
			"because nothing ever ran it.")
	}
	if !buildsML {
		t.Error("no step builds ml/Dockerfile. The four Rust services are deployed as " +
			"images like everything else and would be the only ones nothing builds.")
	}
}

// The context sent to the daemon excludes what no image copies.
//
// Every one of the thirty Go images builds from the repository root, and a
// context is sent in full before the first COPY is read. .git is half a gigabyte
// here — the gate clones it in full, because the schema upgrade test applies
// every committed version — and ml/target is another gigabyte of Rust output.
// Thirty times over, that is the difference between a build job and a build job
// nobody will keep.
func TestTheBuildContextLeavesOutWhatNoImageCopies(t *testing.T) {
	root := repoRoot(t)
	b, err := os.ReadFile(filepath.Join(root, ".dockerignore"))
	if err != nil {
		t.Fatalf("read .dockerignore: %v\nWithout one, every image build sends .git and "+
			"the Rust build directory to the daemon first.", err)
	}

	ignored := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ignored[strings.Trim(line, "/")] = true
	}
	for _, want := range []string{".git", "ml"} {
		if !ignored[want] {
			t.Errorf(".dockerignore does not exclude %q, which is sent to the daemon "+
				"before the first COPY of all thirty image builds", want)
		}
	}

	// And it must not exclude anything an image copies, which would turn a
	// context saving into a build that cannot find a package.
	for _, df := range dockerfiles(t, root) {
		body, err := os.ReadFile(df)
		if err != nil {
			t.Fatalf("read %s: %v", df, err)
		}
		for _, p := range copiedPaths(string(body)) {
			top := strings.Trim(strings.SplitN(strings.Trim(p, "/"), "/", 2)[0], "/")
			if ignored[top] {
				t.Errorf("%s copies %q and .dockerignore excludes %q, so the build "+
					"cannot see it", filepath.Base(filepath.Dir(df)), p, top)
			}
		}
	}
}

// dockerfiles is every image built from this repository's Go workspace.
func dockerfiles(t *testing.T, root string) []string {
	t.Helper()
	found, err := filepath.Glob(filepath.Join(root, "services", "*", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 {
		t.Fatal("no service Dockerfiles found; this test is looking in the wrong place")
	}
	sort.Strings(found)
	return found
}

// workspaceModules is every directory go.work lists, relative to the root.
func workspaceModules(t *testing.T, root string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, "go.work"))
	if err != nil {
		t.Fatalf("read go.work: %v", err)
	}

	var mods []string
	inBlock := false
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "use (":
			inBlock = true
		case inBlock && line == ")":
			inBlock = false
		case inBlock && line != "":
			mods = append(mods, strings.TrimPrefix(line, "./"))
		case strings.HasPrefix(line, "use "):
			mods = append(mods, strings.TrimPrefix(strings.TrimSpace(line[4:]), "./"))
		}
	}
	return mods
}

var copyLine = regexp.MustCompile(`(?mi)^COPY\s+(.*)$`)

// copiedPaths is every source path a Dockerfile's COPY instructions name.
//
// The last field of a COPY is the destination, and flags such as --from name a
// stage rather than a path; both are dropped. Good enough for the Dockerfiles
// here, and it fails loudly rather than quietly if one grows a shape it does not
// handle: an unrecognised source is simply not counted as covering anything.
func copiedPaths(dockerfile string) []string {
	var out []string
	for _, m := range copyLine.FindAllStringSubmatch(dockerfile, -1) {
		fields := strings.Fields(m[1])
		var srcs []string
		for _, f := range fields {
			if strings.HasPrefix(f, "--") {
				continue
			}
			srcs = append(srcs, f)
		}
		if len(srcs) < 2 {
			continue
		}
		out = append(out, srcs[:len(srcs)-1]...)
	}
	return out
}

func copiesFile(copied []string, want string) bool {
	for _, c := range copied {
		if strings.Trim(c, "./") == want {
			return true
		}
	}
	return false
}

// copiesModule says whether the copied paths put a module in the build's module
// graph: its whole directory, a directory containing it, or its bare go.mod.
func copiesModule(copied []string, module string) bool {
	module = strings.Trim(module, "/")
	for _, c := range copied {
		c = strings.TrimPrefix(c, "./")
		c = strings.Trim(c, "/")
		switch {
		case c == module, c == module+"/go.mod":
			return true
		case strings.HasPrefix(module+"/", c+"/"):
			return true
		}
	}
	return false
}

var exposeLine = regexp.MustCompile(`(?mi)^EXPOSE\s+(\d+)`)

func exposedPort(dockerfile string) (int, bool) {
	m := exposeLine.FindStringSubmatch(dockerfile)
	if m == nil {
		return 0, false
	}
	p, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return p, true
}
