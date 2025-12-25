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
	_ = os.Remove(socketPath)
	l, err := net.ListenUnix("unix", &net.UnixAddr{
		Name: socketPath,
		Net:  "unix",
	})
	if err != nil {
		s.log.Println("Error occurred while using unix socket:", err.Error())
		s.log.Println("Trying to use tcp socket")
		tcpListener, tcpErr := net.Listen("tcp", fmt.Sprintf("%s:%d", common.TCPHost, s.port))
		if tcpErr != nil {
			return nil, fmt.Errorf("error listening: %s", tcpErr.Error())
		}
		return tcpListener, nil
	}
	_ = os.Chmod(socketPath, 0766)
	return l, nil
}
