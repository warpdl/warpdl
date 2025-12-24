//go:build windows

package warplib

import (
	"errors"
	"syscall"
)

// errNotSameDevice is the Windows error code for ERROR_NOT_SAME_DEVICE.
// This error (0x11 = 17 decimal) occurs when attempting to move a file
// between different drives (e.g., from C: to D:).
const errNotSameDevice syscall.Errno = 0x11

// isCrossDeviceError checks if the error is a cross-device error.
//
// On Windows, this error is ERROR_NOT_SAME_DEVICE (error code 17, 0x11),
// which occurs when attempting to rename/move a file between different
// drives (e.g., from C:\Downloads to D:\Videos).
//
// The function uses errors.As to unwrap nested errors, correctly handling
// errors wrapped by os.Rename (typically as *os.LinkError) or by
// fmt.Errorf with %w.
//
// Returns true if the underlying error is ERROR_NOT_SAME_DEVICE, false otherwise.
// Returns false for nil errors.
func isCrossDeviceError(err error) bool {
	if err == nil {
		return false
	}

	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == errNotSameDevice
	}

	return false
}
