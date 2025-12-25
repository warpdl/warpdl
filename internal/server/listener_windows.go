//go:build windows

package server

import (
    "fmt"
    "net"

    "github.com/Microsoft/go-winio"
    "github.com/warpdl/warpdl/common"
)

// pipeSecurityDescriptor restricts pipe access to:
// - SYSTEM: Full control (for service scenarios)
// - Built-in Administrators: Full control
// - Creator Owner: Full control (the user running the daemon)
// This prevents unauthorized users from connecting to the daemon.
const pipeSecurityDescriptor = "D:(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;CO)"

// createListener creates a Windows named pipe listener with TCP fallback.
// It first attempts to create a named pipe listener with restricted permissions.
// If that fails, it falls back to a TCP listener on the configured port.
// Transport priority: Named pipe > TCP
//
// Security: The pipe uses a restricted security descriptor to limit access
// to SYSTEM, Administrators, and the Creator Owner only.
func (s *Server) createListener() (net.Listener, error) {
    if forceTCP() {
        s.log.Println("Force TCP mode enabled, using TCP listener")
        return net.Listen("tcp", fmt.Sprintf("%s:%d", common.TCPHost, s.port))
    }

    pipePath := pipePath()

    // Configure pipe with security restrictions
    cfg := &winio.PipeConfig{
        SecurityDescriptor: pipeSecurityDescriptor,
    }

    l, err := winio.ListenPipe(pipePath, cfg)
    if err != nil {
        s.log.Println("WARNING: Named pipe creation failed:", err.Error())
        s.log.Println("Falling back to TCP (firewall prompts may occur)")
        tcpListener, tcpErr := net.Listen("tcp", fmt.Sprintf("%s:%d", common.TCPHost, s.port))
        if tcpErr != nil {
            return nil, fmt.Errorf("error listening: %s", tcpErr.Error())
        }
        return tcpListener, nil
    }
    return l, nil
}
