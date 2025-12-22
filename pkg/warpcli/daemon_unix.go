//go:build !windows

package warpcli

import (
	"fmt"
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
