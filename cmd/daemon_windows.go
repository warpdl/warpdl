//go:build windows

package cmd

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/cookiejar"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/internal/api"
	daemonpkg "github.com/warpdl/warpdl/internal/daemon"
	"github.com/warpdl/warpdl/internal/extl"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/internal/service"
	"github.com/warpdl/warpdl/pkg/warplib"
	"golang.org/x/sys/windows/svc"
)

// checkWindowsService checks if we're running as a Windows service.
// If running as service, it initializes and runs the service properly.
// Returns true if running as service, false if running interactively.
func checkWindowsService(ctx *cli.Context) (bool, error) {
	// Check if we're running as a Windows service
	isService, err := svc.IsWindowsService()
	if err != nil {
		return false, fmt.Errorf("failed to determine if running as service: %w", err)
	}

	if !isService {
		// Not running as service, return false to run normal daemon
		return false, nil
	}

	// Running as Windows service - set up event logging
	var eventLogger service.EventLogger
	eventLogger, err = service.NewWindowsEventLogger(daemonpkg.DefaultServiceName)
	if err != nil {
		// If we can't create event logger, fall back to console logger
		eventLogger = service.NewConsoleEventLogger(log.Default())
		_ = eventLogger.Warning(fmt.Sprintf("Failed to create Windows Event Logger, using console: %v", err))
	}
	defer eventLogger.Close()

	_ = eventLogger.Info("Initializing WarpDL service...")

	// Initialize all the components needed for the daemon
	cm, err := cookieManagerFunc(ctx)
	if err != nil {
		_ = eventLogger.Error(fmt.Sprintf("Failed to initialize cookie manager: %v", err))
		return true, err
	}
	defer cm.Close()

	l := log.Default()
	elEng, err := extl.NewEngine(l, cm, false)
	if err != nil {
		_ = eventLogger.Error(fmt.Sprintf("Failed to initialize extension engine: %v", err))
		common.PrintRuntimeErr(ctx, "daemon", "extloader_engine", err)
		return true, err
	}
	defer elEng.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		_ = eventLogger.Error(fmt.Sprintf("Failed to create cookie jar: %v", err))
		common.PrintRuntimeErr(ctx, "daemon", "cookie_jar", err)
		return true, err
	}

	client := &http.Client{Jar: jar}

	m, err := warplib.InitManager()
	if err != nil {
		_ = eventLogger.Error(fmt.Sprintf("Failed to initialize manager: %v", err))
		common.PrintRuntimeErr(ctx, "daemon", "init_manager", err)
		return true, err
	}

	s, err := api.NewApi(l, m, client, elEng, currentBuildArgs.Version, currentBuildArgs.Commit, currentBuildArgs.BuildType)
	if err != nil {
		_ = eventLogger.Error(fmt.Sprintf("Failed to create API: %v", err))
		common.PrintRuntimeErr(ctx, "daemon", "new_api", err)
		return true, err
	}

	serv := server.NewServer(l, m, DEF_PORT)
	s.RegisterHandlers(serv)

	// Create a custom runner that wraps the server
	runner := &serviceRunner{
		server:      serv,
		eventLogger: eventLogger,
		apiService:  s,
		manager:     m,
	}

	// Create Windows service handler with event logger
	handler := service.NewWindowsHandlerWithLogger(runner, eventLogger)

	// Run as Windows service
	err = svc.Run(daemonpkg.DefaultServiceName, handler)
	if err != nil {
		_ = eventLogger.Error(fmt.Sprintf("Service failed: %v", err))
		return true, fmt.Errorf("failed to run service: %w", err)
	}

	return true, nil
}

// serviceRunner implements the RunnerInterface for the Windows service.
// It wraps the actual server Start method.
type serviceRunner struct {
	server      *server.Server
	eventLogger service.EventLogger
	apiService  *api.Api
	manager     *warplib.Manager
	running     bool
}

func (r *serviceRunner) Start(ctx context.Context) error {
	r.running = true
	defer func() { r.running = false }()
	
	_ = r.eventLogger.Info("Starting daemon server...")
	err := r.server.Start(ctx)
	if err != nil && err != context.Canceled {
		_ = r.eventLogger.Error(fmt.Sprintf("Server error: %v", err))
	}
	return err
}

func (r *serviceRunner) Shutdown() error {
	_ = r.eventLogger.Info("Shutting down daemon server...")
	
	// Stop all active downloads
	for _, item := range r.manager.GetItems() {
		if item.IsDownloading() {
			_ = r.eventLogger.Info(fmt.Sprintf("Stopping download: %s", item.Hash))
			item.StopDownload()
		}
	}

	// Close API
	if err := r.apiService.Close(); err != nil {
		_ = r.eventLogger.Warning(fmt.Sprintf("Error closing API: %v", err))
	}

	return nil
}

func (r *serviceRunner) IsRunning() bool {
	return r.running
}

