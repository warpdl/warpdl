//go:build windows

package cmd

import (
	"context"
	"os"
	"os/signal"
)

var (
	signalNotify = signal.Notify
	signalStop   = signal.Stop
)

// setupShutdownHandler sets up signal handling for graceful shutdown.
// It returns a context that is canceled when an interrupt signal is received.
// On Windows, syscall.SIGTERM is not available, so we use os.Interrupt only.
func setupShutdownHandler() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	sigChan := make(chan os.Signal, 1)
	signalNotify(sigChan, os.Interrupt)

	go func() {
		<-sigChan
		signalStop(sigChan) // Unregister handler to prevent leak
		cancel()
	}()

	return ctx, cancel
}
