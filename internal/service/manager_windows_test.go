//go:build windows

package service

import (
	"errors"
	"testing"
)

// MockSCManager implements a test double for the Windows Service Control Manager.
type MockSCManager struct {
	services         map[string]*MockService
	openErr          error
	createServiceErr error
	openServiceErr   error
	serviceExistsErr error
}

func NewMockSCManager() *MockSCManager {
	return &MockSCManager{
		services: make(map[string]*MockService),
	}
}

func (m *MockSCManager) OpenService(name string) (ServiceInterface, error) {
	if m.openServiceErr != nil {
		return nil, m.openServiceErr
	}
	svc, ok := m.services[name]
	if !ok {
		return nil, ErrServiceNotFound
	}
	return svc, nil
}

func (m *MockSCManager) CreateService(name, exePath string, config ServiceConfig) (ServiceInterface, error) {
	if m.createServiceErr != nil {
		return nil, m.createServiceErr
	}
	if _, exists := m.services[name]; exists {
		return nil, ErrServiceExists
	}
	svc := &MockService{
		name:        name,
		displayName: config.DisplayName,
		startType:   config.StartType,
		status:      StatusStopped,
	}
	m.services[name] = svc
	return svc, nil
}

func (m *MockSCManager) Close() error {
	return nil
}

// MockService implements a test double for a Windows service.
type MockService struct {
	name         string
	displayName  string
	startType    uint32
	status       ServiceStatus
	startErr     error
	stopErr      error
	deleteErr    error
	statusErr    error
	startCalled  bool
	stopCalled   bool
	deleteCalled bool
}

func (s *MockService) Start() error {
	s.startCalled = true
	if s.startErr != nil {
		return s.startErr
	}
	s.status = StatusRunning
	return nil
}

func (s *MockService) Stop() error {
	s.stopCalled = true
	if s.stopErr != nil {
		return s.stopErr
	}
	s.status = StatusStopped
	return nil
}

func (s *MockService) Delete() error {
	s.deleteCalled = true
	return s.deleteErr
}

func (s *MockService) Status() (ServiceStatus, error) {
	if s.statusErr != nil {
		return 0, s.statusErr
	}
	return s.status, nil
}

func (s *MockService) Close() error {
	return nil
}

