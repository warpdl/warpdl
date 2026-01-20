//go:build windows

package cmd

import (
	"context"
	"log"
	"os"
	"strconv"

	"github.com/urfave/cli"
	daemonpkg "github.com/warpdl/warpdl/internal/daemon"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/pkg/logger"
	"golang.org/x/sys/windows/svc"
)

// Test hooks for mocking
var (
	svcIsWindowsService    = svc.IsWindowsService
	svcRun                 = svc.Run
	newEventLogger         = logger.NewEventLogger
	windowsServerStartFunc = func(srv *server.Server, ctx context.Context) error { return srv.Start(ctx) }
)

// getDaemonAction returns the platform-specific daemon action.
// On Windows, this detects service mode and uses Event Log.
func getDaemonAction() cli.ActionFunc {
	return daemonWindows
}

// daemonWindows detects if running as a Windows service and uses the appropriate logger.
// When running as a service, logs go to both console and Windows Event Log.
// When running as a console application, the standard daemon() function is used.
func daemonWindows(ctx *cli.Context) error {
	isService, err := svcIsWindowsService()
	if err != nil {
		return err
	}

	if !isService {
		// Console mode - use existing daemon() function (unchanged behavior)
		return daemon(ctx)
	}

	// Service mode - use Event Log
	return runAsWindowsService()
}

// runAsWindowsService runs the daemon as a Windows service with Event Log integration.
func runAsWindowsService() error {
	stdLogger := logger.NewStandardLogger(log.Default())

	// Attempt to open Event Log
	eventLogger, err := newEventLogger(daemonpkg.DefaultServiceName)
	if err != nil {
		// Fallback: Event Log unavailable (not registered, permissions issue)
		// Use console-only logging
		return runServiceWithLogger(stdLogger)
	}
	defer eventLogger.Close()

	// Multi-backend: Console output + Event Log
	multiLogger := logger.NewMultiLogger(stdLogger, eventLogger)
	return runServiceWithLogger(multiLogger)
}

// getMaxConcurrentFromEnv reads WARPDL_MAX_CONCURRENT env var, defaults to 3.
func getMaxConcurrentFromEnv() int {
	if val := os.Getenv("WARPDL_MAX_CONCURRENT"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			return n
		}
	}
	return 3 // default
}

// runServiceWithLogger runs the Windows service handler with full daemon functionality.
func runServiceWithLogger(log logger.Logger) error {
	// Read max concurrent from env var (no CLI context in service mode)
	maxConcurrent := getMaxConcurrentFromEnv()

	// Initialize all daemon components using shared initialization
	components, err := initDaemonComponents(log, maxConcurrent)
	if err != nil {
		log.Error("Failed to initialize daemon components: %v", err)
		return err
	}

	// Create a context for the server that we can cancel on shutdown
	ctx, cancel := context.WithCancel(context.Background())

	// Start server in background
	// Capture function by value to avoid race with test mocks
	startFunc := windowsServerStartFunc
	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- startFunc(components.Server, ctx)
	}()

	// Create service handler with full daemon functionality
	handler := &fullDaemonHandler{
		components: components,
		logger:     log,
		cancel:     cancel,
		serverErr:  serverErrCh,
	}

	// svc.Run blocks until service stops
	return svcRun(daemonpkg.DefaultServiceName, handler)
}

// fullDaemonHandler implements svc.Handler with full daemon functionality.
// Unlike the previous implementation that used a bare Runner, this handler
// manages all daemon components (cookie manager, extensions, API, server).
type fullDaemonHandler struct {
	components *DaemonComponents
	logger     logger.Logger
	cancel     context.CancelFunc
	serverErr  <-chan error
}

// Execute implements svc.Handler.Execute for Windows service control.
func (h *fullDaemonHandler) Execute(args []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (ssec bool, errno uint32) {
	// args is intentionally unused - WarpDL reads configuration from files
	_ = args

	// Report starting state to SCM
	if h.logger != nil {
		h.logger.Info("Service starting")
	}
	status <- svc.Status{State: svc.StartPending}

	// Check for immediate server start errors
	select {
	case err := <-h.serverErr:
		if err != nil {
			if h.logger != nil {
				h.logger.Error("Service failed to start: %v", err)
			}
			status <- svc.Status{State: svc.Stopped}
			return true, 1
		}
	default:
		// Server starting asynchronously
	}

	// Report running state to SCM
	if h.logger != nil {
		h.logger.Info("Service running")
	}
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	// Process service control requests until stop
	return h.processControlRequests(requests, status)
}

// processControlRequests handles incoming service control requests.
func (h *fullDaemonHandler) processControlRequests(requests <-chan svc.ChangeRequest, status chan<- svc.Status) (ssec bool, errno uint32) {
	for req := range requests {
		switch req.Cmd {
		case svc.Interrogate:
			status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

		case svc.Stop, svc.Shutdown:
			return h.handleStopRequest(status)
		}
	}
	return false, 0
}

// handleStopRequest processes a stop or shutdown command.
func (h *fullDaemonHandler) handleStopRequest(status chan<- svc.Status) (ssec bool, errno uint32) {
	if h.logger != nil {
		h.logger.Info("Service stopping")
	}
	status <- svc.Status{State: svc.StopPending}

	// Cancel context to signal server to stop
	h.cancel()

	// Clean up all daemon components
	if h.components != nil {
		h.components.Close()
	}

	if h.logger != nil {
		h.logger.Info("Service stopped")
	}
	status <- svc.Status{State: svc.Stopped}
	return false, 0
}
