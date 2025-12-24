//go:build windows

package cmd

import (
	"fmt"
	"net"
	"testing"

	"github.com/warpdl/warpdl/common"
)

// createTestListener creates a TCP listener for testing on Windows.
// It also sets up the environment to force TCP mode for the client.
func createTestListener(t *testing.T, socketPath string) (net.Listener, error) {
	t.Helper()
	// On Windows, use TCP instead of Unix sockets
	// Force TCP mode so clients connect via TCP
	t.Setenv(common.ForceTCPEnv, "1")
	t.Setenv(common.TCPPortEnv, fmt.Sprintf("%d", common.DefaultTCPPort))

	return net.Listen("tcp", fmt.Sprintf("%s:%d", common.TCPHost, common.DefaultTCPPort))
}
