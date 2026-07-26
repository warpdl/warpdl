package cmd

import (
	"fmt"
	"os"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
)

func stopDaemon(ctx *cli.Context) error {
	pid, err := ReadPidFile()
	if err != nil {
		if os.IsNotExist(err) {
			err = fmt.Errorf("daemon is not running (PID file not found): %w", err)
		}
		return common.PrintRuntimeErr(ctx, "stop-daemon", "read_pid", err)
	}

	fmt.Printf("Stopping daemon (PID %d)...\n", pid)

	if err := killDaemon(pid); err != nil {
		return common.PrintRuntimeErr(ctx, "stop-daemon", "stop", err)
	}

	// Note: PID file is removed by daemon's deferred cleanup
	fmt.Println("Daemon stopped successfully")
	return nil
}
