package service

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A store with one ordinary file in it, and the traps beside it.
func store(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "invoice.pdf"), []byte("%PDF-1.4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	// A real file one level down, so the rule that refuses a path can be told
	// apart from the file simply not being there.
	if err := os.WriteFile(filepath.Join(root, "nested", "deep.pdf"),
		[]byte("%PDF-1.4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestAnOrdinaryNameResolves(t *testing.T) {
	root := store(t)
	got, err := resolve(root, "invoice.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "invoice.pdf" {
		t.Errorf("resolved to %q", got)
	}
	if !within(mustEval(t, root), got) {
		t.Errorf("%q is outside %q", got, root)
	}
}

// Nothing that is not a plain name in the store may be opened.
//
// stored_name is the caller's word for where something else put a file. This
// code turns it into a path it opens and streams to a browser, so each of these
// accepted is a download link that serves a file nobody registered.
func TestAPathIsNeverOpened(t *testing.T) {
	root := store(t)

	// Something to steal, outside the store.
	outside := filepath.Join(filepath.Dir(root), "secret.txt")
	if err := os.WriteFile(outside, []byte("a private key"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{
		"../secret.txt",
		"../../secret.txt",
		"nested/../../secret.txt",
		"/etc/passwd",
		"/",
		"..",
		".",
		"nested/deep.pdf",
		"./invoice.pdf",
		"",
		"   ",
		".hidden",
		"invoice.pdf\x00.txt",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := resolve(root, name)
			if err == nil {
				t.Fatalf("%q resolved to %q; only a plain name in the store may be opened",
					name, got)
			}
		})
	}

	if runtime.GOOS == "windows" {
		for _, name := range []string{`..\secret.txt`, `C:\Windows\win.ini`} {
			if _, err := resolve(root, name); err == nil {
				t.Errorf("%q resolved", name)
			}
		}
	}
}

// A path inside the store is still refused, and that is a rule rather than a
// boundary.
//
// Containment is what makes this safe: `nested/deep.pdf` is inside the store,
// so nothing is escaping. Refusing it is a decision about what `stored_name`
// means — one name, one file, in one directory — and it is worth pinning
// separately because it is the cheaper of the two checks and the one that
// still refuses the obvious payloads if the other ever has a bug in it.
//
// Written against a file that exists, because a test against a name with
// nothing behind it passes whether the rule is there or not.
func TestAPathInsideTheStoreIsRefusedByTheNameRule(t *testing.T) {
	root := store(t)

	// The file is really there: opened directly, it reads.
	if _, err := os.ReadFile(filepath.Join(root, "nested", "deep.pdf")); err != nil {
		t.Fatalf("the test's own fixture is missing: %v", err)
	}

	got, err := resolve(root, "nested/deep.pdf")
	if err == nil {
		t.Fatalf("a path inside the store resolved to %q; a stored name is one file in the "+
			"store, not a tree to walk", got)
	}
	if errors.Is(err, ErrNotOnDisk) {
		t.Errorf("it was refused for not being there, which it is; the name rule is what "+
			"should have refused it: %v", err)
	}
}

// A symlink inside the store is a plain name and an arbitrary file.
//
// This is the case the name check cannot catch: `invoice.pdf` is a plain name
// by every test there is, and if something wrote it as a link to /etc/shadow
// then what gets served is not in the store at all.
func TestASymlinkOutOfTheStoreIsRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege this test will not ask for on Windows")
	}
	root := store(t)

	outside := filepath.Join(filepath.Dir(root), "shadow")
	if err := os.WriteFile(outside, []byte("root:!:19000:0:99999:7:::"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "harmless.pdf")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("this filesystem will not make a symlink: %v", err)
	}

	got, err := resolve(root, "harmless.pdf")
	if err == nil {
		t.Fatalf("a symlink out of the store resolved to %q, so a download link for a file "+
			"somebody registered serves a file nobody did", got)
	}
	if !strings.Contains(err.Error(), "outside the store") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}

// A symlink that stays inside the store is fine.
//
// The check is containment, not a ban on links: refusing every link would break
// an ordinary store where a deduplicating writer points two names at one file.
func TestASymlinkInsideTheStoreIsServed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege this test will not ask for on Windows")
	}
	root := store(t)
	if err := os.Symlink(filepath.Join(root, "invoice.pdf"),
		filepath.Join(root, "same.pdf")); err != nil {
		t.Skipf("this filesystem will not make a symlink: %v", err)
	}

	got, err := resolve(root, "same.pdf")
	if err != nil {
		t.Fatalf("a link inside the store was refused: %v", err)
	}
	if filepath.Base(got) != "invoice.pdf" {
		t.Errorf("resolved to %q", got)
	}
}

// A store reached through a symlink still works.
//
// /data being a link to /mnt/data is an ordinary arrangement. Comparing the
// resolved file against an unresolved root would fail every comparison, and
// this service would serve nothing at all while every name check passed.
func TestAStoreBehindASymlinkStillServes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege this test will not ask for on Windows")
	}
	real := store(t)
	parent := t.TempDir()
	linked := filepath.Join(parent, "data")
	if err := os.Symlink(real, linked); err != nil {
		t.Skipf("this filesystem will not make a symlink: %v", err)
	}

	if _, err := resolve(linked, "invoice.pdf"); err != nil {
		t.Errorf("a store reached through a symlink served nothing: %v", err)
	}
}

// A name that is a prefix of the store's is not inside it.
//
// /data is a string prefix of /database. Compared as strings, a file in the
// second would be reported as being in the first.
func TestContainmentIsNotAStringPrefix(t *testing.T) {
	if within("/data", "/database/secret") {
		t.Error("/database/secret was reported as being inside /data")
	}
	if !within("/data", "/data/invoice.pdf") {
		t.Error("/data/invoice.pdf was reported as being outside /data")
	}
	if !within("/data", "/data") {
		t.Error("a store was reported as being outside itself")
	}
	if within("/data/a", "/data") {
		t.Error("a parent was reported as being inside its child")
	}
}

// A record naming something that is not there is refused as that, specifically.
//
// Handing out a link to a file the store does not hold is a link that 404s
// after somebody has emailed it. "No such record" and "the object is gone" are
// different answers and only one of them means the record should be tidied up.
func TestAMissingObjectIsRefusedAsMissing(t *testing.T) {
	root := store(t)
	_, err := resolve(root, "never-written.pdf")
	if !errors.Is(err, ErrNotOnDisk) {
		t.Errorf("a name with no file behind it gave %v, want ErrNotOnDisk", err)
	}
}

// What is not a file is not served.
func TestOnlyARegularFileIsServed(t *testing.T) {
	root := store(t)
	if _, err := resolve(root, "nested"); err == nil {
		t.Error("a directory resolved; opening one and streaming the result is nonsense")
	}
}

func TestNoStoreIsRefusedAsThat(t *testing.T) {
	if _, err := resolve("", "invoice.pdf"); !errors.Is(err, ErrNoStore) {
		t.Errorf("an unset store gave %v, want ErrNoStore", err)
	}
	if _, err := resolve("   ", "invoice.pdf"); !errors.Is(err, ErrNoStore) {
		t.Errorf("a blank store gave %v", err)
	}
}

func mustEval(t *testing.T, path string) string {
	t.Helper()
	got, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
