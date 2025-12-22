//go:build !windows

package cmd

import (
    "os"
    "syscall"
)

// isProcessRunning checks if a process with the given PID is still running.
// On Unix systems, this uses signal 0 to check process existence.
func isProcessRunning(pid int) bool {
    process, err := os.FindProcess(pid)
    if err != nil {
        return false
    }
    // Signal 0 doesn't actually send a signal but checks if process exists
    err = process.Signal(syscall.Signal(0))
    return err == nil
}
