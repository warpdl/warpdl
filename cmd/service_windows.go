//go:build windows

package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/urfave/cli"
	daemonpkg "github.com/warpdl/warpdl/internal/daemon"
	"github.com/warpdl/warpdl/internal/service"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc/eventlog"
)

// ErrRequiresAdmin is returned when an operation requires administrator privileges.
var ErrRequiresAdmin = errors.New("this operation requires administrator privileges")

// Dependency injection variables for testing.
// These allow tests to mock service manager operations without Windows API calls.
var (
	isAdminFunc   = isAdmin
	openSCManager = service.OpenSCManager

	// serviceManagerInstallFunc allows tests to mock the Install operation.
	serviceManagerInstallFunc func(serviceName, displayName, exePath string, startType uint32) error

	// serviceManagerUninstallFunc allows tests to mock the Uninstall operation.
	serviceManagerUninstallFunc func(serviceName string) error

	// serviceManagerStatusFunc allows tests to mock the Status operation.
	// Returns the status value and any error.
	serviceManagerStatusFunc func(serviceName string) (uint32, error)
)

// isAdmin checks if the current process has administrator privileges.
// It uses the Windows API to check if the process token is elevated.
func isAdmin() bool {
	var sid *windows.SID

	// Create a SID for the BUILTIN\Administrators group
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid,
	)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)

	// Check if the current process token is a member of the Administrators group
	token := windows.Token(0)
	isMember, err := token.IsMember(sid)
	if err != nil {
		return false
	}

	return isMember
}

// serviceCommand returns the service management command with subcommands.
func serviceCommand() cli.Command {
	return cli.Command{
		Name:  "service",
		Usage: "Manage WarpDL Windows service",
		Subcommands: []cli.Command{
			{
				Name:   "install",
				Usage:  "Install WarpDL as a Windows service",
				Action: serviceInstall,
			},
			{
				Name:   "uninstall",
				Usage:  "Remove the WarpDL Windows service",
				Action: serviceUninstall,
			},
			{
				Name:   "start",
				Usage:  "Start the WarpDL Windows service",
				Action: serviceStart,
			},
			{
				Name:   "stop",
				Usage:  "Stop the WarpDL Windows service",
				Action: serviceStop,
			},
			{
				Name:   "status",
				Usage:  "Show the current status of the WarpDL Windows service",
				Action: serviceStatus,
			},
		},
	}
}

// requireAdmin checks for admin privileges and returns ErrRequiresAdmin if not elevated.
func requireAdmin() error {
	if !isAdminFunc() {
		return ErrRequiresAdmin
	}
	return nil
}

// getServiceManager opens the SCM and creates a ServiceManager.
// Returns the manager, scm handle, and any error.
// Caller is responsible for closing scm when done.
func getServiceManager() (*service.ServiceManager, service.SCManagerInterface, error) {
	scm, err := openSCManager()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to service control manager: %w", err)
	}
	return service.NewServiceManager(scm), scm, nil
}

