//go:build !windows

package warplib

import (
	"fmt"
	"syscall"
)

// checkDiskSpace checks if there's enough disk space available at the given path
// to accommodate a file of the specified size.
// Returns an error if insufficient space is available.
func checkDiskSpace(path string, requiredBytes int64) error {
	if requiredBytes <= 0 {
		// Unknown size or zero size, skip check
		return nil
	}

	var stat syscall.Statfs_t
	err := syscall.Statfs(path, &stat)
	if err != nil {
		// If we can't check disk space, gracefully ignore and don't fail
		// (better to try and potentially fail later than block downloads)
		return nil
	}

	// Calculate available space
	// Bavail is available blocks for unprivileged users
	availableBytes := int64(stat.Bavail) * int64(stat.Bsize)

	if availableBytes < requiredBytes {
		return fmt.Errorf("%w: required space %s, available space %s",
			ErrInsufficientDiskSpace,
			ContentLength(requiredBytes),
			ContentLength(availableBytes))
	}

	return nil
}
