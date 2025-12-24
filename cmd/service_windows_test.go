//go:build windows

package cmd

import (
	"errors"
	"testing"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/internal/service"
)

// TestServiceCommand_ReturnsCorrectSubcommands tests that serviceCommand()
// returns a command with the correct subcommands.
func TestServiceCommand_ReturnsCorrectSubcommands(t *testing.T) {
	cmd := serviceCommand()

	if cmd.Name != "service" {
		t.Errorf("Name = %q, want %q", cmd.Name, "service")
	}

	expectedSubcommands := []string{"install", "uninstall", "start", "stop", "status"}
	subcommandNames := make(map[string]bool)

	for _, subcmd := range cmd.Subcommands {
		subcommandNames[subcmd.Name] = true
	}

	for _, expected := range expectedSubcommands {
		if !subcommandNames[expected] {
			t.Errorf("missing subcommand %q", expected)
		}
	}
}

// TestServiceInstall_ActionExists tests that the install subcommand has an action.
func TestServiceInstall_ActionExists(t *testing.T) {
	cmd := serviceCommand()

	var installCmd *cli.Command
	for i := range cmd.Subcommands {
		if cmd.Subcommands[i].Name == "install" {
			installCmd = &cmd.Subcommands[i]
			break
		}
	}

	if installCmd == nil {
		t.Fatal("install subcommand not found")
	}

	if installCmd.Action == nil {
		t.Error("install subcommand has no action")
	}
}

// TestServiceUninstall_ActionExists tests that the uninstall subcommand has an action.
func TestServiceUninstall_ActionExists(t *testing.T) {
	cmd := serviceCommand()

	var uninstallCmd *cli.Command
	for i := range cmd.Subcommands {
		if cmd.Subcommands[i].Name == "uninstall" {
			uninstallCmd = &cmd.Subcommands[i]
			break
		}
	}

	if uninstallCmd == nil {
		t.Fatal("uninstall subcommand not found")
	}

	if uninstallCmd.Action == nil {
		t.Error("uninstall subcommand has no action")
	}
}

// TestServiceStart_ActionExists tests that the start subcommand has an action.
func TestServiceStart_ActionExists(t *testing.T) {
	cmd := serviceCommand()

	var startCmd *cli.Command
	for i := range cmd.Subcommands {
		if cmd.Subcommands[i].Name == "start" {
			startCmd = &cmd.Subcommands[i]
			break
		}
	}

	if startCmd == nil {
		t.Fatal("start subcommand not found")
	}

	if startCmd.Action == nil {
		t.Error("start subcommand has no action")
	}
}

// TestServiceStop_ActionExists tests that the stop subcommand has an action.
func TestServiceStop_ActionExists(t *testing.T) {
	cmd := serviceCommand()

	var stopCmd *cli.Command
	for i := range cmd.Subcommands {
		if cmd.Subcommands[i].Name == "stop" {
			stopCmd = &cmd.Subcommands[i]
			break
		}
	}

	if stopCmd == nil {
		t.Fatal("stop subcommand not found")
	}

	if stopCmd.Action == nil {
		t.Error("stop subcommand has no action")
	}
}

// TestServiceStatus_ActionExists tests that the status subcommand has an action.
func TestServiceStatus_ActionExists(t *testing.T) {
	cmd := serviceCommand()

	var statusCmd *cli.Command
	for i := range cmd.Subcommands {
		if cmd.Subcommands[i].Name == "status" {
			statusCmd = &cmd.Subcommands[i]
			break
		}
	}

	if statusCmd == nil {
		t.Fatal("status subcommand not found")
	}

	if statusCmd.Action == nil {
		t.Error("status subcommand has no action")
	}
}

// TestServiceInstall_RequiresAdmin tests that install returns error for non-admin.
func TestServiceInstall_RequiresAdmin(t *testing.T) {
	// Mock the admin check function
	oldIsAdmin := isAdminFunc
	isAdminFunc = func() bool { return false }
	defer func() { isAdminFunc = oldIsAdmin }()

	ctx := newContext(cli.NewApp(), nil, "install")
	err := serviceInstall(ctx)

	if err == nil {
		t.Error("serviceInstall() should return error for non-admin")
	}
	if !errors.Is(err, ErrRequiresAdmin) {
		t.Errorf("serviceInstall() error = %v, want ErrRequiresAdmin", err)
	}
}

