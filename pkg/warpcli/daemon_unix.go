//go:build !windows

package warpcli

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"syscall"
)

// spawnDaemon starts the daemon as a background process on Unix systems.
func spawnDaemon() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	cmd := exec.Command(executable, "daemon")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	// Detach from parent process group so daemon survives CLI exit
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Release process so it doesn't become a zombie when it exits
	_ = cmd.Process.Release()

	return nil
}

// getConnectionPath returns the Unix socket path for daemon communication.
func getConnectionPath() string {
	return socketPath()
}

// isDaemonRunning checks if the daemon is reachable via Unix socket or TCP.
// It tries Unix socket first (unless forceTCP is enabled), then falls back to TCP.
func isDaemonRunning(path string) bool {
	// Try Unix socket first unless forceTCP is enabled
	if !forceTCP() {
		conn, err := net.DialTimeout("unix", path, socketDialTimeout)
		if err == nil {
			conn.Close()
			debugLog("daemon detected via Unix socket: %s", path)
			return true
		}
		debugLog("Unix socket dial failed: %v, trying TCP fallback", err)
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
