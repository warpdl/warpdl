//go:build !windows

package warplib

import "os"

// WarpOpen opens a file (pass-through on non-Windows)
func WarpOpen(path string) (*os.File, error) {
	return os.Open(path)
}

// WarpCreate creates a file (pass-through on non-Windows)
func WarpCreate(path string) (*os.File, error) {
	return os.Create(path)
}

// WarpOpenFile opens a file with flags and permissions (pass-through on non-Windows)
func WarpOpenFile(path string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(path, flag, perm)
}

// WarpMkdirAll creates a directory path (pass-through on non-Windows)
func WarpMkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// WarpMkdir creates a single directory (pass-through on non-Windows)
func WarpMkdir(path string, perm os.FileMode) error {
	return os.Mkdir(path, perm)
}

// WarpRemove removes a file or directory (pass-through on non-Windows)
func WarpRemove(path string) error {
	return os.Remove(path)
}

// WarpRemoveAll removes a path and any children (pass-through on non-Windows)
func WarpRemoveAll(path string) error {
	return os.RemoveAll(path)
}

// WarpStat returns file info (pass-through on non-Windows)
func WarpStat(path string) (os.FileInfo, error) {
	return os.Stat(path)
}

// WarpRename renames a file or directory (pass-through on non-Windows)
func WarpRename(src, dst string) error {
	return os.Rename(src, dst)
}
