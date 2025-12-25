//go:build windows

package warpcli

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"

	"github.com/Microsoft/go-winio"
)

// spawnDaemon starts the daemon as a background process on Windows.
func spawnDaemon() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	cmd := exec.Command(executable, "daemon")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Release process so it doesn't become a zombie when it exits
	_ = cmd.Process.Release()

	return nil
}

// getConnectionPath returns the Windows named pipe path for daemon communication.
func getConnectionPath() string {
	return pipePath()
}

// isDaemonRunning checks if the daemon is reachable via named pipe or TCP.
// It tries named pipe first (unless forceTCP is enabled), then falls back to TCP.
func isDaemonRunning(path string) bool {
	// Try named pipe first unless forceTCP is enabled
	if !forceTCP() {
		timeout := socketDialTimeout
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		conn, err := winio.DialPipeContext(ctx, path)
		if err == nil {
			conn.Close()
			debugLog("daemon detected via named pipe: %s", path)
			return true
		}
		debugLog("Named pipe dial failed: %v, trying TCP fallback", err)
	}

	// Try TCP fallback
	tcpAddr := tcpAddress()
	conn, err := net.DialTimeout("tcp", tcpAddr, socketDialTimeout)
	if err == nil {
		conn.Close()
		debugLog("daemon detected via TCP: %s", tcpAddr)
		return true
	}
	debugLog("TCP dial failed: %v", err)

	return false
}
