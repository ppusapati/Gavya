package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Finding the bytes behind a file record.
//
// This service never sees a file. It records where something else put one, and
// `stored_name` is the caller's word for that — untrusted input that this code
// is about to turn into a path it opens and streams to a browser. Getting it
// wrong is not a subtle bug: it is a download link that serves /etc/passwd, or
// the private key beside the binary, to anybody who can ask for one file.
//
// So two separate defences, because either alone has a way round it.
//
// The name is required to be a plain base name: no separator, no `..`, nothing
// beginning with a dot. That refuses the obvious attack and most of the
// accidental ones at the point the record is read.
//
// And the resolved path is required to still be inside the bucket after
// symbolic links have been followed. That is the one the first defence misses:
// a plain name is a plain name, and if something wrote a symlink called
// `invoice.pdf` inside the bucket pointing at /etc/shadow, every check on the
// name passes and the file served is not in the bucket at all.

// ErrNoStore says this deployment has nowhere to read files from.
var ErrNoStore = errors.New("no file store is configured")

// ErrNotOnDisk says the record names an object that is not there.
//
// Kept apart from "no such record", because they mean different things to
// whoever is looking: one is a file nobody registered, the other is a file
// somebody registered and something else never wrote, or has since deleted.
var ErrNotOnDisk = errors.New("the file named by this record is not in the store")

// resolve turns a stored name into a path inside the bucket, or refuses.
//
// The returned path is safe to open. Nothing else in this service opens a path
// that did not come from here.
func resolve(bucket, storedName string) (string, error) {
	if strings.TrimSpace(bucket) == "" {
		return "", ErrNoStore
	}

	name := strings.TrimSpace(storedName)
	switch {
	case name == "":
		return "", errors.New("this record has no stored name, so there is nothing to open")
	case name != filepath.Base(name):
		// Covers every separator on the platform, and `a/../b` along with it.
		return "", fmt.Errorf("the stored name %q is a path rather than a name; this service "+
			"opens files in its own store and nowhere else", storedName)
	case name == "." || name == "..":
		return "", fmt.Errorf("the stored name %q names a directory", storedName)
	case strings.HasPrefix(name, "."):
		// A leading dot is not dangerous on its own, and refusing it keeps the
		// store to files somebody meant to put there rather than to whatever a
		// process dropped beside them.
		return "", fmt.Errorf("the stored name %q begins with a dot", storedName)
	case strings.ContainsRune(name, 0):
		// A NUL truncates the path in anything that eventually reaches C, so
		// what Go checked and what the kernel opened would be different
		// strings.
		return "", errors.New("the stored name holds a null byte")
	}

	root, err := filepath.Abs(filepath.Clean(bucket))
	if err != nil {
		return "", fmt.Errorf("the store %q is not a path this service can resolve: %w", bucket, err)
	}
	candidate := filepath.Join(root, name)

	// And now the part the name check cannot do. A symlink inside the store is
	// a plain name by every test above and an arbitrary file by the time it is
	// opened.
	//
	// EvalSymlinks also tells us whether the file is there at all, which is
	// the other thing worth knowing before a link is handed out.
	real, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotOnDisk
		}
		return "", fmt.Errorf("the file behind this record could not be reached: %w", err)
	}

	// The store itself is resolved the same way before they are compared. A
	// bucket that is itself reached through a symlink — /data being a link to
	// /mnt/data is an ordinary arrangement — would otherwise fail every
	// comparison and this service would serve nothing at all.
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("the store %q could not be resolved: %w", bucket, err)
	}

	if !within(realRoot, real) {
		return "", fmt.Errorf("the file behind this record resolves outside the store; "+
			"%q is not in %q", real, realRoot)
	}

	info, err := os.Stat(real)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotOnDisk
		}
		return "", err
	}
	if !info.Mode().IsRegular() {
		// A directory, a device, a socket or a fifo. Opening any of them and
		// streaming the result is at best nonsense and at worst a handler that
		// blocks for ever on a pipe nobody writes to.
		return "", fmt.Errorf("what this record names is not a file")
	}
	return real, nil
}

// within reports whether path is root or is inside it.
//
// Compared component-wise rather than with a string prefix, because `/data` is
// a prefix of `/database` and a prefix test would call the second one inside
// the first.
func within(root, path string) bool {
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
