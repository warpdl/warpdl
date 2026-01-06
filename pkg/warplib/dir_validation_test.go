package warplib

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestValidateDownloadDirectory_ValidDir(t *testing.T) {
	validDir := t.TempDir()

	if err := ValidateDownloadDirectory(validDir); err != nil {
		t.Fatalf("expected no error for valid dir, got: %v", err)
	}
}

func TestValidateDownloadDirectory_NonExistent(t *testing.T) {
	err := ValidateDownloadDirectory("/nonexistent/path/12345/xyz")
	if err == nil {
		t.Fatal("expected error for non-existent directory")
	}
	if !errors.Is(err, ErrDirectoryNotFound) {
		t.Errorf("expected ErrDirectoryNotFound, got: %v", err)
	}
}

func TestValidateDownloadDirectory_IsFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "testfile.txt")
	if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	err := ValidateDownloadDirectory(filePath)
	if err == nil {
		t.Fatal("expected error when path is a file")
	}
	if !errors.Is(err, ErrNotADirectory) {
		t.Errorf("expected ErrNotADirectory, got: %v", err)
	}
}

func TestValidateDownloadDirectory_NotWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission test not reliable on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("cannot test as root")
	}

	readOnlyDir := t.TempDir()
	if err := os.Chmod(readOnlyDir, 0555); err != nil {
		t.Fatalf("failed to change permissions: %v", err)
	}
	defer os.Chmod(readOnlyDir, 0755) // restore for cleanup

	err := ValidateDownloadDirectory(readOnlyDir)
	if err == nil {
		t.Fatal("expected error for non-writable directory")
	}
	if !errors.Is(err, ErrDirectoryNotWritable) {
		t.Errorf("expected ErrDirectoryNotWritable, got: %v", err)
	}
}

func TestValidateDownloadDirectory_EmptyPath(t *testing.T) {
	err := ValidateDownloadDirectory("")
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestValidateDownloadDirectory_RelativePath(t *testing.T) {
	// Create a subdirectory in temp
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	// Change to tmpDir
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change dir: %v", err)
	}
	defer os.Chdir(oldWd)

	// Relative path should work
	if err := ValidateDownloadDirectory("subdir"); err != nil {
		t.Fatalf("expected no error for relative path, got: %v", err)
	}
}

func TestValidateDownloadDirectory_Symlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test requires elevated privileges on Windows")
	}

	tmpDir := t.TempDir()
	realDir := filepath.Join(tmpDir, "realdir")
	if err := os.Mkdir(realDir, 0755); err != nil {
		t.Fatalf("failed to create realdir: %v", err)
	}

	linkPath := filepath.Join(tmpDir, "linkdir")
	if err := os.Symlink(realDir, linkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	// Symlink to valid directory should work
	if err := ValidateDownloadDirectory(linkPath); err != nil {
		t.Fatalf("expected no error for symlink to valid dir, got: %v", err)
	}
}
