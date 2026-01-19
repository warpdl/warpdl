package cmd

import (
	"context"
	"log"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/logger"
)

var (
	startServerFunc = func(serv *server.Server, ctx context.Context) error { return serv.Start(ctx) }
)

func daemon(ctx *cli.Context) error {
	stdLog := logger.NewStandardLogger(log.Default())

	// Clean up stale PID file or fail if daemon already running
	if err := CleanupStalePidFile(); err != nil {
		common.PrintRuntimeErr(ctx, "daemon", "cleanup_pid", err)
		return nil
	}

	// Write PID file
	if err := WritePidFile(); err != nil {
		common.PrintRuntimeErr(ctx, "daemon", "write_pid", err)
		return nil
	}
	defer RemovePidFile()

	// Setup signal handler for graceful shutdown
	shutdownCtx, cancel := setupShutdownHandler()
	defer cancel()

	// Get max concurrent downloads setting (flag or env var via urfave/cli)
	maxConcurrent := ctx.Int("max-concurrent")

	// Initialize all daemon components using shared initialization
	components, err := initDaemonComponents(stdLog, maxConcurrent)
	if err != nil {
		common.PrintRuntimeErr(ctx, "daemon", "init_components", err)
		return nil
	}
	defer components.Close()

	return startServerFunc(components.Server, shutdownCtx)
}
