package warpcli

import (
	"fmt"
	"net"
	"time"
)

const (
	daemonStartTimeout = 3 * time.Second
	socketPollInterval = 50 * time.Millisecond
	socketDialTimeout  = 100 * time.Millisecond
)

// ensureDaemon checks if the daemon is running and spawns it if not.
// Returns nil if daemon is running or was successfully started.
func ensureDaemon() error {
	path := socketPath()

	// Quick check: can we connect?
	if isDaemonRunning(path) {
		return nil
	}

	// Spawn daemon
	if err := spawnDaemon(); err != nil {
		return err
	}

	// Wait for socket to become available
	return waitForSocket(path, daemonStartTimeout)
}

// isDaemonRunning checks if the daemon is reachable via the socket.
func isDaemonRunning(path string) bool {
	conn, err := net.DialTimeout("unix", path, socketDialTimeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// waitForSocket polls until the socket becomes available or timeout expires.
func waitForSocket(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isDaemonRunning(path) {
			return nil
		}
		time.Sleep(socketPollInterval)
	}
	return fmt.Errorf("daemon failed to start within %v", timeout)
}