// TestServiceUninstall_RequiresAdmin tests that uninstall returns error for non-admin.
func TestServiceUninstall_RequiresAdmin(t *testing.T) {
	oldIsAdmin := isAdminFunc
	isAdminFunc = func() bool { return false }
	defer func() { isAdminFunc = oldIsAdmin }()

	ctx := newContext(cli.NewApp(), nil, "uninstall")
	err := serviceUninstall(ctx)

	if err == nil {
		t.Error("serviceUninstall() should return error for non-admin")
	}
	if !errors.Is(err, ErrRequiresAdmin) {
		t.Errorf("serviceUninstall() error = %v, want ErrRequiresAdmin", err)
	}
}

// TestServiceStart_RequiresAdmin tests that start returns error for non-admin.
func TestServiceStart_RequiresAdmin(t *testing.T) {
	oldIsAdmin := isAdminFunc
	isAdminFunc = func() bool { return false }
	defer func() { isAdminFunc = oldIsAdmin }()

	ctx := newContext(cli.NewApp(), nil, "start")
	err := serviceStart(ctx)

	if err == nil {
		t.Error("serviceStart() should return error for non-admin")
	}
	if !errors.Is(err, ErrRequiresAdmin) {
		t.Errorf("serviceStart() error = %v, want ErrRequiresAdmin", err)
	}
}

// TestServiceStop_RequiresAdmin tests that stop returns error for non-admin.
func TestServiceStop_RequiresAdmin(t *testing.T) {
	oldIsAdmin := isAdminFunc
	isAdminFunc = func() bool { return false }
	defer func() { isAdminFunc = oldIsAdmin }()

	ctx := newContext(cli.NewApp(), nil, "stop")
	err := serviceStop(ctx)

	if err == nil {
		t.Error("serviceStop() should return error for non-admin")
	}
	if !errors.Is(err, ErrRequiresAdmin) {
		t.Errorf("serviceStop() error = %v, want ErrRequiresAdmin", err)
	}
}

// TestServiceInstall_SuccessWithAdmin tests successful install with admin privileges.
func TestServiceInstall_SuccessWithAdmin(t *testing.T) {
	oldIsAdmin := isAdminFunc
	oldInstall := serviceManagerInstallFunc
	isAdminFunc = func() bool { return true }
	serviceManagerInstallFunc = func(serviceName, displayName, exePath string, startType uint32) error {
		return nil
	}
	defer func() {
		isAdminFunc = oldIsAdmin
		serviceManagerInstallFunc = oldInstall
	}()

	ctx := newContext(cli.NewApp(), nil, "install")
	err := serviceInstall(ctx)

	if err != nil {
		t.Errorf("serviceInstall() error = %v, want nil", err)
	}
}

// TestServiceUninstall_SuccessWithAdmin tests successful uninstall with admin privileges.
func TestServiceUninstall_SuccessWithAdmin(t *testing.T) {
	oldIsAdmin := isAdminFunc
	oldUninstall := serviceManagerUninstallFunc
	isAdminFunc = func() bool { return true }
	serviceManagerUninstallFunc = func(serviceName string) error {
		return nil
	}
	defer func() {
		isAdminFunc = oldIsAdmin
		serviceManagerUninstallFunc = oldUninstall
	}()

	ctx := newContext(cli.NewApp(), nil, "uninstall")
	err := serviceUninstall(ctx)

	if err != nil {
		t.Errorf("serviceUninstall() error = %v, want nil", err)
	}
}

// TestServiceCommand_HasCorrectUsage tests that service command has usage text.
func TestServiceCommand_HasCorrectUsage(t *testing.T) {
	cmd := serviceCommand()

	if cmd.Usage == "" {
		t.Error("service command has no usage text")
	}

	for _, subcmd := range cmd.Subcommands {
		if subcmd.Usage == "" {
			t.Errorf("subcommand %q has no usage text", subcmd.Name)
		}
	}
}

