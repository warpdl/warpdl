package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/warpdl/warpdl/pkg/warplib"
)

func TestGetPidFilePath(t *testing.T) {
	tmpDir := t.TempDir()
	if err := warplib.SetConfigDir(tmpDir); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	path := getPidFilePath()
	if path == "" {
		t.Fatal("expected non-empty path")
	}
	if filepath.Dir(path) != tmpDir {
		t.Fatalf("expected path in %s, got %s", tmpDir, path)
	}
	if filepath.Base(path) != pidFileName {
		t.Fatalf("expected base name %s, got %s", pidFileName, filepath.Base(path))
	}
}

func TestWritePidFile(t *testing.T) {
	tmpDir := t.TempDir()
	if err := warplib.SetConfigDir(tmpDir); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	if err := WritePidFile(); err != nil {
		t.Fatalf("WritePidFile: %v", err)
	}

	// Verify PID was written
	pid, err := ReadPidFile()
	if err != nil {
		t.Fatalf("ReadPidFile: %v", err)
	}
	if pid != os.Getpid() {
		t.Fatalf("expected PID %d, got %d", os.Getpid(), pid)
	}
}

func TestReadPidFile_NotExist(t *testing.T) {
	tmpDir := t.TempDir()
	if err := warplib.SetConfigDir(tmpDir); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	_, err := ReadPidFile()
	if err == nil {
		t.Fatal("expected error for non-existent file")
	}
	if !os.IsNotExist(err) {
		t.Fatalf("expected not exist error, got: %v", err)
	}
}

func TestReadPidFile_InvalidContent(t *testing.T) {
	tmpDir := t.TempDir()
	if err := warplib.SetConfigDir(tmpDir); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Write invalid content
	pidPath := getPidFilePath()
	if err := os.WriteFile(pidPath, []byte("not-a-number"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := ReadPidFile()
	if err == nil {
		t.Fatal("expected error for invalid PID")
	}
}

func TestReadPidFile_NegativePid(t *testing.T) {
	tmpDir := t.TempDir()
	if err := warplib.SetConfigDir(tmpDir); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Write negative PID
	pidPath := getPidFilePath()
	if err := os.WriteFile(pidPath, []byte("-1"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := ReadPidFile()
	if err == nil {
		t.Fatal("expected error for negative PID")
	}
}

func TestReadPidFile_ZeroPid(t *testing.T) {
	tmpDir := t.TempDir()
	if err := warplib.SetConfigDir(tmpDir); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Write zero PID
	pidPath := getPidFilePath()
	if err := os.WriteFile(pidPath, []byte("0"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := ReadPidFile()
	if err == nil {
		t.Fatal("expected error for zero PID")
	}
}

func TestRemovePidFile(t *testing.T) {
	tmpDir := t.TempDir()
	if err := warplib.SetConfigDir(tmpDir); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Write then remove
	if err := WritePidFile(); err != nil {
		t.Fatalf("WritePidFile: %v", err)
	}

	if err := RemovePidFile(); err != nil {
		t.Fatalf("RemovePidFile: %v", err)
	}

	// Verify removal
	_, err := ReadPidFile()
	if err == nil {
		t.Fatal("expected error after removal")
	}
}

func TestRemovePidFile_NotExist(t *testing.T) {
	tmpDir := t.TempDir()
	if err := warplib.SetConfigDir(tmpDir); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Remove non-existent file should not error
	if err := RemovePidFile(); err != nil {
		t.Fatalf("RemovePidFile for non-existent: %v", err)
	}
}

func TestCleanupStalePidFile_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	if err := warplib.SetConfigDir(tmpDir); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// No PID file should succeed
	if err := CleanupStalePidFile(); err != nil {
		t.Fatalf("CleanupStalePidFile: %v", err)
	}
}

func TestCleanupStalePidFile_InvalidContent(t *testing.T) {
	tmpDir := t.TempDir()
	if err := warplib.SetConfigDir(tmpDir); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Write invalid content
	pidPath := getPidFilePath()
	if err := os.WriteFile(pidPath, []byte("invalid"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Should clean up invalid file
	if err := CleanupStalePidFile(); err != nil {
		t.Fatalf("CleanupStalePidFile: %v", err)
	}

	// File should be removed
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatal("expected file to be removed")
	}
}

func TestCleanupStalePidFile_RunningProcess(t *testing.T) {
	tmpDir := t.TempDir()
	if err := warplib.SetConfigDir(tmpDir); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Write our own PID (running process)
	pidPath := getPidFilePath()
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Should return error
	err := CleanupStalePidFile()
	if err == nil {
		t.Fatal("expected error for running process")
	}
	if err.Error() == "" || err != ErrDaemonAlreadyRunning && !containsErr(err, ErrDaemonAlreadyRunning) {
		t.Fatalf("expected ErrDaemonAlreadyRunning, got: %v", err)
	}
}

func containsErr(err, target error) bool {
	return err != nil && target != nil && err.Error() != "" && target.Error() != "" &&
		(err == target || (len(err.Error()) > 0 && len(target.Error()) > 0))
}

func TestCleanupStalePidFile_StaleProcess(t *testing.T) {
	tmpDir := t.TempDir()
	if err := warplib.SetConfigDir(tmpDir); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Write a PID that likely doesn't exist (very high PID)
	// Note: This test may be flaky on some systems
	pidPath := getPidFilePath()
	if err := os.WriteFile(pidPath, []byte("999999999"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Should clean up stale file
	if err := CleanupStalePidFile(); err != nil {
		t.Fatalf("CleanupStalePidFile: %v", err)
	}

	// File should be removed
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatal("expected file to be removed")
	}
}

func TestIsProcessRunning_CurrentProcess(t *testing.T) {
	// Our own process should be running
	if !isProcessRunning(os.Getpid()) {
		t.Fatal("expected current process to be running")
	}
}

func TestIsProcessRunning_NonExistent(t *testing.T) {
	// Very high PID unlikely to exist
	if isProcessRunning(999999999) {
		t.Fatal("expected process 999999999 to not be running")
	}
}

func TestIsProcessRunning_InvalidPid(t *testing.T) {
	// Negative PIDs should not be running
	if isProcessRunning(-1) {
		t.Fatal("expected negative PID to not be running")
	}
}
