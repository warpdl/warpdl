//go:build windows

// Package service provides Windows service integration for WarpDL.
// It implements the Windows Service Control Manager (SCM) interface
// to run the daemon as a Windows service.
package service

import (
	"context"
	"time"

	"golang.org/x/sys/windows/svc"
)

// Service control constants for accepted commands.
const (
	// acceptedCommands defines which SCM commands the service handles.
	acceptedCommands = svc.AcceptStop | svc.AcceptShutdown
)

// RunnerInterface defines the interface for the daemon runner.
// This allows for dependency injection and mocking in tests.
type RunnerInterface interface {
	// Start begins the daemon with the given context. Returns error if already running.
	Start(ctx context.Context) error

	// Shutdown gracefully stops the daemon.
	Shutdown() error

	// IsRunning returns true if the daemon is currently running.
	IsRunning() bool
}

// WindowsHandler implements svc.Handler for Windows service control.
// It bridges the Windows SCM with the WarpDL daemon runner.
type WindowsHandler struct {
	runner RunnerInterface
	logger EventLogger
}

// NewWindowsHandler creates a new Windows service handler with the given runner.
// If logger is nil, a console logger will be used.
func NewWindowsHandler(runner RunnerInterface) *WindowsHandler {
	return NewWindowsHandlerWithLogger(runner, nil)
}

// NewWindowsHandlerWithLogger creates a new Windows service handler with a custom logger.
// If logger is nil, a console logger will be used.
func NewWindowsHandlerWithLogger(runner RunnerInterface, logger EventLogger) *WindowsHandler {
	if logger == nil {
		logger = NewConsoleEventLogger(nil)
	}
	return &WindowsHandler{
		runner: runner,
		logger: logger,
	}
}

// Execute implements svc.Handler.Execute for Windows service control.
// It manages the service lifecycle including start, stop, and status reporting.
//
// The args parameter contains command-line arguments passed when starting the service.
// WarpDL ignores these because configuration is read from files (~/.config/warpdl/),
// not from CLI arguments. This is intentional - the daemon discovers its configuration
// at runtime from the standard config locations.
//
// The state machine follows the Windows service model:
//
//	StartPending -> Running -> StopPending -> Stopped
//
// Returns svcSpecificEC (always false) and exitCode.
// exitCode is 0 on successful shutdown, non-zero on error.
func (h *WindowsHandler) Execute(args []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (svcSpecificEC bool, exitCode uint32) {
	// args is intentionally unused - WarpDL reads configuration from files,
	// not from service start arguments. See function documentation above.
	_ = args

	// Report starting state to SCM
	status <- svc.Status{State: svc.StartPending}
	_ = h.logger.Info("WarpDL service starting...")

	// Create a context for the daemon runner that we can cancel on shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the daemon runner in a goroutine
	startErrCh := make(chan error, 1)
	go func() {
		startErrCh <- h.runner.Start(ctx)
	}()

	// Give the runner a moment to start and check for immediate errors.
	// We use a short timeout instead of a non-blocking select to ensure
	// the goroutine has time to execute and report any immediate failures.
	// This fixes a race condition where the goroutine might not have started
	// before a non-blocking select would check the channel.
	select {
	case err := <-startErrCh:
		// Runner returned immediately with an error
		if err != nil {
			_ = h.logger.Error("Failed to start WarpDL service: " + err.Error())
			status <- svc.Status{State: svc.Stopped}
			return false, 1
		}
	case <-time.After(50 * time.Millisecond):
		// Runner is starting asynchronously, continue
	}

	// Report running state to SCM
	status <- svc.Status{State: svc.Running, Accepts: acceptedCommands}
	_ = h.logger.Info("WarpDL service started successfully")

	// Process service control requests until stop
	return h.processControlRequests(requests, status, cancel)
}

// processControlRequests handles incoming service control requests.
// It runs until a stop or shutdown command is received.
func (h *WindowsHandler) processControlRequests(requests <-chan svc.ChangeRequest, status chan<- svc.Status, cancel context.CancelFunc) (svcSpecificEC bool, exitCode uint32) {
	for req := range requests {
		switch req.Cmd {
		case svc.Interrogate:
			// Report current status
			status <- svc.Status{State: svc.Running, Accepts: acceptedCommands}

		case svc.Stop, svc.Shutdown:
			return h.handleStopRequest(status, cancel)
		}
	}

	// Channel closed unexpectedly
	return false, 0
}

// handleStopRequest processes a stop or shutdown command.
// It gracefully shuts down the runner and reports the stopped state.
func (h *WindowsHandler) handleStopRequest(status chan<- svc.Status, cancel context.CancelFunc) (svcSpecificEC bool, exitCode uint32) {
	_ = h.logger.Info("WarpDL service stopping...")
	status <- svc.Status{State: svc.StopPending}

	// Cancel the context to signal the runner to stop
	cancel()

	// Call Shutdown to perform cleanup
	if err := h.runner.Shutdown(); err != nil {
		// Log the error but still report stopped state
		// The service is stopping regardless of cleanup errors
		_ = h.logger.Error("Error during service shutdown: " + err.Error())
		status <- svc.Status{State: svc.Stopped}
		return false, 1
	}

	_ = h.logger.Info("WarpDL service stopped successfully")
	status <- svc.Status{State: svc.Stopped}
	return false, 0
}

// AcceptedCommands returns the service commands this handler accepts.
// This is useful for testing and documentation.
func (h *WindowsHandler) AcceptedCommands() svc.Accepted {
	return acceptedCommands
}
