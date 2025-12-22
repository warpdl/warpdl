//go:build windows

package cmd

import (
    "golang.org/x/sys/windows"
)

// isProcessRunning checks if a process with the given PID is still running.
// On Windows, we open the process with minimal access rights to check if it exists.
func isProcessRunning(pid int) bool {
    // Try to open the process with SYNCHRONIZE access
    // This is a minimal access right that lets us check if the process exists
    handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
    if err != nil {
        return false
    }
    windows.CloseHandle(handle)
    return true
}
