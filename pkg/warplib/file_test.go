package warplib

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestMoveFile_SameDevice_Success(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()

	// Create source file with content
	srcPath := filepath.Join(tmpDir, "source.txt")
	content := []byte("test content for same device move")
	if err := os.WriteFile(srcPath, content, 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	// Define destination path in the same directory
	dstPath := filepath.Join(tmpDir, "destination.txt")

	// Perform the move
	err := moveFile(srcPath, dstPath)
	if err != nil {
		t.Errorf("moveFile() returned error: %v", err)
	}

	// Verify destination file exists and has correct content
	gotContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Errorf("failed to read destination file: %v", err)
	}
	if !bytes.Equal(gotContent, content) {
		t.Errorf("destination content = %q, want %q", gotContent, content)
	}

	// Verify source file no longer exists
	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Errorf("source file should not exist after move, got err: %v", err)
	}
}

func TestMoveFile_CrossDevice_FallbackToCopyDelete(t *testing.T) {
	// Create two separate temporary directories
	// While they may be on the same device, the function should still work
	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	// Create source file with content
	srcPath := filepath.Join(tmpDir1, "source.txt")
	content := []byte("test content for cross device move")
	if err := os.WriteFile(srcPath, content, 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	// Define destination path in a different directory
	dstPath := filepath.Join(tmpDir2, "destination.txt")

	// Perform the move
	err := moveFile(srcPath, dstPath)
	if err != nil {
		t.Errorf("moveFile() returned error: %v", err)
	}

	// Verify destination file exists and has correct content
	gotContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Errorf("failed to read destination file: %v", err)
	}
	if !bytes.Equal(gotContent, content) {
		t.Errorf("destination content = %q, want %q", gotContent, content)
	}

	// Verify source file no longer exists
	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Errorf("source file should not exist after move, got err: %v", err)
	}
}

func TestMoveFile_SourceNotExist(t *testing.T) {
	tmpDir := t.TempDir()

	srcPath := filepath.Join(tmpDir, "nonexistent.txt")
	dstPath := filepath.Join(tmpDir, "destination.txt")

	err := moveFile(srcPath, dstPath)
	if err == nil {
		t.Error("moveFile() should return error for non-existent source")
	}
	// Check that the wrapped error contains fs.ErrNotExist
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("moveFile() error = %v, want error wrapping fs.ErrNotExist", err)
	}
}

func TestMoveFile_DestDirNotExist(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source file
	srcPath := filepath.Join(tmpDir, "source.txt")
	content := []byte("test content")
	if err := os.WriteFile(srcPath, content, 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	// Destination directory does not exist
	dstPath := filepath.Join(tmpDir, "nonexistent_dir", "destination.txt")

	err := moveFile(srcPath, dstPath)
	if err == nil {
		t.Error("moveFile() should return error when destination directory does not exist")
	}
}

func TestMoveFile_LargeFile(t *testing.T) {
	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	// Create a 1MB file with random content
	srcPath := filepath.Join(tmpDir1, "large_source.bin")
	content := make([]byte, 1024*1024) // 1MB
	if _, err := io.ReadFull(rand.Reader, content); err != nil {
		t.Fatalf("failed to generate random content: %v", err)
	}
	if err := os.WriteFile(srcPath, content, 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	dstPath := filepath.Join(tmpDir2, "large_destination.bin")

	// Perform the move
	err := moveFile(srcPath, dstPath)
	if err != nil {
		t.Errorf("moveFile() returned error: %v", err)
	}

	// Verify destination file has correct content
	gotContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Errorf("failed to read destination file: %v", err)
	}
	if !bytes.Equal(gotContent, content) {
		t.Errorf("destination content does not match source (size: got %d, want %d)",
			len(gotContent), len(content))
	}

	// Verify source file no longer exists
	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Errorf("source file should not exist after move, got err: %v", err)
	}
}

func TestMoveFile_PreservesPermissions(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source file with specific permissions
	srcPath := filepath.Join(tmpDir, "source_perms.txt")
	content := []byte("test content for permissions")
	if err := os.WriteFile(srcPath, content, 0755); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	// Verify source permissions
	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		t.Fatalf("failed to stat source file: %v", err)
	}
	srcMode := srcInfo.Mode().Perm()

	dstPath := filepath.Join(tmpDir, "destination_perms.txt")

	// Perform the move
	err = moveFile(srcPath, dstPath)
	if err != nil {
		t.Errorf("moveFile() returned error: %v", err)
	}

	// Verify destination file has same permissions
	dstInfo, err := os.Stat(dstPath)
	if err != nil {
		t.Errorf("failed to stat destination file: %v", err)
	}
	dstMode := dstInfo.Mode().Perm()

	if dstMode != srcMode {
		t.Errorf("destination permissions = %o, want %o", dstMode, srcMode)
	}
}

func TestCopyAndDelete_Success(t *testing.T) {
	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	// Create source file
	srcPath := filepath.Join(tmpDir1, "source.txt")
	content := []byte("test content for copy and delete")
	if err := os.WriteFile(srcPath, content, 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	dstPath := filepath.Join(tmpDir2, "destination.txt")

	// Perform copy and delete
	err := copyAndDelete(srcPath, dstPath)
	if err != nil {
		t.Errorf("copyAndDelete() returned error: %v", err)
	}

	// Verify destination file exists and has correct content
	gotContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Errorf("failed to read destination file: %v", err)
	}
	if !bytes.Equal(gotContent, content) {
		t.Errorf("destination content = %q, want %q", gotContent, content)
	}

	// Verify source file no longer exists
	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Errorf("source file should not exist after copyAndDelete, got err: %v", err)
	}
}

func TestCopyAndDelete_SourceNotExist(t *testing.T) {
	tmpDir := t.TempDir()

	srcPath := filepath.Join(tmpDir, "nonexistent.txt")
	dstPath := filepath.Join(tmpDir, "destination.txt")

	err := copyAndDelete(srcPath, dstPath)
	if err == nil {
		t.Error("copyAndDelete() should return error for non-existent source")
	}
}

func TestErrCrossDeviceMove_IsSentinel(t *testing.T) {
	// Verify ErrCrossDeviceMove is not nil
	if ErrCrossDeviceMove == nil {
		t.Error("ErrCrossDeviceMove should not be nil")
	}

	// Verify it has a meaningful message
	msg := ErrCrossDeviceMove.Error()
	if msg == "" {
		t.Error("ErrCrossDeviceMove.Error() should not be empty")
	}

	// Verify it's a sentinel error (can be compared with errors.Is)
	if !errors.Is(ErrCrossDeviceMove, ErrCrossDeviceMove) {
		t.Error("ErrCrossDeviceMove should be comparable with errors.Is")
	}
}
