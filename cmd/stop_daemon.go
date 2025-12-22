package cmd

import (
    "fmt"
    "os"

    "github.com/urfave/cli"
)

func stopDaemon(ctx *cli.Context) error {
    pid, err := ReadPidFile()
    if err != nil {
        if os.IsNotExist(err) {
            fmt.Println("Daemon is not running (PID file not found)")
            return nil
        }
        fmt.Fprintf(os.Stderr, "Error reading PID file: %v\n", err)
        return nil
    }

    fmt.Printf("Stopping daemon (PID %d)...\n", pid)

    if err := killDaemon(pid); err != nil {
        fmt.Fprintf(os.Stderr, "Error stopping daemon: %v\n", err)
        return nil
    }

    // Note: PID file is removed by daemon's deferred cleanup
    fmt.Println("Daemon stopped successfully")
    return nil
}
