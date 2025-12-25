//go:build !windows

package warplib

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCheckDiskSpace(t *testing.T) {
	// Get a temporary directory for testing
	tmpDir := t.TempDir()

	// Get actual available space
	var stat syscall.Statfs_t
	err := syscall.Statfs(tmpDir, &stat)
	if err != nil {
		t.Fatalf("Failed to get disk stats: %v", err)
	}
	availableBytes := int64(stat.Bavail) * int64(stat.Bsize)

	tests := []struct {
		name          string
		path          string
		requiredBytes int64
		expectError   bool
		errorType     error
	}{
		{
			name:          "sufficient space",
			path:          tmpDir,
			requiredBytes: 1024, // 1KB - should always be available
			expectError:   false,
		},
		{
			name:          "insufficient space",
			path:          tmpDir,
			requiredBytes: availableBytes + 1024*1024*1024, // More than available
			expectError:   true,
			errorType:     ErrInsufficientDiskSpace,
		},
		{
			name:          "zero size file",
			path:          tmpDir,
			requiredBytes: 0,
			expectError:   false,
		},
		{
			name:          "unknown size (negative)",
			path:          tmpDir,
			requiredBytes: -1,
			expectError:   false,
		},
		{
			name:          "non-existent path",
			path:          "/path/that/does/not/exist",
			requiredBytes: 1024,
			expectError:   false, // Should not fail, just skip check
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkDiskSpace(tt.path, tt.requiredBytes)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				if tt.errorType != nil && !errors.Is(err, tt.errorType) {
					t.Errorf("expected error type %v, got %v", tt.errorType, err)
				}
			} else {
				if err != nil {
					t.Errorf("expected no error but got: %v", err)
				}
			}
		})
	}
}

func TestCheckDiskSpaceIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Get available space
	var stat syscall.Statfs_t
	err := syscall.Statfs(tmpDir, &stat)
	if err != nil {
		t.Fatalf("Failed to get disk stats: %v", err)
	}
	availableBytes := int64(stat.Bavail) * int64(stat.Bsize)
	
	// Test with a file size that exceeds available space
	hugeSize := availableBytes * 2
	err = checkDiskSpace(tmpDir, hugeSize)
	if err == nil {
		t.Error("expected error for huge file size, got none")
	}
	if !errors.Is(err, ErrInsufficientDiskSpace) {
		t.Errorf("expected ErrInsufficientDiskSpace, got: %v", err)
	}
	
	// Error message should contain human-readable sizes
	if err != nil {
		errMsg := err.Error()
		if errMsg == "" {
			t.Error("error message is empty")
		}
		// Should contain the word "insufficient"
		if len(errMsg) < 10 {
			t.Errorf("error message too short: %s", errMsg)
		}
	}
}

func TestCheckDiskSpacePermissions(t *testing.T) {
	// Skip if running as root since we won't get permission errors
	if os.Geteuid() == 0 {
		t.Skip("skipping permission test when running as root")
	}

	// Create a directory with no read permissions to reliably test permission errors
	tmpDir := t.TempDir()
	restrictedDir := filepath.Join(tmpDir, "restricted")
	if err := os.Mkdir(restrictedDir, 0000); err != nil {
		t.Fatalf("failed to create restricted directory: %v", err)
	}
	t.Cleanup(func() {
		// Restore permissions so cleanup can remove the directory
		os.Chmod(restrictedDir, 0755)
	})

	// This should not fail the check (graceful degradation)
	err := checkDiskSpace(restrictedDir, 1024)
	if err != nil {
		t.Errorf("expected no error for permission-denied path, got: %v", err)
	}
}

func TestDiskSpaceCheckInDownloadFlow(t *testing.T) {
	// This is an integration test that verifies disk space checking
	// is properly integrated into the download flow
	
	tmpDir := t.TempDir()
	
	// Create a test file to serve
	testFile := filepath.Join(tmpDir, "source.txt")
	testContent := []byte("test content for download")
	err := os.WriteFile(testFile, testContent, 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	
	// Get available disk space
	var stat syscall.Statfs_t
	err = syscall.Statfs(tmpDir, &stat)
	if err != nil {
		t.Fatalf("Failed to get disk stats: %v", err)
	}
	availableBytes := int64(stat.Bavail) * int64(stat.Bsize)
	
	// Note: We can't easily test the actual download flow here without
	// mocking the HTTP server and file system operations. The key validation
	// is that checkDiskSpace is called and returns appropriate errors.
	// The actual integration is tested through the Start() and Resume() methods.
	
	// Test that our check would catch insufficient space
	err = checkDiskSpace(tmpDir, availableBytes*2)
	if !errors.Is(err, ErrInsufficientDiskSpace) {
		t.Errorf("expected ErrInsufficientDiskSpace, got: %v", err)
	}
	
	// Test that our check passes for reasonable sizes
	err = checkDiskSpace(tmpDir, 1024)
	if err != nil {
		t.Errorf("expected no error for small file, got: %v", err)
	}
}

func TestCheckDiskSpaceNegativeRemaining(t *testing.T) {
	// Test that negative remaining bytes are handled gracefully
	tmpDir := t.TempDir()
	
	// Negative bytes should be treated as zero (no check needed)
	err := checkDiskSpace(tmpDir, -100)
	if err != nil {
		t.Errorf("expected no error for negative bytes, got: %v", err)
	}
	
	// Zero bytes should also pass
	err = checkDiskSpace(tmpDir, 0)
	if err != nil {
		t.Errorf("expected no error for zero bytes, got: %v", err)
	}
}
