//go:build windows

package service

import (
	"fmt"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// windowsSCManager wraps the Windows Service Control Manager.
// It implements SCManagerInterface for production use.
type windowsSCManager struct {
	mgr *mgr.Mgr
}

// windowsService wraps a Windows service handle.
// It implements ServiceInterface for production use.
type windowsService struct {
	svc *mgr.Service
}

// OpenSCManager opens a connection to the Windows Service Control Manager.
// The caller must call Close() when done.
func OpenSCManager() (SCManagerInterface, error) {
	m, err := mgr.Connect()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to service control manager: %w", err)
	}
	return &windowsSCManager{mgr: m}, nil
}

// OpenService opens an existing service by name.
// Returns ErrServiceNotFound if the service does not exist.
func (m *windowsSCManager) OpenService(name string) (ServiceInterface, error) {
	s, err := m.mgr.OpenService(name)
	if err != nil {
		return nil, fmt.Errorf("failed to open service %q: %w", name, ErrServiceNotFound)
	}
	return &windowsService{svc: s}, nil
}

// CreateService creates a new service with the given configuration.
//
// Note on atomicity: The Windows SCM CreateService API is atomic. If the call
// fails, Windows automatically cleans up any partial state. No manual cleanup
// is required on error paths. This is documented Windows behavior - a service
// either exists completely or not at all after CreateService returns.
func (m *windowsSCManager) CreateService(name, exePath string, config ServiceConfig) (ServiceInterface, error) {
	// Check if service already exists
	existing, err := m.mgr.OpenService(name)
	if err == nil {
		existing.Close()
		return nil, ErrServiceExists
	}

	// Create the service configuration
	svcConfig := mgr.Config{
		DisplayName:  config.DisplayName,
		Description:  config.Description,
		StartType:    config.StartType,
		ServiceType:  windows.SERVICE_WIN32_OWN_PROCESS,
		ErrorControl: windows.SERVICE_ERROR_NORMAL,
	}

	s, err := m.mgr.CreateService(name, exePath, svcConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create service %q: %w", name, err)
	}

	return &windowsService{svc: s}, nil
}

// Close releases the SCM handle.
func (m *windowsSCManager) Close() error {
	return m.mgr.Disconnect()
}

// Start starts the service.
func (s *windowsService) Start() error {
	if err := s.svc.Start(); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}
	return nil
}

// Stop stops the service by sending a stop control signal.
func (s *windowsService) Stop() error {
	_, err := s.svc.Control(svc.Stop)
	if err != nil {
		return fmt.Errorf("failed to stop service: %w", err)
	}
	return nil
}

// Delete removes the service from the system.
func (s *windowsService) Delete() error {
	if err := s.svc.Delete(); err != nil {
		return fmt.Errorf("failed to delete service: %w", err)
	}
	return nil
}

// Status returns the current service status.
func (s *windowsService) Status() (ServiceStatus, error) {
	status, err := s.svc.Query()
	if err != nil {
		return 0, fmt.Errorf("failed to query service status: %w", err)
	}
	return ServiceStatus(status.State), nil
}

// Close releases the service handle.
func (s *windowsService) Close() error {
	return s.svc.Close()
}
