//go:build !windows

package server

import (
    "io"
    "log"
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
