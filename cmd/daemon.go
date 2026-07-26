package cmd

import (
	"context"
	"errors"
	"log"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/logger"
)

var (
	startServerFunc         = func(serv *server.Server, ctx context.Context) error { return serv.Start(ctx) }
	closeDaemonComponentsFn = func(components *DaemonComponents) error { return components.Close() }
)

func daemon(ctx *cli.Context) (err error) {
	stdLog := logger.NewStandardLogger(log.Default())
	maxConcurrent := ctx.Int("max-concurrent")
	if maxConcurrent < 0 {
		return common.PrintRuntimeErr(
			ctx,
			"daemon",
			"max_concurrent",
			errors.New("max-concurrent must be zero or greater"),
		)
	}

	// Clean up stale PID file or fail if daemon already running
	if err := CleanupStalePidFile(); err != nil {
		return common.PrintRuntimeErr(ctx, "daemon", "cleanup_pid", err)
	}

	// Write PID file
	if err := WritePidFile(); err != nil {
		return common.PrintRuntimeErr(ctx, "daemon", "write_pid", err)
	}
	defer RemovePidFile()

	// Setup signal handler for graceful shutdown
	shutdownCtx, cancel := setupShutdownHandler()
	defer cancel()

	// RPC is always enabled. Default bind is 127.0.0.1; --rpc-listen-all
	// is the explicit opt-in to bind on all interfaces.
	rpcCfg := &server.RPCConfig{
		ListenAll: ctx.Bool("rpc-listen-all"),
		Version:   currentBuildArgs.Version,
		Commit:    currentBuildArgs.Commit,
		BuildType: currentBuildArgs.BuildType,
	}

	// Initialize all daemon components using shared initialization
	components, err := initDaemonComponents(stdLog, maxConcurrent, rpcCfg)
	if err != nil {
		return common.PrintRuntimeErr(ctx, "daemon", "init_components", err)
	}
	defer func() {
		if errors.Is(err, server.ErrDrainIncomplete) {
			return
		}
		if closeErr := closeDaemonComponentsFn(components); closeErr != nil {
			err = common.PrintRuntimeErr(
				ctx,
				"daemon",
				"shutdown",
				errors.Join(err, closeErr),
			)
		}
	}()

	return startServerFunc(components.Server, shutdownCtx)
}
