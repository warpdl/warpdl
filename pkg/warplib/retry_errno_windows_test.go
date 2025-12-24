//go:build windows

package warplib

import (
	"syscall"
	"testing"
)

func TestIsRetryableErrnoWindows(t *testing.T) {
	tests := []struct {
		name     string
		errno    syscall.Errno
		expected bool
	}{
		{"WSAECONNRESET", syscall.Errno(10054), true},
		{"WSAECONNABORTED", syscall.Errno(10053), true},
		{"WSAECONNREFUSED", syscall.Errno(10061), true},
		{"WSAETIMEDOUT", syscall.Errno(10060), true},
		{"WSAENETUNREACH", syscall.Errno(10051), true},
		{"WSAEHOSTUNREACH", syscall.Errno(10065), true},
		{"WSAENETDOWN", syscall.Errno(10050), true},
		{"WSAENETRESET", syscall.Errno(10052), true},
		{"WSAENOBUFS", syscall.Errno(10055), true},
		{"WSAEHOSTDOWN", syscall.Errno(10064), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRetryableErrno(tt.errno)
			if got != tt.expected {
				t.Errorf("isRetryableErrno(%v [%d]) = %v, want %v", tt.name, tt.errno, got, tt.expected)
			}
		})
	}
}

func TestClassifyErrorWindowsSocketErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected ErrorCategory
	}{
		{"WSAECONNRESET", syscall.Errno(10054), ErrCategoryRetryable},
		{"WSAECONNABORTED", syscall.Errno(10053), ErrCategoryRetryable},
		{"WSAECONNREFUSED", syscall.Errno(10061), ErrCategoryRetryable},
		{"WSAETIMEDOUT", syscall.Errno(10060), ErrCategoryRetryable},
		{"WSAENETDOWN", syscall.Errno(10050), ErrCategoryRetryable},
		{"WSAENETRESET", syscall.Errno(10052), ErrCategoryRetryable},
		{"WSAENOBUFS", syscall.Errno(10055), ErrCategoryRetryable},
		{"WSAEHOSTDOWN", syscall.Errno(10064), ErrCategoryRetryable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyError(tt.err)
			if got != tt.expected {
				t.Errorf("ClassifyError(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}
