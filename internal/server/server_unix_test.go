//go:build !windows

package server

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

// getTestSocketPath returns a Unix socket path for testing.
func getTestSocketPath(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	return filepath.Join(tmpDir, "test.sock")
}

// setupTestListener configures environment for Unix socket testing.
func setupTestListener(t *testing.T, sockPath string) {
	t.Helper()
	_ = os.Remove(sockPath)
	t.Setenv("WARPDL_SOCKET_PATH", sockPath)
}

// createTestListener creates a Unix socket listener for tests.
func createTestListener(t *testing.T) (net.Listener, string, error) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("/tmp", "wdl")
	if err != nil {
		return nil, "", err
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })
	socketPath := filepath.Join(tmpDir, "w.sock")

	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, "", err
	}

	return listener, socketPath, nil
}
