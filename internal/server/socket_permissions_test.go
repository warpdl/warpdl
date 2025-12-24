//go:build !windows

package server

import (
	"io"
	"log"
	"os"
	"testing"
)

// TestSocketPermissions verifies that Unix sockets are created with
// secure 0700 permissions (owner read/write/execute only).
func TestSocketPermissions(t *testing.T) {
	sockPath := getTestSocketPath(t)
	setupTestListener(t, sockPath)

	s := &Server{
		log:  log.New(io.Discard, "", 0),
		port: 0,
	}
	l, err := s.createListener()
	if err != nil {
		t.Fatalf("createListener: %v", err)
	}
	defer l.Close()

	// Check socket file permissions
	info, err := os.Stat(sockPath)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}

	mode := info.Mode().Perm()
	expected := os.FileMode(0700)
	if mode != expected {
		t.Errorf("socket permissions = %o, want %o", mode, expected)
	}
}