// TestServiceStatus_NoAdminRequired tests that status does not require admin.
func TestServiceStatus_NoAdminRequired(t *testing.T) {
	oldIsAdmin := isAdminFunc
	oldStatus := serviceManagerStatusFunc
	isAdminFunc = func() bool { return false }
	statusCalled := false
	serviceManagerStatusFunc = func(serviceName string) (uint32, error) {
		statusCalled = true
		return 1, nil // StatusStopped
	}
	defer func() {
		isAdminFunc = oldIsAdmin
		serviceManagerStatusFunc = oldStatus
	}()

	ctx := newContext(cli.NewApp(), nil, "status")
	_ = serviceStatus(ctx)

	if !statusCalled {
		t.Error("serviceStatus() did not check service status")
	}
}

// TestGetServiceManager_Success tests successful opening of service manager.
func TestGetServiceManager_Success(t *testing.T) {
	oldOpenSCManager := openSCManager
	mockSCM := &mockSCManagerInterface{}
	openSCManager = func() (service.SCManagerInterface, error) {
		return mockSCM, nil
	}
	defer func() { openSCManager = oldOpenSCManager }()

	mgr, scm, err := getServiceManager()

	if err != nil {
		t.Errorf("getServiceManager() error = %v, want nil", err)
	}
	if mgr == nil {
		t.Error("getServiceManager() returned nil manager")
	}
	if scm == nil {
		t.Error("getServiceManager() returned nil SCM")
	}
}

// TestGetServiceManager_OpenError tests error handling when opening SCM fails.
func TestGetServiceManager_OpenError(t *testing.T) {
	oldOpenSCManager := openSCManager
	expectedErr := errors.New("mock SCM open error")
	openSCManager = func() (service.SCManagerInterface, error) {
		return nil, expectedErr
	}
	defer func() { openSCManager = oldOpenSCManager }()

	mgr, scm, err := getServiceManager()

	if err == nil {
		t.Error("getServiceManager() should return error when openSCManager fails")
	}
	if mgr != nil {
		t.Error("getServiceManager() should return nil manager on error")
	}
	if scm != nil {
		t.Error("getServiceManager() should return nil SCM on error")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("getServiceManager() error does not wrap original error")
	}
	// Verify error message contains "service control manager"
	if err != nil && err.Error() == "" {
		t.Error("getServiceManager() error message is empty")
	}
	if err != nil {
		errMsg := err.Error()
		if !contains(errMsg, "service control manager") {
			t.Errorf("getServiceManager() error message = %q, should contain 'service control manager'", errMsg)
		}
	}
}

// TestServiceStart_RequiresAdminFirst tests that start checks admin before calling SCM.
func TestServiceStart_RequiresAdminFirst(t *testing.T) {
	oldIsAdmin := isAdminFunc
	oldOpenSCManager := openSCManager
	scmCalled := false
	isAdminFunc = func() bool { return false }
	openSCManager = func() (service.SCManagerInterface, error) {
		scmCalled = true
		return nil, errors.New("should not be called")
	}
	defer func() {
		isAdminFunc = oldIsAdmin
		openSCManager = oldOpenSCManager
	}()

	ctx := newContext(cli.NewApp(), nil, "start")
	err := serviceStart(ctx)

	if !errors.Is(err, ErrRequiresAdmin) {
		t.Errorf("serviceStart() error = %v, want ErrRequiresAdmin", err)
	}
	if scmCalled {
		t.Error("serviceStart() should not call openSCManager when not admin")
	}
}

