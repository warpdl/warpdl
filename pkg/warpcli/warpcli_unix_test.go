//go:build !windows

package warpcli

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

// createTestListener creates a Unix socket listener for testing.
func createTestListener(t *testing.T) (net.Listener, string, error) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("/tmp", "wdl")
	if err != nil {
		return nil, "", err
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })
	socketPath := filepath.Join(tmpDir, "test.sock")

	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, "", err
	}

	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	return listener, socketPath, nil
}