// serviceInstall installs WarpDL as a Windows service.
func serviceInstall(ctx *cli.Context) error {
	if err := requireAdmin(); err != nil {
		return err
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Check if mock is set for testing
	if serviceManagerInstallFunc != nil {
		return serviceManagerInstallFunc(
			daemonpkg.DefaultServiceName,
			daemonpkg.DefaultDisplayName,
			exePath,
			service.StartTypeAutomatic,
		)
	}

	mgr, scm, err := getServiceManager()
	if err != nil {
		return err
	}
	defer func() {
		if scm != nil {
			scm.Close()
		}
	}()

	err = mgr.Install(
		daemonpkg.DefaultServiceName,
		daemonpkg.DefaultDisplayName,
		exePath,
		service.StartTypeAutomatic,
	)
	if err != nil {
		if errors.Is(err, service.ErrServiceExists) {
			return fmt.Errorf("service '%s' is already installed", daemonpkg.DefaultServiceName)
		}
		return fmt.Errorf("failed to install service: %w", err)
	}

	// Register event source for Windows Event Log
	err = eventlog.InstallAsEventCreate(
		daemonpkg.DefaultServiceName,
		eventlog.Info|eventlog.Warning|eventlog.Error,
	)
	if err != nil {
		// Rollback: uninstall service if event source registration fails
		_ = mgr.Uninstall(daemonpkg.DefaultServiceName)
		return fmt.Errorf("failed to register event source: %w", err)
	}

	fmt.Printf("Service '%s' installed successfully\n", daemonpkg.DefaultServiceName)
	return nil
}

// serviceUninstall removes the WarpDL Windows service.
func serviceUninstall(ctx *cli.Context) error {
	if err := requireAdmin(); err != nil {
		return err
	}

	// Check if mock is set for testing
	if serviceManagerUninstallFunc != nil {
		err := serviceManagerUninstallFunc(daemonpkg.DefaultServiceName)
		if err != nil {
			return err
		}
		fmt.Printf("Service '%s' uninstalled successfully\n", daemonpkg.DefaultServiceName)
		return nil
	}

	mgr, scm, err := getServiceManager()
	if err != nil {
		return err
	}
	defer func() {
		if scm != nil {
			scm.Close()
		}
	}()

	err = mgr.Uninstall(daemonpkg.DefaultServiceName)
	if err != nil {
		if errors.Is(err, service.ErrServiceNotFound) {
			return fmt.Errorf("service '%s' is not installed", daemonpkg.DefaultServiceName)
		}
		return fmt.Errorf("failed to uninstall service: %w", err)
	}

	// Best-effort cleanup of event source (ignore errors)
	_ = eventlog.Remove(daemonpkg.DefaultServiceName)

	fmt.Printf("Service '%s' uninstalled successfully\n", daemonpkg.DefaultServiceName)
	return nil
}

// serviceStart starts the WarpDL Windows service.
func serviceStart(ctx *cli.Context) error {
	if err := requireAdmin(); err != nil {
		return err
	}

	mgr, scm, err := getServiceManager()
	if err != nil {
		return err
	}
	defer func() {
		if scm != nil {
			scm.Close()
		}
	}()

	err = mgr.Start(daemonpkg.DefaultServiceName)
	if err != nil {
		if errors.Is(err, service.ErrServiceNotFound) {
			return fmt.Errorf("service '%s' is not installed", daemonpkg.DefaultServiceName)
		}
		if errors.Is(err, service.ErrServiceAlreadyRunning) {
			return fmt.Errorf("service '%s' is already running", daemonpkg.DefaultServiceName)
		}
		return fmt.Errorf("failed to start service: %w", err)
	}

	fmt.Printf("Service '%s' started successfully\n", daemonpkg.DefaultServiceName)
	return nil
}

// serviceStop stops the WarpDL Windows service.
func serviceStop(ctx *cli.Context) error {
	if err := requireAdmin(); err != nil {
		return err
	}

	mgr, scm, err := getServiceManager()
	if err != nil {
		return err
	}
	defer func() {
		if scm != nil {
			scm.Close()
		}
	}()

	err = mgr.Stop(daemonpkg.DefaultServiceName)
	if err != nil {
		if errors.Is(err, service.ErrServiceNotFound) {
			return fmt.Errorf("service '%s' is not installed", daemonpkg.DefaultServiceName)
		}
		if errors.Is(err, service.ErrServiceNotRunning) {
			return fmt.Errorf("service '%s' is not running", daemonpkg.DefaultServiceName)
		}
		return fmt.Errorf("failed to stop service: %w", err)
	}

	fmt.Printf("Service '%s' stopped successfully\n", daemonpkg.DefaultServiceName)
	return nil
}

// serviceStatus shows the current status of the WarpDL Windows service.
// This command does not require admin privileges.
func serviceStatus(ctx *cli.Context) error {
	// Check if mock is set for testing
	if serviceManagerStatusFunc != nil {
		statusVal, err := serviceManagerStatusFunc(daemonpkg.DefaultServiceName)
		if err != nil {
			return err
		}
		status := service.ServiceStatus(statusVal)
		fmt.Printf("Service '%s': %s\n", daemonpkg.DefaultServiceName, status.String())
		return nil
	}

	mgr, scm, err := getServiceManager()
	if err != nil {
		return err
	}
	defer func() {
		if scm != nil {
			scm.Close()
		}
	}()

	status, err := mgr.Status(daemonpkg.DefaultServiceName)
	if err != nil {
		if errors.Is(err, service.ErrServiceNotFound) {
			return fmt.Errorf("service '%s' is not installed", daemonpkg.DefaultServiceName)
		}
		return fmt.Errorf("failed to get service status: %w", err)
	}

	fmt.Printf("Service '%s': %s\n", daemonpkg.DefaultServiceName, status.String())
	return nil
}
