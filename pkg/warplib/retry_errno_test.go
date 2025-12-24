package warplib

import (
	"syscall"
	"testing"
)

func TestIsRetryableErrno(t *testing.T) {
	tests := []struct {
		name     string
		errno    syscall.Errno
		expected bool
	}{
		{"ECONNRESET", syscall.ECONNRESET, true},
		{"ECONNREFUSED", syscall.ECONNREFUSED, true},
		{"ECONNABORTED", syscall.ECONNABORTED, true},
		{"ETIMEDOUT", syscall.ETIMEDOUT, true},
		{"ENETUNREACH", syscall.ENETUNREACH, true},
		{"EHOSTUNREACH", syscall.EHOSTUNREACH, true},
		{"EPIPE", syscall.EPIPE, true},
		{"ENOENT", syscall.ENOENT, false},
		{"EINVAL", syscall.EINVAL, false},
		{"EACCES", syscall.EACCES, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRetryableErrno(tt.errno)
			if got != tt.expected {
				t.Errorf("isRetryableErrno(%v) = %v, want %v", tt.name, got, tt.expected)
			}
		})
	}
}
