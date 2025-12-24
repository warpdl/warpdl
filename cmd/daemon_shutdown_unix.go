//go:build !windows

package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// setupShutdownHandler sets up signal handling for graceful shutdown.
// It returns a context that is canceled when SIGTERM or SIGINT is received.
func setupShutdownHandler() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-sigChan
		signal.Stop(sigChan) // Unregister handler to prevent leak
		cancel()
	}()

	return ctx, cancel
}
