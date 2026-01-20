package warpcli

import (
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	defaultDaemonStartTimeout = 10 * time.Second
	socketPollInterval        = 50 * time.Millisecond
	socketDialTimeout         = 100 * time.Millisecond
)

// getDaemonStartTimeout returns the daemon startup timeout.
// It can be overridden via WARPDL_DAEMON_TIMEOUT environment variable.
func getDaemonStartTimeout() time.Duration {
	if val := os.Getenv("WARPDL_DAEMON_TIMEOUT"); val != "" {
		if d, err := time.ParseDuration(val); err == nil && d > 0 {
			return d
		}
	}
	return defaultDaemonStartTimeout
}

// ensureDaemon checks if the daemon is running and spawns it if not.
// Returns nil if daemon is running or was successfully started.
func ensureDaemon() error {
	path := getConnectionPath()

	// Quick check: can we connect?
	if isDaemonRunning(path) {
		return nil
	}

	if skipDaemonForTests() {
		return fmt.Errorf("daemon not running (test mode)")
	}

	// Spawn daemon
	if err := spawnDaemon(); err != nil {
		return err
	}

	// Wait for socket to become available
	return waitForSocket(path, getDaemonStartTimeout())
}

func skipDaemonForTests() bool {
	if os.Getenv("WARPDL_TEST_SKIP_DAEMON") != "1" {
		return false
	}
	return strings.HasSuffix(os.Args[0], ".test")
}

// waitForSocket polls until the socket/pipe becomes available or timeout expires.
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
