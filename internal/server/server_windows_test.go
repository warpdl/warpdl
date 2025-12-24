//go:build windows

package server

import (
	"fmt"
	"net"
	"testing"

	"github.com/warpdl/warpdl/common"
)

// getTestSocketPath returns a dummy path for Windows (unused, TCP is used instead).
func getTestSocketPath(t *testing.T) string {
	t.Helper()
	// On Windows, we use TCP, so return an invalid path to trigger TCP fallback
	return "/nonexistent/path/test.sock"
}

// setupTestListener configures environment for TCP testing on Windows.
func setupTestListener(t *testing.T, sockPath string) {
	t.Helper()
	// Force TCP mode on Windows
	t.Setenv(common.ForceTCPEnv, "1")
	// Use the invalid socket path to trigger TCP fallback
	t.Setenv("WARPDL_SOCKET_PATH", sockPath)
}

// createTestListener creates a TCP listener for tests on Windows.
func createTestListener(t *testing.T) (net.Listener, string, error) {
	t.Helper()

	listener, err := net.Listen("tcp", fmt.Sprintf("%s:0", common.TCPHost))
	if err != nil {
		return nil, "", err
	}

	port := listener.Addr().(*net.TCPAddr).Port
	t.Setenv(common.ForceTCPEnv, "1")
	t.Setenv(common.TCPPortEnv, fmt.Sprintf("%d", port))

	return listener, "", nil
}
