//go:build !windows

package ext

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

// createTestListener creates a Unix socket listener for testing on Unix systems.
// It uses a short path under /tmp to avoid macOS path length limits.
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
