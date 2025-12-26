//go:build !windows

package warpcli

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
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

// TestNewClient_FallsBackToTCP verifies that when Unix socket fails,
// client automatically falls back to TCP connection
func TestNewClient_FallsBackToTCP(t *testing.T) {
	oldEnsure := ensureDaemonFunc
	oldDial := dialFunc
	defer func() {
		ensureDaemonFunc = oldEnsure
		dialFunc = oldDial
	}()

	ensureDaemonFunc = func() error { return nil }

	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	callCount := 0
	dialFunc = func(network, addr string) (net.Conn, error) {
		callCount++
		if network == "unix" {
			return nil, errors.New("unix socket connection failed")
		}
		if network == "tcp" {
			return c1, nil
		}
		return nil, errors.New("unexpected network type")
	}

	client, err := NewClient()
	if err != nil {
		t.Fatalf("expected successful fallback to TCP, got error: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if callCount != 2 {
		t.Fatalf("expected 2 dial calls (unix then tcp), got %d", callCount)
	}

	// Verify client is usable
	if client.conn == nil {
		t.Fatal("expected non-nil connection")
	}
}

// TestNewClient_BothTransportsFail verifies that error is returned
// when both Unix socket and TCP connection fail
func TestNewClient_BothTransportsFail(t *testing.T) {
	oldEnsure := ensureDaemonFunc
	oldDial := dialFunc
	defer func() {
		ensureDaemonFunc = oldEnsure
		dialFunc = oldDial
	}()

	ensureDaemonFunc = func() error { return nil }

	callCount := 0
	dialFunc = func(network, addr string) (net.Conn, error) {
		callCount++
		if network == "unix" {
			return nil, errors.New("unix socket failed")
		}
		if network == "tcp" {
			return nil, errors.New("tcp connection failed")
		}
		return nil, errors.New("unexpected network type")
	}

	client, err := NewClient()
	if err == nil {
		t.Fatal("expected error when both transports fail")
	}
	if client != nil {
		t.Fatal("expected nil client on error")
	}
	if !strings.Contains(err.Error(), "failed to connect") {
		t.Fatalf("expected 'failed to connect' error, got: %v", err)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 dial calls (unix then tcp), got %d", callCount)
	}
}
