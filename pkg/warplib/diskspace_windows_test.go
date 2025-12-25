//go:build windows

package warplib

import "testing"

func TestCheckDiskSpace(t *testing.T) {
	// Windows implementation currently returns nil (no-op stub)
	// This test verifies the stub behavior

	tests := []struct {
		name          string
		path          string
		requiredBytes int64
	}{
		{
			name:          "any path returns nil",
			path:          "C:\\",
			requiredBytes: 1024,
		},
		{
			name:          "large size returns nil",
			path:          "C:\\",
			requiredBytes: 1024 * 1024 * 1024 * 1024, // 1TB
		},
		{
			name:          "zero size returns nil",
			path:          "C:\\",
			requiredBytes: 0,
		},
		{
			name:          "negative size returns nil",
			path:          "C:\\",
			requiredBytes: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkDiskSpace(tt.path, tt.requiredBytes)
			if err != nil {
				t.Errorf("expected nil error from Windows stub, got: %v", err)
			}
		})
	}
}
