package cmd

import (
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "strconv"
    "strings"

    "github.com/warpdl/warpdl/pkg/warplib"
)

const pidFileName = "daemon.pid"

// ErrDaemonAlreadyRunning indicates another daemon instance is already running.
var ErrDaemonAlreadyRunning = errors.New("daemon already running")

// getPidFilePath returns the path to the daemon PID file.
func getPidFilePath() string {
    return filepath.Join(warplib.ConfigDir, pidFileName)
}

// CleanupStalePidFile checks if a PID file exists and removes it if the
// process is no longer running. Returns ErrDaemonAlreadyRunning if another
// daemon instance is still active.
func CleanupStalePidFile() error {
    pid, err := ReadPidFile()
    if err != nil {
        if os.IsNotExist(err) {
            return nil // No PID file, nothing to clean up
        }
        // Invalid PID file, remove it
        RemovePidFile()
        return nil
    }

    // Check if process is still running
    if isProcessRunning(pid) {
        return fmt.Errorf("%w with PID %d", ErrDaemonAlreadyRunning, pid)
    }

    // Process is dead, clean up stale PID file
    RemovePidFile()
    return nil
}

// WritePidFile writes the current process ID to the PID file.
func WritePidFile() error {
    pid := os.Getpid()
    return os.WriteFile(getPidFilePath(), []byte(strconv.Itoa(pid)), 0644)
}

// ReadPidFile reads and returns the PID from the PID file.
func ReadPidFile() (int, error) {
    data, err := os.ReadFile(getPidFilePath())
    if err != nil {
        return 0, err
    }
    pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
    if err != nil {
        return 0, fmt.Errorf("invalid PID in file: %w", err)
    }
    if pid <= 0 {
        return 0, fmt.Errorf("invalid PID: %d", pid)
    }
    return pid, nil
}

// RemovePidFile removes the PID file.
func RemovePidFile() error {
    err := os.Remove(getPidFilePath())
    if os.IsNotExist(err) {
        return nil // Already removed, not an error
    }
    return err
}
