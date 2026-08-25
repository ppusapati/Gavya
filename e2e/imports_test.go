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

// knownAbsent are packages the shared module refers to that were never brought
// across when it was imported. They are not a case problem and cannot be fixed
// by renaming anything: the code has to be written or generated. Until then the
// packages that import them do not build, and neither does the module as a
// whole under a plain `go build ./...`.
//
// This is a register, not an exemption. The test fails if one of these turns up
// on disk — so the entry has to be deleted when the package is supplied — and it
// fails if anything not listed here goes missing.
var knownAbsent = map[string]string{
	sharedModule + "/classregistry/api/v1": "generated protobuf for the class registry; " +
		"breaks pkg/classregistry",
	sharedModule + "/classregistry/api/v1/classregistryv1connect": "generated Connect bindings for the " +
		"same; breaks pkg/classregistry",
	sharedModule + "/classregistry/pgstore": "the Postgres-backed class registry store; " +
		"breaks pkg/onboarding",
	sharedModule + "/convert/sql": "SQL scan and value helpers; breaks pkg/convert",
	sharedModule + "/saga/models": "saga step and state types; breaks pkg/saga",
}

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
	root := repoRoot(t)
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
	stillAbsent := map[string]bool{}
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
			if _, ok := knownAbsent[p]; ok {
				stillAbsent[p] = true
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

	// A register that is never pruned stops describing anything. If one of these
	// packages has been supplied, say so here rather than letting the entry sit
	// and excuse a future absence.
	for p, why := range knownAbsent {
		if real[p] {
			t.Errorf("%q now exists, so delete its entry from knownAbsent (%s)", p, why)
			continue
		}
		if !stillAbsent[p] {
			t.Errorf("%q is listed as a missing package that breaks the build, but nothing imports it "+
				"any more — delete its entry from knownAbsent (%s)", p, why)
		}
	}

	t.Logf("checked %d imports of %s against %d packages on disk; %d known-absent packages still "+
		"break the module build", checked, sharedModule, len(real), len(stillAbsent))
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
			t.Fatal("no go.work found above the test's working directory")
		}
		dir = parent
	}
}
