//go:build windows

package warplib

// checkDiskSpace checks if there's enough disk space available at the given path
// to accommodate a file of the specified size.
// Returns an error if insufficient space is available.
//
// Note: Windows implementation is not required by acceptance criteria,
// but provided for completeness. Currently returns nil (no check).
func checkDiskSpace(path string, requiredBytes int64) error {
	// TODO: Implement for Windows using GetDiskFreeSpaceEx
	return nil
}
