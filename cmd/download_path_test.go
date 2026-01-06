package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/warpdl/warpdl/common"
)

// TestResolveDownloadPath_EnvVarFallback tests that the environment variable
// is used when the CLI flag is not provided.
func TestResolveDownloadPath_EnvVarFallback(t *testing.T) {
	envDir := t.TempDir()
	t.Setenv(common.DefaultDlDirEnv, envDir)

	// CLI flag is empty, should fall back to env var
	result, err := resolveDownloadPath("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != envDir {
		t.Errorf("expected env dir %s, got %s", envDir, result)
	}
}

// TestResolveDownloadPath_CLIFlagPriority tests that CLI flag takes priority
// over the environment variable.
func TestResolveDownloadPath_CLIFlagPriority(t *testing.T) {
	cliDir := t.TempDir()
	envDir := t.TempDir()
	t.Setenv(common.DefaultDlDirEnv, envDir)

	result, err := resolveDownloadPath(cliDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != cliDir {
		t.Errorf("expected CLI dir %s, got %s", cliDir, result)
	}
}

// TestResolveDownloadPath_CwdFallback tests that current working directory
// is used when neither flag nor env var is set.
func TestResolveDownloadPath_CwdFallback(t *testing.T) {
	// Ensure env var is not set
	t.Setenv(common.DefaultDlDirEnv, "")

	result, err := resolveDownloadPath("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cwd, _ := os.Getwd()
	if result != cwd {
		t.Errorf("expected cwd %s, got %s", cwd, result)
	}
}

// TestResolveDownloadPath_NonExistentDir tests that an error is returned
// for a non-existent directory.
func TestResolveDownloadPath_NonExistentDir(t *testing.T) {
	nonExistent := "/path/that/does/not/exist/12345"

	_, err := resolveDownloadPath(nonExistent)
	if err == nil {
		t.Fatal("expected error for non-existent directory")
	}
}

// TestResolveDownloadPath_FileInsteadOfDir tests that an error is returned
// when the path is a file instead of a directory.
func TestResolveDownloadPath_FileInsteadOfDir(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "testfile.txt")
	if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	_, err := resolveDownloadPath(filePath)
	if err == nil {
		t.Fatal("expected error when path is a file, not directory")
	}
}

// TestResolveDownloadPath_EnvVarNonExistent tests that an error is returned
// when the env var points to a non-existent path.
func TestResolveDownloadPath_EnvVarNonExistent(t *testing.T) {
	t.Setenv(common.DefaultDlDirEnv, "/nonexistent/path/xyz")

	_, err := resolveDownloadPath("")
	if err == nil {
		t.Fatal("expected error for non-existent env var path")
	}
}

// TestResolveDownloadPath_RelativeToAbsolute tests that relative paths
// are converted to absolute paths.
func TestResolveDownloadPath_RelativeToAbsolute(t *testing.T) {
	// Create a subdirectory in temp
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	// Change to tmpDir
	oldWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change dir: %v", err)
	}
	defer os.Chdir(oldWd)

	result, err := resolveDownloadPath("subdir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !filepath.IsAbs(result) {
		t.Errorf("expected absolute path, got relative: %s", result)
	}
}
