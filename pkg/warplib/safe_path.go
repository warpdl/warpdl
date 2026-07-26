package warplib

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	// ErrUnsafeFileName is returned when a caller-supplied filename could
	// escape the configured download directory or is not a single path
	// component.
	ErrUnsafeFileName = errors.New("unsafe download filename")

	// ErrDownloadSizeMismatch is returned when the completed output does not
	// contain exactly the number of bytes advertised by the server.
	ErrDownloadSizeMismatch = errors.New("download size mismatch")

	// ErrInvalidRangeResponse is returned when a server does not honour an
	// HTTP byte-range request exactly.
	ErrInvalidRangeResponse = errors.New("invalid HTTP range response")

	// ErrResourceChanged is returned when a ranged request no longer refers to
	// the strong ETag captured for the download. Continuing would combine bytes
	// from different representations into one output file.
	ErrResourceChanged = errors.New("remote resource changed during download")
)

// validateDownloadFileName accepts exactly one portable path component. URL-
// and Content-Disposition-derived names are sanitized before reaching this
// helper; explicit and plugin-provided names are rejected rather than silently
// rewritten so the caller never downloads to a path it did not request.
func validateDownloadFileName(name string) error {
	if name == "" {
		return ErrFileNameNotFound
	}
	if name == "." || name == ".." {
		return fmt.Errorf("%w: %q is not a file", ErrUnsafeFileName, name)
	}
	if strings.ContainsRune(name, '\x00') {
		return fmt.Errorf("%w: filename contains NUL", ErrUnsafeFileName)
	}
	if filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
		return fmt.Errorf("%w: absolute filename %q", ErrUnsafeFileName, name)
	}
	// filepath treats several Windows metacharacters as ordinary characters
	// on Unix. Reject the portable superset so a name accepted on one host
	// cannot become a path, wildcard, or alternate data stream on another.
	if strings.ContainsAny(name, `<>:"/\|?*`) {
		return fmt.Errorf("%w: filename contains a non-portable character: %q", ErrUnsafeFileName, name)
	}
	for _, r := range name {
		if r < 0x20 {
			return fmt.Errorf("%w: filename contains a control character", ErrUnsafeFileName)
		}
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return fmt.Errorf("%w: filename has a trailing dot or space: %q", ErrUnsafeFileName, name)
	}
	if isWindowsReservedFileName(name) {
		return fmt.Errorf("%w: reserved device filename %q", ErrUnsafeFileName, name)
	}
	if filepath.Base(filepath.Clean(name)) != name {
		return fmt.Errorf("%w: filename is not a single path component: %q", ErrUnsafeFileName, name)
	}
	return nil
}

func isWindowsReservedFileName(name string) bool {
	stem := name
	if dot := strings.IndexByte(stem, '.'); dot >= 0 {
		stem = stem[:dot]
	}
	stem = strings.ToUpper(stem)
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	return len(stem) == 4 &&
		(stem[:3] == "COM" || stem[:3] == "LPT") &&
		stem[3] >= '1' && stem[3] <= '9'
}

// confinedDownloadPath returns an absolute destination path and verifies it is
// lexically contained by directory. The single-component check is deliberate:
// downloads do not support caller-created subdirectories.
func confinedDownloadPath(directory, name string) (string, error) {
	if err := validateDownloadFileName(name); err != nil {
		return "", err
	}
	absDir, err := filepath.Abs(directory)
	if err != nil {
		return "", fmt.Errorf("resolve download directory: %w", err)
	}
	target := filepath.Join(absDir, name)
	rel, err := filepath.Rel(absDir, target)
	if err != nil {
		return "", fmt.Errorf("verify download path: %w", err)
	}
	if rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q escapes %q", ErrUnsafeFileName, name, absDir)
	}
	return target, nil
}

// openFreshDownloadFile creates a destination without following a pre-existing
// symlink. O_EXCL closes the check/create race for new files. In overwrite mode
// an existing regular file is removed first, then recreated exclusively; an
// attacker racing in a symlink makes the exclusive create fail rather than
// redirecting the write.
func openFreshDownloadFile(path string, overwrite bool) (*os.File, error) {
	info, err := WarpLstat(path)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: destination is a symbolic link: %s", ErrUnsafeFileName, path)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: destination is not a regular file: %s", ErrUnsafeFileName, path)
		}
		if !overwrite {
			return nil, fmt.Errorf("%w: %s", ErrFileExists, path)
		}
		if err := WarpRemove(path); err != nil {
			return nil, fmt.Errorf("remove existing destination: %w", err)
		}
	case !os.IsNotExist(err):
		return nil, fmt.Errorf("inspect destination: %w", err)
	}

	f, err := WarpOpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, DefaultFileMode)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrFileExists, path)
		}
		return nil, err
	}
	return f, nil
}

// openClaimedEmptyDownloadFile reopens a crash-left destination only when it
// is still the same regular, empty file. It never removes or truncates an
// existing path. Holding the verified file descriptor also prevents a later
// path swap from redirecting writes through a symlink; a non-empty or unsafe
// destination retains normal ErrFileExists/ErrUnsafeFileName behavior.
func openClaimedEmptyDownloadFile(path string) (*os.File, error) {
	f, err := openDownloadFileForResume(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("inspect claimed destination: %w", err)
	}
	if info.Size() != 0 {
		_ = f.Close()
		return nil, fmt.Errorf("%w: managed destination is no longer empty: %s", ErrFileExists, path)
	}
	return f, nil
}

// openDownloadFileForResume opens only a regular, non-symlink destination.
// When no file exists it creates one exclusively.
func openDownloadFileForResume(path string) (*os.File, error) {
	info, err := WarpLstat(path)
	switch {
	case err == nil:
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: destination is a symbolic link: %s", ErrUnsafeFileName, path)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: destination is not a regular file: %s", ErrUnsafeFileName, path)
		}
		f, openErr := WarpOpenFile(path, os.O_RDWR, DefaultFileMode)
		if openErr != nil {
			return nil, openErr
		}
		openedInfo, statErr := f.Stat()
		if statErr != nil {
			f.Close()
			return nil, fmt.Errorf("inspect opened destination: %w", statErr)
		}
		currentInfo, lstatErr := WarpLstat(path)
		if lstatErr != nil ||
			currentInfo.Mode()&os.ModeSymlink != 0 ||
			!os.SameFile(openedInfo, currentInfo) ||
			!os.SameFile(info, openedInfo) {
			f.Close()
			if lstatErr != nil {
				return nil, fmt.Errorf("%w: destination changed while opening: %v", ErrUnsafeFileName, lstatErr)
			}
			return nil, fmt.Errorf("%w: destination changed while opening: %s", ErrUnsafeFileName, path)
		}
		return f, nil
	case os.IsNotExist(err):
		f, openErr := WarpOpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, DefaultFileMode)
		if openErr != nil && os.IsExist(openErr) {
			return nil, fmt.Errorf("%w: destination appeared during resume: %s", ErrFileExists, path)
		}
		return f, openErr
	default:
		return nil, fmt.Errorf("inspect destination: %w", err)
	}
}
