//go:build !windows

package server

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// TestCreateListenerSocketPathIsDirectory tests error when socket path is a directory
func TestCreateListenerSocketPathIsDirectory(t *testing.T) {
	// Setup: Create a directory at socket path
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")
	if err := os.Mkdir(sockPath, 0755); err != nil {
		t.Fatalf("failed to create test directory: %v", err)
	}
	t.Setenv(common.SocketPathEnv, sockPath)

	s := &Server{
		log:  log.New(io.Discard, "", 0),
		port: 0,
	}
	_, err := s.createListener()

	// Expected: Error should mention "not a socket"
	if err == nil {
		t.Fatalf("expected error for directory at socket path")
	}
	if !strings.Contains(err.Error(), "not a socket") {
		t.Fatalf("expected error about non-socket, got: %v", err)
	}
}

// TestCreateListenerRemoveStaleSocket tests removal of stale socket
func TestCreateListenerRemoveStaleSocket(t *testing.T) {
	// Use /tmp to avoid path length issues with Unix sockets
	sockPath := fmt.Sprintf("/tmp/warpdl-test-%d.sock", time.Now().UnixNano())
	defer os.Remove(sockPath)

	// Create a fake socket file (regular file pretending to be a socket won't work)
	// Instead, we'll test that if a socket path exists, it gets removed properly
	// by creating two sequential listeners
	t.Setenv(common.SocketPathEnv, sockPath)

	// First listener creates the socket
	s1 := &Server{
		log:  log.New(io.Discard, "", 0),
		port: 0,
	}
	l1, err := s1.createListener()
	if err != nil {
		t.Fatalf("failed to create first listener: %v", err)
	}

	// Get the address before closing
	addr := l1.Addr()
	l1.Close()

	// Second server should be able to create listener even if socket file might still exist
	s2 := &Server{
		log:  log.New(io.Discard, "", 0),
		port: 0,
	}
	l2, err := s2.createListener()
	if err != nil {
		t.Fatalf("failed to create second listener: %v", err)
	}
	defer l2.Close()

	// Verify it's a Unix socket and on the same path
	if l2.Addr().Network() != "unix" {
		t.Fatalf("expected unix socket, got %s", l2.Addr().Network())
	}
	if l2.Addr().String() != addr.String() {
		t.Fatalf("expected same socket path %s, got %s", addr.String(), l2.Addr().String())
	}
}

// TestCreateListenerSocketPathIsSymlink tests error when socket path is a symlink to non-socket
func TestCreateListenerSocketPathIsSymlink(t *testing.T) {
	// Setup: Create a regular file and symlink to it
	tmpDir := t.TempDir()
	regularFile := filepath.Join(tmpDir, "regular.txt")
	sockPath := filepath.Join(tmpDir, "test.sock")

	if err := os.WriteFile(regularFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create regular file: %v", err)
	}
	if err := os.Symlink(regularFile, sockPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}
	t.Setenv(common.SocketPathEnv, sockPath)

	s := &Server{
		log:  log.New(io.Discard, "", 0),
		port: 0,
	}
	_, err := s.createListener()

	// Expected: Error should mention "not a socket"
	if err == nil {
		t.Fatalf("expected error for symlink at socket path")
	}
	if !strings.Contains(err.Error(), "not a socket") {
		t.Fatalf("expected error about non-socket, got: %v", err)
	}
}
