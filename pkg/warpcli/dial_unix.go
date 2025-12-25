//go:build !windows

package warpcli

import (
	"fmt"
	"net"
)

// dial establishes a connection to the daemon using Unix socket with TCP fallback.
// It first attempts to connect via Unix socket. If that fails, it falls back to TCP.
// Transport priority: Unix socket > TCP
func dial() (net.Conn, error) {
	debugLog("Attempting connection via Unix socket at %s", socketPath())
	conn, unixErr := dialFunc("unix", socketPath())
	if unixErr != nil {
		debugLog("Unix socket connection failed: %v, falling back to TCP", unixErr)
		conn, err := dialFunc("tcp", tcpAddress())
		if err != nil {
			return nil, fmt.Errorf("failed to connect: unix socket error: %v; tcp error: %w", unixErr, err)
		}
		debugLog("Successfully connected via TCP fallback to %s", tcpAddress())
		return conn, nil
	}
	debugLog("Successfully connected via Unix socket")
	return conn, nil
}
