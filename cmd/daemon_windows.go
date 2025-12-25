//go:build windows

package cmd

import (
	"log"

	"github.com/urfave/cli"
	daemonpkg "github.com/warpdl/warpdl/internal/daemon"
	"github.com/warpdl/warpdl/internal/service"
	"github.com/warpdl/warpdl/pkg/logger"
	"golang.org/x/sys/windows/svc"
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
	isService, err := svc.IsWindowsService()
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
	eventLogger, err := logger.NewEventLogger(daemonpkg.DefaultServiceName)
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

// runServiceWithLogger runs the Windows service handler with the given logger.
func runServiceWithLogger(log logger.Logger) error {
	// Create daemon runner with default configuration
	runner := daemonpkg.New(nil, nil)

	// Create service handler with logger
	handler := service.NewWindowsHandler(runner, log)

	// svc.Run blocks until service stops
	return svc.Run(daemonpkg.DefaultServiceName, handler)
}
