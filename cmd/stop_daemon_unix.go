//go:build !windows

package cmd

import (
    "fmt"
    "os"
    "syscall"
    "time"
)

const (
    shutdownTimeout  = 5 * time.Second
    pollInterval     = 100 * time.Millisecond
)

// killDaemon sends SIGTERM to the daemon and waits for it to exit.
// If the daemon doesn't exit within the timeout, it sends SIGKILL.
func killDaemon(pid int) error {
    process, err := os.FindProcess(pid)
    if err != nil {
        return fmt.Errorf("process not found: %w", err)
    }

    // Check if process is running
    if err := process.Signal(syscall.Signal(0)); err != nil {
        return fmt.Errorf("daemon not running (PID %d): %w", pid, err)
    }

    // Send SIGTERM for graceful shutdown
    if err := process.Signal(syscall.SIGTERM); err != nil {
        return fmt.Errorf("failed to send SIGTERM: %w", err)
    }

    // Wait for process to exit
    deadline := time.Now().Add(shutdownTimeout)
    for time.Now().Before(deadline) {
        // Check if process is still running
        if err := process.Signal(syscall.Signal(0)); err != nil {
            // Process has exited
            return nil
        }
        time.Sleep(pollInterval)
    }

    // Timeout exceeded, send SIGKILL
    fmt.Println("Graceful shutdown timeout, forcing kill...")
    if err := process.Signal(syscall.SIGKILL); err != nil {
        return fmt.Errorf("failed to send SIGKILL: %w", err)
    }

    // Wait a bit for SIGKILL to take effect
    time.Sleep(500 * time.Millisecond)
    return nil
}