// TestServiceStart_GetServiceManagerError tests start handles getServiceManager error.
func TestServiceStart_GetServiceManagerError(t *testing.T) {
	oldIsAdmin := isAdminFunc
	oldOpenSCManager := openSCManager
	expectedErr := errors.New("mock SCM error")
	isAdminFunc = func() bool { return true }
	openSCManager = func() (service.SCManagerInterface, error) {
		return nil, expectedErr
	}
	defer func() {
		isAdminFunc = oldIsAdmin
		openSCManager = oldOpenSCManager
	}()

	ctx := newContext(cli.NewApp(), nil, "start")
	err := serviceStart(ctx)

	if err == nil {
		t.Error("serviceStart() should return error when getServiceManager fails")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("serviceStart() error does not wrap getServiceManager error")
	}
}

// TestServiceStop_RequiresAdminFirst tests that stop checks admin before calling SCM.
func TestServiceStop_RequiresAdminFirst(t *testing.T) {
	oldIsAdmin := isAdminFunc
	oldOpenSCManager := openSCManager
	scmCalled := false
	isAdminFunc = func() bool { return false }
	openSCManager = func() (service.SCManagerInterface, error) {
		scmCalled = true
		return nil, errors.New("should not be called")
	}
	defer func() {
		isAdminFunc = oldIsAdmin
		openSCManager = oldOpenSCManager
	}()

	ctx := newContext(cli.NewApp(), nil, "stop")
	err := serviceStop(ctx)

	if !errors.Is(err, ErrRequiresAdmin) {
		t.Errorf("serviceStop() error = %v, want ErrRequiresAdmin", err)
	}
	if scmCalled {
		t.Error("serviceStop() should not call openSCManager when not admin")
	}
}

// TestServiceStop_GetServiceManagerError tests stop handles getServiceManager error.
func TestServiceStop_GetServiceManagerError(t *testing.T) {
	oldIsAdmin := isAdminFunc
	oldOpenSCManager := openSCManager
	expectedErr := errors.New("mock SCM error")
	isAdminFunc = func() bool { return true }
	openSCManager = func() (service.SCManagerInterface, error) {
		return nil, expectedErr
	}
	defer func() {
		isAdminFunc = oldIsAdmin
		openSCManager = oldOpenSCManager
	}()

	ctx := newContext(cli.NewApp(), nil, "stop")
	err := serviceStop(ctx)

	if err == nil {
		t.Error("serviceStop() should return error when getServiceManager fails")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("serviceStop() error does not wrap getServiceManager error")
	}
}

// TestServiceStatus_GetServiceManagerError tests status handles getServiceManager error.
func TestServiceStatus_GetServiceManagerError(t *testing.T) {
	oldOpenSCManager := openSCManager
	oldStatus := serviceManagerStatusFunc
	expectedErr := errors.New("mock SCM error")
	// Clear the status func mock to ensure we go through getServiceManager
	serviceManagerStatusFunc = nil
	openSCManager = func() (service.SCManagerInterface, error) {
		return nil, expectedErr
	}
	defer func() {
		openSCManager = oldOpenSCManager
		serviceManagerStatusFunc = oldStatus
	}()

	ctx := newContext(cli.NewApp(), nil, "status")
	err := serviceStatus(ctx)

	if err == nil {
		t.Error("serviceStatus() should return error when getServiceManager fails")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("serviceStatus() error does not wrap getServiceManager error")
	}
}

// mockSCManagerInterface is a mock implementation of SCManagerInterface for testing.
type mockSCManagerInterface struct{}

func (m *mockSCManagerInterface) OpenService(name string) (service.ServiceInterface, error) {
	return nil, errors.New("not implemented")
}

func (m *mockSCManagerInterface) CreateService(name, exePath string, config service.ServiceConfig) (service.ServiceInterface, error) {
	return nil, errors.New("not implemented")
}

func (m *mockSCManagerInterface) Close() error {
	return nil
}

// contains checks if a string contains a substring (case-sensitive).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || indexOfSubstring(s, substr) >= 0)
}

// indexOfSubstring returns the index of substr in s, or -1 if not found.
func indexOfSubstring(s, substr string) int {
	n := len(substr)
	if n == 0 {
		return 0
	}
	for i := 0; i+n <= len(s); i++ {
		if s[i:i+n] == substr {
			return i
		}
	}
	return -1
}
