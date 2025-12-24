//go:build !windows

package warplib

import "syscall"

// isRetryableErrno checks if a syscall.Errno represents a retryable error.
// On Unix systems, only POSIX-style error constants are checked.
func isRetryableErrno(errno syscall.Errno) bool {
	switch errno {
	case syscall.ECONNRESET, syscall.ECONNREFUSED, syscall.ECONNABORTED,
		syscall.ETIMEDOUT, syscall.ENETUNREACH, syscall.EHOSTUNREACH,
		syscall.EPIPE:
		return true
	}
	return false
}
