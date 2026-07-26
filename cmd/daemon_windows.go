//go:build windows

package cmd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/common"
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

// getMaxConcurrentFromEnv reads WARPDL_MAX_CONCURRENT, defaulting to 3.
// Zero means unlimited; negative, malformed, and overflowing values are
// configuration errors rather than an implicit queue-disable switch.
func getMaxConcurrentFromEnv() (int, error) {
	val := os.Getenv("WARPDL_MAX_CONCURRENT")
	if val == "" {
		return 3, nil
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0, fmt.Errorf("invalid WARPDL_MAX_CONCURRENT %q: %w", val, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("WARPDL_MAX_CONCURRENT must be zero or greater")
	}
	return n, nil
}

// getRPCConfigFromEnv reads RPC config from environment variables (used in Windows service mode).
func getRPCConfigFromEnv() *server.RPCConfig {
	listenAll := false
	if val := os.Getenv("WARPDL_RPC_LISTEN_ALL"); val == "1" || val == "true" {
		listenAll = true
	}
	return &server.RPCConfig{
		ListenAll: listenAll,
		Version:   currentBuildArgs.Version,
		Commit:    currentBuildArgs.Commit,
		BuildType: currentBuildArgs.BuildType,
	}
}

// runServiceWithLogger runs the Windows service handler with full daemon functionality.
func runServiceWithLogger(log logger.Logger) error {
	// Read max concurrent from env var (no CLI context in service mode)
	maxConcurrent, err := getMaxConcurrentFromEnv()
	if err != nil {
		log.Error("Invalid service configuration: %v", err)
		return err
	}

	// Build RPC config from env vars (no CLI context in service mode)
	rpcCfg := getRPCConfigFromEnv()

	// Initialize all daemon components using shared initialization
	components, err := initDaemonComponents(log, maxConcurrent, rpcCfg)
	if err != nil {
		log.Error("Failed to initialize daemon components: %v", err)
		return err
	}

	// Create a context for the server that we can cancel on shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server in background
	// Capture function by value to avoid race with test mocks
	startFunc := windowsServerStartFunc
	serverErrCh := make(chan error, 1)
	serverDone := make(chan struct{})
	var (
		serverResultMu sync.Mutex
		serverRunErr   error
	)
	go func() {
		defer close(serverDone)
		err := startFunc(components.Server, ctx)
		serverResultMu.Lock()
		serverRunErr = err
		serverResultMu.Unlock()
		serverErrCh <- err
	}()
	serverResult := func() error {
		serverResultMu.Lock()
		defer serverResultMu.Unlock()
		return serverRunErr
	}

	// Create service handler with full daemon functionality
	var closeOnce sync.Once
	var componentCloseErr error
	closeComponentsFn := func() error {
		closeOnce.Do(func() {
			componentCloseErr = closeDaemonComponentsFn(components)
		})
		return componentCloseErr
	}
	handler := &fullDaemonHandler{
		components:      components,
		logger:          log,
		cancel:          cancel,
		serverErr:       serverErrCh,
		serverDone:      serverDone,
		serverResult:    serverResult,
		closeComponents: closeComponentsFn,
	}

	// svc.Run may return without delivering a stop control (for example when
	// the SCM bridge fails). Cancel and wait for the server/web drain before
	// closing manager, extension, or credential resources they may still use.
	runErr := svcRun(daemonpkg.DefaultServiceName, handler)
	cancel()
	waitErr := waitForWindowsServer(serverDone)
	var serverErr error
	if waitErr == nil {
		serverErr = serverResult()
	}
	serverShutdownErr := errors.Join(waitErr, serverErr)
	if waitErr != nil {
		log.Error("Server shutdown did not finish: %v", waitErr)
	} else if serverErr != nil {
		log.Error("Server shutdown failed: %v", serverErr)
	}
	var closeErr error
	if !errors.Is(serverShutdownErr, server.ErrDrainIncomplete) {
		closeErr = closeComponentsFn()
	}
	return errors.Join(runErr, waitErr, serverErr, closeErr)
}

// fullDaemonHandler implements svc.Handler with full daemon functionality.
// Unlike the previous implementation that used a bare Runner, this handler
// manages all daemon components (cookie manager, extensions, API, server).
type fullDaemonHandler struct {
	components *DaemonComponents
	logger     logger.Logger
	cancel     context.CancelFunc
	serverErr  <-chan error
	serverDone <-chan struct{}
	// serverResult reports the result recorded before serverDone closes.
	// ErrDrainIncomplete means component resources must remain open until
	// process exit; ordinary runtime errors still represent a proven drain.
	serverResult func() error
	// closeComponents is shared with runServiceWithLogger so normal SCM stop
	// and the outer svc.Run return path cannot close the same resources twice.
	closeComponents func() error
}

func (h *fullDaemonHandler) closeDaemonComponents() error {
	if h.closeComponents != nil {
		return h.closeComponents()
	}
	if h.components != nil {
		return h.components.Close()
	}
	return nil
}

func waitForWindowsServer(done <-chan struct{}) error {
	if done == nil {
		return nil
	}
	timer := time.NewTimer(common.ShutdownTimeout)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		select {
		case <-done:
			return nil
		default:
		}
		return fmt.Errorf("%w: timed out waiting for server shutdown", server.ErrDrainIncomplete)
	}
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
		if h.logger != nil {
			if err != nil {
				h.logger.Error("Service failed to start: %v", err)
			} else {
				h.logger.Error("Service server stopped during startup")
			}
		}
		h.cancel()
		status <- svc.Status{State: svc.Stopped}
		return true, 1
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
	for {
		select {
		case err := <-h.serverErr:
			if h.logger != nil {
				if err != nil {
					h.logger.Error("Service server stopped unexpectedly: %v", err)
				} else {
					h.logger.Error("Service server stopped unexpectedly")
				}
			}
			h.cancel()
			status <- svc.Status{State: svc.Stopped}
			return true, 1

		case req, ok := <-requests:
			if !ok {
				return false, 0
			}
			switch req.Cmd {
			case svc.Interrogate:
				status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

			case svc.Stop, svc.Shutdown:
				return h.handleStopRequest(status)
			}
		}
	}
}

