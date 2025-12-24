//go:build !windows

package warplib

import (
	"errors"
	"syscall"
)

// isCrossDeviceError checks if the error is a cross-device link error.
//
// On Unix systems, this error is EXDEV (errno 18), which occurs when
// attempting to rename a file across different filesystems or mount points.
//
// The function uses errors.As to unwrap nested errors, correctly handling
// errors wrapped by os.Rename (typically as *os.LinkError) or by
// fmt.Errorf with %w.
//
// Returns true if the underlying error is syscall.EXDEV, false otherwise.
// Returns false for nil errors.
func isCrossDeviceError(err error) bool {
	if err == nil {
		return false
	}

	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.EXDEV
	}

	return false
}
