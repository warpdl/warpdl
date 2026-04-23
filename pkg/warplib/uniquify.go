package warplib

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// maxUniquifyAttempts caps how many " (N)" candidates we'll probe
// before giving up. Bounds runtime in pathological cases (e.g. a
// directory hand-crammed with `name (1).ext` … `name (1000).ext`)
// and prevents an infinite loop if Stat keeps reporting "exists" due
// to a permission error.
const maxUniquifyAttempts = 4096

// uniquifyPath returns a non-colliding sibling path under the same
// directory as path. If path doesn't exist, returns it unchanged.
// Otherwise inserts " (N)" before the file extension, picking the
// smallest N >= 1 that doesn't already exist on disk — same convention
// browsers use for the Save-As dialog.
//
// Examples:
//   /tmp/report.pdf       (exists) -> /tmp/report (1).pdf
//   /tmp/report (1).pdf   (exists) -> /tmp/report (2).pdf
//   /tmp/Makefile         (exists) -> /tmp/Makefile (1)
//   /tmp/archive.tar.gz   (exists) -> /tmp/archive.tar (1).gz
//
// The last extension is treated as the extension (Chrome/Firefox-ish);
// this matches applyTimestampSuffix so naming is consistent.
//
// Returns an error if either Stat fails for a non-ENOENT reason or
// every candidate up to maxUniquifyAttempts already exists.
func uniquifyPath(path string) (string, error) {
	exists, err := pathExists(path)
	if err != nil {
		return "", fmt.Errorf("uniquify: stat %s: %w", path, err)
	}
	if !exists {
		return path, nil
	}
	dir := filepath.Dir(path)
	leaf := filepath.Base(path)
	ext := filepath.Ext(leaf)
	base := strings.TrimSuffix(leaf, ext)
	for n := 1; n < maxUniquifyAttempts; n++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, n, ext))
		exists, err := pathExists(candidate)
		if err != nil {
			return "", fmt.Errorf("uniquify: stat %s: %w", candidate, err)
		}
		if !exists {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("uniquify: too many collisions for %s", path)
}

// pathExists is os.Stat that distinguishes "doesn't exist" from a real
// error. Returns (false, nil) for ENOENT, (true, nil) for present,
// (false, err) for anything else (permissions, I/O, etc.).
func pathExists(path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