// handleStopRequest processes a stop or shutdown command.
func (h *fullDaemonHandler) handleStopRequest(status chan<- svc.Status) (ssec bool, errno uint32) {
	if h.logger != nil {
		h.logger.Info("Service stopping")
	}
	status <- svc.Status{State: svc.StopPending}

	// Cancel context to signal server to stop
	h.cancel()
	if err := waitForWindowsServer(h.serverDone); err != nil {
		if h.logger != nil {
			h.logger.Error("Service server shutdown failed: %v", err)
		}
		status <- svc.Status{State: svc.Stopped}
		return true, 1
	}
	var serverErr error
	if h.serverResult != nil {
		serverErr = h.serverResult()
	}
	if serverErr != nil && h.logger != nil {
		h.logger.Error("Service server shutdown failed: %v", serverErr)
	}
	if errors.Is(serverErr, server.ErrDrainIncomplete) {
		status <- svc.Status{State: svc.Stopped}
		return true, 1
	}

	// Clean up all daemon components. The shared once-guard makes the outer
	// svc.Run return path idempotent with this normal stop path.
	if err := h.closeDaemonComponents(); err != nil {
		if h.logger != nil {
			h.logger.Error("Service cleanup failed: %v", err)
		}
		status <- svc.Status{State: svc.Stopped}
		return true, 1
	}
	if serverErr != nil {
		status <- svc.Status{State: svc.Stopped}
		return true, 1
	}

	if h.logger != nil {
		h.logger.Info("Service stopped")
	}
	status <- svc.Status{State: svc.Stopped}
	return false, 0
}
