//go:build windows

package warplib

import (
	"fmt"
	"os"
	"syscall"
	"testing"
)

func TestIsCrossDeviceError_Windows(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "ERROR_NOT_SAME_DEVICE (0x11) returns true",
			err:      syscall.Errno(0x11),
			expected: true,
		},
		{
			name:     "ERROR_FILE_NOT_FOUND (0x2) returns false",
			err:      syscall.Errno(0x2),
			expected: false,
		},
		{
			name:     "ERROR_ACCESS_DENIED (0x5) returns false",
			err:      syscall.Errno(0x5),
			expected: false,
		},
		{
			name:     "ERROR_INVALID_PARAMETER (0x57) returns false",
			err:      syscall.Errno(0x57),
			expected: false,
		},
		{
			name:     "ERROR_DISK_FULL (0x70) returns false",
			err:      syscall.Errno(0x70),
			expected: false,
		},
		{
			name:     "arbitrary errno returns false",
			err:      syscall.Errno(0xFF),
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

func TestIsCrossDeviceError_Windows_WrappedLinkError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name: "os.LinkError wrapping ERROR_NOT_SAME_DEVICE",
			err: &os.LinkError{
				Op:  "rename",
				Old: "C:\\src",
				New: "D:\\dst",
				Err: syscall.Errno(0x11),
			},
			expected: true,
		},
		{
			name: "os.LinkError wrapping ERROR_FILE_NOT_FOUND",
			err: &os.LinkError{
				Op:  "rename",
				Old: "C:\\src",
				New: "D:\\dst",
				Err: syscall.Errno(0x2),
			},
			expected: false,
		},
		{
			name:     "fmt.Errorf wrapping ERROR_NOT_SAME_DEVICE",
			err:      fmt.Errorf("rename failed: %w", syscall.Errno(0x11)),
			expected: true,
		},
		{
			name: "deeply wrapped ERROR_NOT_SAME_DEVICE",
			err: fmt.Errorf("outer: %w",
				fmt.Errorf("middle: %w",
					fmt.Errorf("inner: %w", syscall.Errno(0x11)))),
			expected: true,
		},
		{
			name: "os.PathError wrapping ERROR_NOT_SAME_DEVICE",
			err: &os.PathError{
				Op:   "rename",
				Path: "C:\\some\\path",
				Err:  syscall.Errno(0x11),
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

func TestIsCrossDeviceError_Windows_NilError(t *testing.T) {
	got := isCrossDeviceError(nil)
	if got != false {
		t.Errorf("isCrossDeviceError(nil) = %v, want false", got)
	}
}
