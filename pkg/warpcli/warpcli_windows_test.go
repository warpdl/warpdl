//go:build windows

package warpcli

import (
	"fmt"
	"net"
	"testing"

	"github.com/warpdl/warpdl/common"
)

// createTestListener creates a TCP listener for testing on Windows.
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
