//go:build windows

package cmd

import (
    "fmt"
    "os"
    "time"
)

const (
    shutdownTimeout = 5 * time.Second
)

// killDaemon sends an interrupt signal to the daemon and waits for it to exit.
// On Windows, we use os.Interrupt and then forcefully terminate if needed.
func killDaemon(pid int) error {
    process, err := os.FindProcess(pid)
    if err != nil {
        return fmt.Errorf("process not found: %w", err)
    }

    // On Windows, os.Interrupt may not work for all processes.
    // We'll try it first, then fall back to Kill.
    if err := process.Signal(os.Interrupt); err != nil {
        // If interrupt fails, try to kill directly
        if err := process.Kill(); err != nil {
            return fmt.Errorf("failed to stop daemon: %w", err)
        }
        return nil
    }

    // Wait for process to exit with timeout using a goroutine
    done := make(chan error, 1)
    go func() {
        _, err := process.Wait()
        done <- err
    }()

    select {
    case <-done:
        return nil // Process exited
    case <-time.After(shutdownTimeout):
        // Timeout exceeded, force kill
        fmt.Println("Graceful shutdown timeout, forcing kill...")
        if err := process.Kill(); err != nil {
            return fmt.Errorf("failed to kill daemon: %w", err)
        }
        return nil
    }
}
