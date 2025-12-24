//go:build windows

// Package service provides Windows service integration for WarpDL.
// It implements the Windows Service Control Manager (SCM) interface
// to run the daemon as a Windows service.
package service

import (
	"context"

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
}

// NewWindowsHandler creates a new Windows service handler with the given runner.
func NewWindowsHandler(runner RunnerInterface) *WindowsHandler {
	return &WindowsHandler{
		runner: runner,
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
// Returns service-specific exit code and Windows exit code.
// Both are 0 on successful shutdown, non-zero on error.
func (h *WindowsHandler) Execute(args []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (svcSpecificExitCode uint32, exitCode uint32) {
	// args is intentionally unused - WarpDL reads configuration from files,
	// not from service start arguments. See function documentation above.
	_ = args

	// Report starting state to SCM
	status <- svc.Status{State: svc.StartPending}

	// Create a context for the daemon runner that we can cancel on shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the daemon runner in a goroutine
	startErrCh := make(chan error, 1)
	go func() {
		startErrCh <- h.runner.Start(ctx)
	}()

	// Give the runner a moment to start and check for immediate errors
	select {
	case err := <-startErrCh:
		// Runner returned immediately with an error
		if err != nil {
			return 1, 1
		}
	default:
		// Runner is starting asynchronously, continue
	}

	// Report running state to SCM
	status <- svc.Status{State: svc.Running, Accepts: acceptedCommands}

	// Process service control requests until stop
	return h.processControlRequests(requests, status, cancel)
}

// processControlRequests handles incoming service control requests.
// It runs until a stop or shutdown command is received.
func (h *WindowsHandler) processControlRequests(requests <-chan svc.ChangeRequest, status chan<- svc.Status, cancel context.CancelFunc) (svcSpecificExitCode uint32, exitCode uint32) {
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
	return 0, 0
}

// handleStopRequest processes a stop or shutdown command.
// It gracefully shuts down the runner and reports the stopped state.
func (h *WindowsHandler) handleStopRequest(status chan<- svc.Status, cancel context.CancelFunc) (svcSpecificExitCode uint32, exitCode uint32) {
	status <- svc.Status{State: svc.StopPending}

	// Cancel the context to signal the runner to stop
	cancel()

	// Call Shutdown to perform cleanup
	if err := h.runner.Shutdown(); err != nil {
		// Log the error but still report stopped state
		// The service is stopping regardless of cleanup errors
		status <- svc.Status{State: svc.Stopped}
		return 1, 1
	}

	status <- svc.Status{State: svc.Stopped}
	return 0, 0
}

// AcceptedCommands returns the service commands this handler accepts.
// This is useful for testing and documentation.
func (h *WindowsHandler) AcceptedCommands() svc.Accepted {
	return acceptedCommands
}