// TestServiceStatus_String tests the String() method for ServiceStatus.
func TestServiceStatus_String(t *testing.T) {
	tests := []struct {
		status   ServiceStatus
		expected string
	}{
		{StatusStopped, "Stopped"},
		{StatusStartPending, "Start Pending"},
		{StatusStopPending, "Stop Pending"},
		{StatusRunning, "Running"},
		{StatusContinuePending, "Continue Pending"},
		{StatusPausePending, "Pause Pending"},
		{StatusPaused, "Paused"},
		{ServiceStatus(255), "Unknown (255)"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := tt.status.String()
			if got != tt.expected {
				t.Errorf("ServiceStatus.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestNewServiceManager_CreatesManager tests that NewServiceManager creates a manager.
func TestNewServiceManager_CreatesManager(t *testing.T) {
	mockSCM := NewMockSCManager()
	manager := NewServiceManager(mockSCM)

	if manager == nil {
		t.Fatal("NewServiceManager() returned nil")
	}
	if manager.scm != mockSCM {
		t.Error("NewServiceManager() did not set SCM correctly")
	}
}

// TestServiceManager_Install_CreatesServiceWithCorrectConfig tests that Install()
// creates a service with the correct configuration.
func TestServiceManager_Install_CreatesServiceWithCorrectConfig(t *testing.T) {
	tests := []struct {
		name        string
		serviceName string
		displayName string
		exePath     string
		startType   uint32
	}{
		{
			name:        "default configuration",
			serviceName: "WarpDL",
			displayName: "WarpDL Download Manager",
			exePath:     "C:\\Program Files\\WarpDL\\warpdl.exe",
			startType:   StartTypeAutomatic,
		},
		{
			name:        "manual start type",
			serviceName: "WarpDL",
			displayName: "WarpDL Download Manager",
			exePath:     "C:\\Program Files\\WarpDL\\warpdl.exe",
			startType:   StartTypeManual,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSCM := NewMockSCManager()
			manager := NewServiceManager(mockSCM)

			err := manager.Install(tt.serviceName, tt.displayName, tt.exePath, tt.startType)
			if err != nil {
				t.Fatalf("Install() error = %v", err)
			}

			// Verify service was created
			svc, exists := mockSCM.services[tt.serviceName]
			if !exists {
				t.Fatal("Install() did not create service")
			}

			if svc.displayName != tt.displayName {
				t.Errorf("DisplayName = %q, want %q", svc.displayName, tt.displayName)
			}

			if svc.startType != tt.startType {
				t.Errorf("StartType = %d, want %d", svc.startType, tt.startType)
			}
		})
	}
}

// TestServiceManager_Install_ReturnsErrorIfServiceExists tests that Install()
// returns an error if the service already exists.
func TestServiceManager_Install_ReturnsErrorIfServiceExists(t *testing.T) {
	mockSCM := NewMockSCManager()
	mockSCM.services["WarpDL"] = &MockService{
		name: "WarpDL",
	}
	manager := NewServiceManager(mockSCM)

	err := manager.Install("WarpDL", "WarpDL Download Manager", "C:\\warpdl.exe", StartTypeAutomatic)
	if err == nil {
		t.Fatal("Install() should return error when service exists")
	}
	if !errors.Is(err, ErrServiceExists) {
		t.Errorf("Install() error = %v, want ErrServiceExists", err)
	}
}

// TestServiceManager_Install_ReturnsErrorWhenCreateFails tests error propagation from CreateService.
func TestServiceManager_Install_ReturnsErrorWhenCreateFails(t *testing.T) {
	expectedErr := errors.New("create service failed")
	mockSCM := NewMockSCManager()
	mockSCM.createServiceErr = expectedErr
	manager := NewServiceManager(mockSCM)

	err := manager.Install("WarpDL", "WarpDL Download Manager", "C:\\warpdl.exe", StartTypeAutomatic)
	if err == nil {
		t.Fatal("Install() should return error when CreateService fails")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("Install() error = %v, want %v", err, expectedErr)
	}
}

// TestServiceManager_Uninstall_RemovesService tests that Uninstall() removes the service.
func TestServiceManager_Uninstall_RemovesService(t *testing.T) {
	mockSCM := NewMockSCManager()
	mockSCM.services["WarpDL"] = &MockService{
		name:   "WarpDL",
		status: StatusStopped,
	}
	manager := NewServiceManager(mockSCM)

	err := manager.Uninstall("WarpDL")
	if err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}

	if !mockSCM.services["WarpDL"].deleteCalled {
		t.Error("Uninstall() did not delete service")
	}
}

// TestServiceManager_Uninstall_StopsRunningService tests that Uninstall() stops
// a running service before removing it.
func TestServiceManager_Uninstall_StopsRunningService(t *testing.T) {
	mockSCM := NewMockSCManager()
	mockSCM.services["WarpDL"] = &MockService{
		name:   "WarpDL",
		status: StatusRunning,
	}
	manager := NewServiceManager(mockSCM)

	err := manager.Uninstall("WarpDL")
	if err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}

	svc := mockSCM.services["WarpDL"]
	if !svc.stopCalled {
		t.Error("Uninstall() did not stop running service")
	}
	if !svc.deleteCalled {
		t.Error("Uninstall() did not delete service after stopping")
	}
}

// TestServiceManager_Uninstall_ReturnsErrorIfNotFound tests that Uninstall()
// returns an error if the service does not exist.
func TestServiceManager_Uninstall_ReturnsErrorIfNotFound(t *testing.T) {
	mockSCM := NewMockSCManager()
	manager := NewServiceManager(mockSCM)

	err := manager.Uninstall("NonExistent")
	if err == nil {
		t.Fatal("Uninstall() should return error when service not found")
	}
	if !errors.Is(err, ErrServiceNotFound) {
		t.Errorf("Uninstall() error = %v, want ErrServiceNotFound", err)
	}
}

// TestServiceManager_Uninstall_ReturnsErrorWhenDeleteFails tests error propagation from Delete.
func TestServiceManager_Uninstall_ReturnsErrorWhenDeleteFails(t *testing.T) {
	expectedErr := errors.New("delete failed")
	mockSCM := NewMockSCManager()
	mockSCM.services["WarpDL"] = &MockService{
		name:      "WarpDL",
		status:    StatusStopped,
		deleteErr: expectedErr,
	}
	manager := NewServiceManager(mockSCM)

	err := manager.Uninstall("WarpDL")
	if err == nil {
		t.Fatal("Uninstall() should return error when Delete fails")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("Uninstall() error = %v, want %v", err, expectedErr)
	}
}

// TestServiceManager_Uninstall_ReturnsErrorWhenStatusFails tests error propagation from Status.
func TestServiceManager_Uninstall_ReturnsErrorWhenStatusFails(t *testing.T) {
	expectedErr := errors.New("status query failed")
	mockSCM := NewMockSCManager()
	mockSCM.services["WarpDL"] = &MockService{
		name:      "WarpDL",
		status:    StatusStopped,
		statusErr: expectedErr,
	}
	manager := NewServiceManager(mockSCM)

	err := manager.Uninstall("WarpDL")
	if err == nil {
		t.Fatal("Uninstall() should return error when Status fails")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("Uninstall() error = %v, want %v", err, expectedErr)
	}
}

// TestServiceManager_Uninstall_ReturnsErrorWhenStopFails tests error propagation from Stop.
func TestServiceManager_Uninstall_ReturnsErrorWhenStopFails(t *testing.T) {
	expectedErr := errors.New("stop failed")
	mockSCM := NewMockSCManager()
	mockSCM.services["WarpDL"] = &MockService{
		name:    "WarpDL",
		status:  StatusRunning,
		stopErr: expectedErr,
	}
	manager := NewServiceManager(mockSCM)

	err := manager.Uninstall("WarpDL")
	if err == nil {
		t.Fatal("Uninstall() should return error when Stop fails")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("Uninstall() error = %v, want %v", err, expectedErr)
	}
}

// TestServiceManager_Start_StartsService tests that Start() starts the service.
func TestServiceManager_Start_StartsService(t *testing.T) {
	mockSCM := NewMockSCManager()
	mockSCM.services["WarpDL"] = &MockService{
		name:   "WarpDL",
		status: StatusStopped,
	}
	manager := NewServiceManager(mockSCM)

	err := manager.Start("WarpDL")
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	svc := mockSCM.services["WarpDL"]
	if !svc.startCalled {
		t.Error("Start() did not start service")
	}
	if svc.status != StatusRunning {
		t.Error("Start() did not set status to Running")
	}
}

// TestServiceManager_Start_ReturnsErrorIfAlreadyRunning tests that Start()
// returns an error if the service is already running.
func TestServiceManager_Start_ReturnsErrorIfAlreadyRunning(t *testing.T) {
	mockSCM := NewMockSCManager()
	mockSCM.services["WarpDL"] = &MockService{
		name:   "WarpDL",
		status: StatusRunning,
	}
	manager := NewServiceManager(mockSCM)

	err := manager.Start("WarpDL")
	if err == nil {
		t.Fatal("Start() should return error when service is already running")
	}
	if !errors.Is(err, ErrServiceAlreadyRunning) {
		t.Errorf("Start() error = %v, want ErrServiceAlreadyRunning", err)
	}
}

// TestServiceManager_Start_ReturnsErrorWhenStatusFails tests error propagation from Status.
func TestServiceManager_Start_ReturnsErrorWhenStatusFails(t *testing.T) {
	expectedErr := errors.New("status query failed")
	mockSCM := NewMockSCManager()
	mockSCM.services["WarpDL"] = &MockService{
		name:      "WarpDL",
		status:    StatusStopped,
		statusErr: expectedErr,
	}
	manager := NewServiceManager(mockSCM)

	err := manager.Start("WarpDL")
	if err == nil {
		t.Fatal("Start() should return error when Status fails")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("Start() error = %v, want %v", err, expectedErr)
	}
}

// TestServiceManager_Start_ReturnsErrorWhenStartFails tests error propagation from Start.
func TestServiceManager_Start_ReturnsErrorWhenStartFails(t *testing.T) {
	expectedErr := errors.New("start failed")
	mockSCM := NewMockSCManager()
	mockSCM.services["WarpDL"] = &MockService{
		name:     "WarpDL",
		status:   StatusStopped,
		startErr: expectedErr,
	}
	manager := NewServiceManager(mockSCM)

	err := manager.Start("WarpDL")
	if err == nil {
		t.Fatal("Start() should return error when Start fails")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("Start() error = %v, want %v", err, expectedErr)
	}
}

// TestServiceManager_Start_ReturnsErrorWhenOpenFails tests error propagation from OpenService.
func TestServiceManager_Start_ReturnsErrorWhenOpenFails(t *testing.T) {
	expectedErr := errors.New("open service failed")
	mockSCM := NewMockSCManager()
	mockSCM.openServiceErr = expectedErr
	manager := NewServiceManager(mockSCM)

	err := manager.Start("WarpDL")
	if err == nil {
		t.Fatal("Start() should return error when OpenService fails")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("Start() error = %v, want %v", err, expectedErr)
	}
}

// TestServiceManager_Stop_StopsService tests that Stop() stops the service.
func TestServiceManager_Stop_StopsService(t *testing.T) {
	mockSCM := NewMockSCManager()
	mockSCM.services["WarpDL"] = &MockService{
		name:   "WarpDL",
		status: StatusRunning,
	}
	manager := NewServiceManager(mockSCM)

	err := manager.Stop("WarpDL")
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	svc := mockSCM.services["WarpDL"]
	if !svc.stopCalled {
		t.Error("Stop() did not stop service")
	}
	if svc.status != StatusStopped {
		t.Error("Stop() did not set status to Stopped")
	}
}

// TestServiceManager_Stop_ReturnsErrorIfNotRunning tests that Stop()
// returns an error if the service is not running.
func TestServiceManager_Stop_ReturnsErrorIfNotRunning(t *testing.T) {
	mockSCM := NewMockSCManager()
	mockSCM.services["WarpDL"] = &MockService{
		name:   "WarpDL",
		status: StatusStopped,
	}
	manager := NewServiceManager(mockSCM)

	err := manager.Stop("WarpDL")
	if err == nil {
		t.Fatal("Stop() should return error when service is not running")
	}
	if !errors.Is(err, ErrServiceNotRunning) {
		t.Errorf("Stop() error = %v, want ErrServiceNotRunning", err)
	}
}

// TestServiceManager_Stop_ReturnsErrorWhenStatusFails tests error propagation from Status.
func TestServiceManager_Stop_ReturnsErrorWhenStatusFails(t *testing.T) {
	expectedErr := errors.New("status query failed")
	mockSCM := NewMockSCManager()
	mockSCM.services["WarpDL"] = &MockService{
		name:      "WarpDL",
		status:    StatusRunning,
		statusErr: expectedErr,
	}
	manager := NewServiceManager(mockSCM)

	err := manager.Stop("WarpDL")
	if err == nil {
		t.Fatal("Stop() should return error when Status fails")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("Stop() error = %v, want %v", err, expectedErr)
	}
}

// TestServiceManager_Stop_ReturnsErrorWhenStopFails tests error propagation from Stop.
func TestServiceManager_Stop_ReturnsErrorWhenStopFails(t *testing.T) {
	expectedErr := errors.New("stop failed")
	mockSCM := NewMockSCManager()
	mockSCM.services["WarpDL"] = &MockService{
		name:    "WarpDL",
		status:  StatusRunning,
		stopErr: expectedErr,
	}
	manager := NewServiceManager(mockSCM)

	err := manager.Stop("WarpDL")
	if err == nil {
		t.Fatal("Stop() should return error when Stop fails")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("Stop() error = %v, want %v", err, expectedErr)
	}
}

// TestServiceManager_Stop_ReturnsErrorWhenOpenFails tests error propagation from OpenService.
func TestServiceManager_Stop_ReturnsErrorWhenOpenFails(t *testing.T) {
	expectedErr := errors.New("open service failed")
	mockSCM := NewMockSCManager()
	mockSCM.openServiceErr = expectedErr
	manager := NewServiceManager(mockSCM)

	err := manager.Stop("WarpDL")
	if err == nil {
		t.Fatal("Stop() should return error when OpenService fails")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("Stop() error = %v, want %v", err, expectedErr)
	}
}

// TestServiceManager_Status_ReturnsCorrectStatus tests that Status() returns
// the correct service status.
func TestServiceManager_Status_ReturnsCorrectStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   ServiceStatus
		expected ServiceStatus
	}{
		{
			name:     "running service",
			status:   StatusRunning,
			expected: StatusRunning,
		},
		{
			name:     "stopped service",
			status:   StatusStopped,
			expected: StatusStopped,
		},
		{
			name:     "starting service",
			status:   StatusStartPending,
			expected: StatusStartPending,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockSCM := NewMockSCManager()
			mockSCM.services["WarpDL"] = &MockService{
				name:   "WarpDL",
				status: tt.status,
			}
			manager := NewServiceManager(mockSCM)

			status, err := manager.Status("WarpDL")
			if err != nil {
				t.Fatalf("Status() error = %v", err)
			}
			if status != tt.expected {
				t.Errorf("Status() = %v, want %v", status, tt.expected)
			}
		})
	}
}

// TestServiceManager_Status_ReturnsErrorIfNotFound tests error propagation when service not found.
func TestServiceManager_Status_ReturnsErrorIfNotFound(t *testing.T) {
	mockSCM := NewMockSCManager()
	manager := NewServiceManager(mockSCM)

	_, err := manager.Status("NonExistent")
	if err == nil {
		t.Fatal("Status() should return error when service not found")
	}
	if !errors.Is(err, ErrServiceNotFound) {
		t.Errorf("Status() error = %v, want ErrServiceNotFound", err)
	}
}

// TestServiceManager_Status_ReturnsErrorWhenStatusQueryFails tests error propagation from Status.
func TestServiceManager_Status_ReturnsErrorWhenStatusQueryFails(t *testing.T) {
	expectedErr := errors.New("status query failed")
	mockSCM := NewMockSCManager()
	mockSCM.services["WarpDL"] = &MockService{
		name:      "WarpDL",
		status:    StatusRunning,
		statusErr: expectedErr,
	}
	manager := NewServiceManager(mockSCM)

	_, err := manager.Status("WarpDL")
	if err == nil {
		t.Fatal("Status() should return error when Status query fails")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("Status() error = %v, want %v", err, expectedErr)
	}
}
