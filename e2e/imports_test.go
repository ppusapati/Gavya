package e2e

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The shared packages module is named after the project it was imported from,
// and its packages are addressed by that path.
const sharedModule = "p9e.in/samavaya/packages"

// There used to be a register here of packages the shared module referred to
// that were never brought across when it was imported — generated protobuf, a
// saga models package, SQL helpers — each of which broke the package importing
// it, so the module as a whole never built. The module has since been cut down
// to the two packages this platform imports, and nothing in it refers to
// anything missing. Every import of the shared module now has to resolve; there
// is no list of excuses.

// TestSharedImportsResolveOnACaseSensitiveFilesystem guards a defect that is
// invisible on the machine it is written on.
//
// The ULID package lives in a directory that was capitalised, and was imported
// as ".../ulid" by the shared module's own files and as ".../ULID" by every
// service. On macOS, whose filesystem is case-insensitive by default, both
// resolve and everything builds. On Linux — where this deploys, and where the
// container images are built — exactly one of the two was always broken, and
// which one depended on which package you asked for. The whole shared module
// failed to load, which took the auth and row-level-security packages with it.
//
// A build on Linux catches this, but only once something imports the broken
// package. This says which import is wrong and why, before that.
func TestSharedImportsResolveOnACaseSensitiveFilesystem(t *testing.T) {
	root := workspaceRoot(t)
	pkgDir := filepath.Join(root, "pkg")
	if _, err := os.Stat(pkgDir); err != nil {
		t.Skipf("no pkg directory at %s", pkgDir)
	}

	// Every directory under pkg/ that holds Go source, by the import path it
	// answers to — spelled exactly as it is on disk.
	real := map[string]bool{}
	err := filepath.WalkDir(pkgDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, rerr := filepath.Rel(pkgDir, filepath.Dir(path))
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			real[sharedModule] = true
			return nil
		}
		real[sharedModule+"/"+filepath.ToSlash(rel)] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(real) == 0 {
		t.Fatal("no Go packages found under pkg/, so this test would pass vacuously")
	}

	// Fold case once, so a wrong-case import can be reported as the near miss it
	// is rather than as a package that does not exist.
	byLower := map[string]string{}
	for p := range real {
		byLower[strings.ToLower(p)] = p
	}

	var checked int
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "build", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, perr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if perr != nil {
			return nil // not this test's business
		}
		for _, imp := range f.Imports {
			p, uerr := strconv.Unquote(imp.Path.Value)
			if uerr != nil || !strings.HasPrefix(p, sharedModule) {
				continue
			}
			checked++
			if real[p] {
				continue
			}
			rel, _ := filepath.Rel(root, path)
			if actual, ok := byLower[strings.ToLower(p)]; ok {
				t.Errorf("%s imports %q, but the directory is %q — these differ only in case, "+
					"so this builds on macOS and fails on Linux", rel, p, actual)
				continue
			}
			t.Errorf("%s imports %q, which does not exist under pkg/", rel, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Fatal("no imports of the shared module were found, so this test would pass vacuously")
	}

	t.Logf("checked %d imports of %s against %d packages on disk", checked, sharedModule, len(real))
}

func workspaceRoot(t *testing.T) string {
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
			t.Fatal("no go.work found above the test's working directory")
		}
		dir = parent
	}
}
