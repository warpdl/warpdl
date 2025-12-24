//go:build windows

package ext

import (
	"fmt"
	"net"
	"testing"

	"github.com/warpdl/warpdl/common"
)

// createTestListener creates a TCP listener for testing on Windows.
// It uses a dynamic port (0) to avoid conflicts between parallel tests,
// then sets the environment variable so the client knows which port to use.
func createTestListener(t *testing.T) (net.Listener, string, error) {
	t.Helper()

	// Listen on port 0 to get a random available port
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:0", common.TCPHost))
	if err != nil {
		return nil, "", err
	}

	// Extract the actual port assigned by the OS
	port := listener.Addr().(*net.TCPAddr).Port

	// Force TCP mode so clients connect via TCP to this specific port
	t.Setenv(common.ForceTCPEnv, "1")
	t.Setenv(common.TCPPortEnv, fmt.Sprintf("%d", port))

	// Return empty socket path since Windows uses TCP
	return listener, "", nil
}
