//go:build !windows

package server

import (
    "io"
    "log"
    "net"
    "os"
    "path/filepath"
    "testing"

    "github.com/warpdl/warpdl/common"
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

// TestCreateListenerTCPFallback tests Unix socket fallback to TCP
func TestCreateListenerTCPFallback(t *testing.T) {
    // Use an invalid path to force TCP fallback
    t.Setenv(common.SocketPathEnv, "/nonexistent/path/test.sock")

    s := &Server{
        log:  log.New(io.Discard, "", 0),
        port: 0, // port 0 lets OS pick available port
    }
    l, err := s.createListener()
    if err != nil {
        t.Fatalf("createListener: %v", err)
    }
    defer l.Close()

    if l.Addr().Network() != "tcp" {
        t.Fatalf("expected tcp socket, got %s", l.Addr().Network())
    }
}
