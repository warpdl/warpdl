//go:build windows

package warplib

import "os"

// WarpOpen opens a file, normalizing the path for long path support
func WarpOpen(path string) (*os.File, error) {
	return os.Open(NormalizePath(path))
}

// WarpCreate creates a file with secure default permissions (0644).
// This replaces os.Create which uses 0666 by default.
func WarpCreate(path string) (*os.File, error) {
	return os.OpenFile(NormalizePath(path), os.O_RDWR|os.O_CREATE|os.O_TRUNC, DefaultFileMode)
}

// WarpOpenFile opens a file with flags and permissions, normalizing the path for long path support
func WarpOpenFile(path string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(NormalizePath(path), flag, perm)
}

// WarpMkdirAll creates a directory path, normalizing the path for long path support
func WarpMkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(NormalizePath(path), perm)
}

// WarpMkdir creates a single directory, normalizing the path for long path support
func WarpMkdir(path string, perm os.FileMode) error {
	return os.Mkdir(NormalizePath(path), perm)
}

// WarpRemove removes a file or directory, normalizing the path for long path support
func WarpRemove(path string) error {
	return os.Remove(NormalizePath(path))
}

// WarpRemoveAll removes a path and any children, normalizing the path for long path support
func WarpRemoveAll(path string) error {
	return os.RemoveAll(NormalizePath(path))
}

// WarpStat returns file info, normalizing the path for long path support
func WarpStat(path string) (os.FileInfo, error) {
	return os.Stat(NormalizePath(path))
}

// WarpRename renames a file or directory, normalizing both paths for long path support
func WarpRename(src, dst string) error {
	return os.Rename(NormalizePath(src), NormalizePath(dst))
}

// WarpChmod changes file permissions, normalizing the path for long path support
func WarpChmod(path string, perm os.FileMode) error {
	return os.Chmod(NormalizePath(path), perm)
}
