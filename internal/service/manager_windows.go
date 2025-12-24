//go:build windows

package service

import (
	"errors"
	"fmt"
)

// Sentinel errors for service management operations.
var (
	// ErrServiceExists is returned when trying to install a service that already exists.
	ErrServiceExists = errors.New("service already exists")

	// ErrServiceNotFound is returned when the service is not found.
	ErrServiceNotFound = errors.New("service not found")

	// ErrServiceAlreadyRunning is returned when trying to start a running service.
	ErrServiceAlreadyRunning = errors.New("service is already running")

	// ErrServiceNotRunning is returned when trying to stop a stopped service.
	ErrServiceNotRunning = errors.New("service is not running")
)

// Start type constants for service configuration.
// These match the Windows SERVICE_START_TYPE values.
const (
	// StartTypeAutomatic means the service starts automatically on system boot.
	StartTypeAutomatic uint32 = 2

	// StartTypeManual means the service must be started manually.
	StartTypeManual uint32 = 3

	// StartTypeDisabled means the service cannot be started.
	StartTypeDisabled uint32 = 4
)

// ServiceStatus represents the current state of a Windows service.
// These match the Windows SERVICE_STATUS dwCurrentState values.
type ServiceStatus uint32

// Service status constants matching Windows SERVICE_STATUS values.
const (
	StatusStopped         ServiceStatus = 1
	StatusStartPending    ServiceStatus = 2
	StatusStopPending     ServiceStatus = 3
	StatusRunning         ServiceStatus = 4
	StatusContinuePending ServiceStatus = 5
	StatusPausePending    ServiceStatus = 6
	StatusPaused          ServiceStatus = 7
)

// String returns a human-readable representation of the service status.
func (s ServiceStatus) String() string {
	switch s {
	case StatusStopped:
		return "Stopped"
	case StatusStartPending:
		return "Start Pending"
	case StatusStopPending:
		return "Stop Pending"
	case StatusRunning:
		return "Running"
	case StatusContinuePending:
		return "Continue Pending"
	case StatusPausePending:
		return "Pause Pending"
	case StatusPaused:
		return "Paused"
	default:
		return fmt.Sprintf("Unknown (%d)", s)
	}
}

// ServiceConfig contains the configuration for creating a Windows service.
type ServiceConfig struct {
	// DisplayName is the human-readable name shown in the Services panel.
	DisplayName string

	// StartType determines when/how the service starts (Automatic, Manual, Disabled).
	StartType uint32

	// Description is an optional description of the service.
	Description string
}

// SCManagerInterface defines the interface for interacting with Windows SCM.
// This abstraction allows for testing without actual Windows API calls.
type SCManagerInterface interface {
	// OpenService opens an existing service by name.
	OpenService(name string) (ServiceInterface, error)

	// CreateService creates a new service with the given configuration.
	CreateService(name, exePath string, config ServiceConfig) (ServiceInterface, error)

	// Close releases the SCM handle.
	Close() error
}

// ServiceInterface defines the interface for a Windows service.
// This abstraction allows for testing without actual Windows API calls.
type ServiceInterface interface {
	// Start starts the service.
	Start() error

	// Stop stops the service.
	Stop() error

	// Delete removes the service from the system.
	Delete() error

	// Status returns the current service status.
	Status() (ServiceStatus, error)

	// Close releases the service handle.
	Close() error
}

// ServiceManager manages Windows service lifecycle operations.
// It provides high-level operations for installing, uninstalling,
// starting, stopping, and querying service status.
type ServiceManager struct {
	scm SCManagerInterface
}

// NewServiceManager creates a new service manager with the given SCM interface.
func NewServiceManager(scm SCManagerInterface) *ServiceManager {
	return &ServiceManager{
		scm: scm,
	}
}

// Install creates and registers a new Windows service.
// Returns ErrServiceExists if the service already exists.
func (m *ServiceManager) Install(serviceName, displayName, exePath string, startType uint32) error {
	config := ServiceConfig{
		DisplayName: displayName,
		StartType:   startType,
	}

	svc, err := m.scm.CreateService(serviceName, exePath, config)
	if err != nil {
		return err
	}

	return svc.Close()
}

// Uninstall removes a Windows service.
// If the service is running, it will be stopped first.
// Returns ErrServiceNotFound if the service does not exist.
func (m *ServiceManager) Uninstall(serviceName string) error {
	svc, err := m.scm.OpenService(serviceName)
	if err != nil {
		return err
	}
	defer svc.Close()

	// Stop running service before deletion
	if err := m.stopIfRunning(svc); err != nil {
		return err
	}

	return svc.Delete()
}

// stopIfRunning stops the service if it is currently running.
func (m *ServiceManager) stopIfRunning(svc ServiceInterface) error {
	status, err := svc.Status()
	if err != nil {
		return err
	}

	if status == StatusRunning {
		return svc.Stop()
	}

	return nil
}

// Start starts a Windows service.
// Returns ErrServiceNotFound if the service does not exist.
// Returns ErrServiceAlreadyRunning if the service is already running.
func (m *ServiceManager) Start(serviceName string) error {
	svc, err := m.scm.OpenService(serviceName)
	if err != nil {
		return err
	}
	defer svc.Close()

	status, err := svc.Status()
	if err != nil {
		return err
	}

	if status == StatusRunning {
		return ErrServiceAlreadyRunning
	}

	return svc.Start()
}

// Stop stops a Windows service.
// Returns ErrServiceNotFound if the service does not exist.
// Returns ErrServiceNotRunning if the service is already stopped.
func (m *ServiceManager) Stop(serviceName string) error {
	svc, err := m.scm.OpenService(serviceName)
	if err != nil {
		return err
	}
	defer svc.Close()

	status, err := svc.Status()
	if err != nil {
		return err
	}

	if status == StatusStopped {
		return ErrServiceNotRunning
	}

	return svc.Stop()
}

// Status returns the current status of a Windows service.
// Returns ErrServiceNotFound if the service does not exist.
func (m *ServiceManager) Status(serviceName string) (ServiceStatus, error) {
	svc, err := m.scm.OpenService(serviceName)
	if err != nil {
		return 0, err
	}
	defer svc.Close()

	return svc.Status()
}
