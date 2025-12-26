//go:build !windows

package server

import (
	"fmt"
	"net"
	"os"

	"github.com/warpdl/warpdl/common"
)

// createListener creates a Unix socket listener with TCP fallback.
// It first attempts to create a Unix domain socket. If that fails,
// it falls back to a TCP listener on the configured port.
// Transport priority: Unix socket > TCP
func (s *Server) createListener() (net.Listener, error) {
	socketPath := socketPath()

	// Check if file exists and validate type before attempting removal
	if stat, err := os.Stat(socketPath); err == nil {
		// File exists - check if it's actually a socket
		if stat.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("socket path exists but is not a socket: %s (mode: %s)", socketPath, stat.Mode())
		}
		// It's a socket - try to remove the stale socket file
		if err := os.Remove(socketPath); err != nil {
			return nil, fmt.Errorf("failed to remove stale socket: %w", err)
		}
	} else if !os.IsNotExist(err) {
		// Some other error occurred during stat (not just "file doesn't exist")
		return nil, fmt.Errorf("failed to stat socket path: %w", err)
	}

	l, err := net.ListenUnix("unix", &net.UnixAddr{
		Name: socketPath,
		Net:  "unix",
	})
	if err != nil {
		s.log.Println("Unix socket creation failed:", err.Error())
		s.log.Println("Falling back to TCP")
		tcpListener, tcpErr := net.Listen("tcp", fmt.Sprintf("%s:%d", common.TCPHost, s.port))
		if tcpErr != nil {
			return nil, fmt.Errorf("error listening: %s", tcpErr.Error())
		}
		return tcpListener, nil
	}
	_ = os.Chmod(socketPath, 0766)
	return l, nil
}
