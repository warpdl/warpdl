//go:build !windows

package warplib

import (
	"fmt"
	"os"
	"syscall"
	"testing"
)

func TestIsCrossDeviceError_Unix(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "syscall.EXDEV returns true",
			err:      syscall.EXDEV,
			expected: true,
		},
		{
			name:     "syscall.ENOENT returns false",
			err:      syscall.ENOENT,
			expected: false,
		},
		{
			name:     "syscall.EINVAL returns false",
			err:      syscall.EINVAL,
			expected: false,
		},
		{
			name:     "syscall.EACCES returns false",
			err:      syscall.EACCES,
			expected: false,
		},
		{
			name:     "syscall.EPERM returns false",
			err:      syscall.EPERM,
			expected: false,
		},
		{
			name:     "syscall.ENOSPC returns false",
			err:      syscall.ENOSPC,
			expected: false,
		},
		{
			name:     "nil error returns false",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCrossDeviceError(tt.err)
			if got != tt.expected {
				t.Errorf("isCrossDeviceError(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}

func TestIsCrossDeviceError_Unix_WrappedError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name: "os.LinkError wrapping syscall.EXDEV",
			err: &os.LinkError{
				Op:  "rename",
				Old: "/src",
				New: "/dst",
				Err: syscall.EXDEV,
			},
			expected: true,
		},
		{
			name: "os.LinkError wrapping syscall.ENOENT",
			err: &os.LinkError{
				Op:  "rename",
				Old: "/src",
				New: "/dst",
				Err: syscall.ENOENT,
			},
			expected: false,
		},
		{
			name:     "fmt.Errorf wrapping syscall.EXDEV",
			err:      fmt.Errorf("rename failed: %w", syscall.EXDEV),
			expected: true,
		},
		{
			name: "deeply wrapped EXDEV",
			err: fmt.Errorf("outer: %w",
				fmt.Errorf("middle: %w",
					fmt.Errorf("inner: %w", syscall.EXDEV))),
			expected: true,
		},
		{
			name: "os.PathError wrapping syscall.EXDEV",
			err: &os.PathError{
				Op:   "rename",
				Path: "/some/path",
				Err:  syscall.EXDEV,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCrossDeviceError(tt.err)
			if got != tt.expected {
				t.Errorf("isCrossDeviceError(%v) = %v, want %v", tt.err, got, tt.expected)
			}
		})
	}
}
