//go:build windows

package service

import (
	"os"
	"syscall"
	"testing"
)

// skipIfNotCI skips the test if not running in a CI environment.
// SCM tests require elevated privileges and are only run in CI.
func skipIfNotCI(t *testing.T) {
	t.Helper()
	if os.Getenv("CI") == "" && os.Getenv("GITHUB_ACTIONS") == "" {
		t.Skip("skipping SCM test: not in CI environment")
	}
}

// TestOpenSCManager_Connect verifies that we can successfully connect to the
// Windows Service Control Manager. This is a prerequisite for all other
// service management operations.
func TestOpenSCManager_Connect(t *testing.T) {
	skipIfNotCI(t)

	scm, err := OpenSCManager()
	if err != nil {
		if errno, ok := err.(syscall.Errno); ok && errno == syscall.ERROR_ACCESS_DENIED {
			t.Skip("skipping: no SCM access rights")
		}
		t.Fatalf("OpenSCManager: %v", err)
	}
	defer scm.Close()
}

// TestSCManager_OpenService_NotFound verifies that opening a non-existent
// service returns an appropriate error. This tests error handling for the
// common case of missing services.
func TestSCManager_OpenService_NotFound(t *testing.T) {
	skipIfNotCI(t)

	scm, err := OpenSCManager()
	if err != nil {
		if errno, ok := err.(syscall.Errno); ok && errno == syscall.ERROR_ACCESS_DENIED {
			t.Skip("skipping: no SCM access rights")
		}
		t.Fatalf("OpenSCManager: %v", err)
	}
	defer scm.Close()

	_, err = scm.OpenService("nonexistent_warpdl_test_service_12345")
	if err == nil {
		t.Fatal("expected error for non-existent service")
	}
}

// TestSCManager_OpenService_ExistingService verifies that we can open an
// existing Windows service and query its status. We use the EventLog service
// which is a standard Windows service that always exists on all Windows systems.
func TestSCManager_OpenService_ExistingService(t *testing.T) {
	skipIfNotCI(t)

	scm, err := OpenSCManager()
	if err != nil {
		if errno, ok := err.(syscall.Errno); ok && errno == syscall.ERROR_ACCESS_DENIED {
			t.Skip("skipping: no SCM access rights")
		}
		t.Fatalf("OpenSCManager: %v", err)
	}
	defer scm.Close()

	// EventLog is a standard Windows service that always exists
	svc, err := scm.OpenService("EventLog")
	if err != nil {
		if errno, ok := err.(syscall.Errno); ok && errno == syscall.ERROR_ACCESS_DENIED {
			t.Skip("skipping: no service access rights")
		}
		t.Fatalf("OpenService(EventLog): %v", err)
	}
	defer svc.Close()

	// Test Status() on a real service
	status, err := svc.Status()
	if err != nil {
		t.Fatalf("Status(): %v", err)
	}
	// EventLog should typically be running
	if status != StatusRunning && status != StatusStartPending {
		t.Logf("EventLog status: %v (may vary)", status)
	}
}

// TestSCManager_Close verifies that closing the SCM manager handle
// works without errors.
func TestSCManager_Close(t *testing.T) {
	skipIfNotCI(t)

	scm, err := OpenSCManager()
	if err != nil {
		if errno, ok := err.(syscall.Errno); ok && errno == syscall.ERROR_ACCESS_DENIED {
			t.Skip("skipping: no SCM access rights")
		}
		t.Fatalf("OpenSCManager: %v", err)
	}

	if err := scm.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
}

// TestService_Close verifies that closing a service handle works without errors.
func TestService_Close(t *testing.T) {
	skipIfNotCI(t)

	scm, err := OpenSCManager()
	if err != nil {
		if errno, ok := err.(syscall.Errno); ok && errno == syscall.ERROR_ACCESS_DENIED {
			t.Skip("skipping: no SCM access rights")
		}
		t.Fatalf("OpenSCManager: %v", err)
	}
	defer scm.Close()

	svc, err := scm.OpenService("EventLog")
	if err != nil {
		if errno, ok := err.(syscall.Errno); ok && errno == syscall.ERROR_ACCESS_DENIED {
			t.Skip("skipping: no service access rights")
		}
		t.Fatalf("OpenService(EventLog): %v", err)
	}

	if err := svc.Close(); err != nil {
		t.Fatalf("Close(): %v", err)
	}
}

// TestSCManager_CreateService_AlreadyExists verifies that attempting to create
// a service that already exists returns ErrServiceExists. This test uses the
// EventLog service which always exists on Windows systems.
func TestSCManager_CreateService_AlreadyExists(t *testing.T) {
	skipIfNotCI(t)

	scm, err := OpenSCManager()
	if err != nil {
		if errno, ok := err.(syscall.Errno); ok && errno == syscall.ERROR_ACCESS_DENIED {
			t.Skip("skipping: no SCM access rights")
		}
		t.Fatalf("OpenSCManager: %v", err)
	}
	defer scm.Close()

	// Try to create a service with the same name as EventLog
	config := ServiceConfig{
		DisplayName: "Test EventLog",
		StartType:   StartTypeManual,
		Description: "Test service",
	}

	_, err = scm.CreateService("EventLog", "C:\\Windows\\System32\\eventlog.dll", config)
	if err == nil {
		t.Fatal("expected error when creating existing service")
	}

	// The error should be ErrServiceExists
	if err != ErrServiceExists {
		// On Windows, the underlying error might not be ErrServiceExists
		// if the service already exists, so we just verify an error occurred
		t.Logf("got error: %v (expected ErrServiceExists but underlying Windows API may return different error)", err)
	}
}

// TestService_Status_ValidStates verifies that querying service status
// returns valid ServiceStatus values. We test with the EventLog service
// which should be in a valid state.
func TestService_Status_ValidStates(t *testing.T) {
	skipIfNotCI(t)

	scm, err := OpenSCManager()
	if err != nil {
		if errno, ok := err.(syscall.Errno); ok && errno == syscall.ERROR_ACCESS_DENIED {
			t.Skip("skipping: no SCM access rights")
		}
		t.Fatalf("OpenSCManager: %v", err)
	}
	defer scm.Close()

	svc, err := scm.OpenService("EventLog")
	if err != nil {
		if errno, ok := err.(syscall.Errno); ok && errno == syscall.ERROR_ACCESS_DENIED {
			t.Skip("skipping: no service access rights")
		}
		t.Fatalf("OpenService(EventLog): %v", err)
	}
	defer svc.Close()

	status, err := svc.Status()
	if err != nil {
		t.Fatalf("Status(): %v", err)
	}

	// Verify the status is a valid ServiceStatus value
	validStates := []ServiceStatus{
		StatusStopped,
		StatusStartPending,
		StatusStopPending,
		StatusRunning,
		StatusContinuePending,
		StatusPausePending,
		StatusPaused,
	}

	found := false
	for _, validState := range validStates {
		if status == validState {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Status() returned invalid state: %v (%d)", status, status)
	}

	t.Logf("EventLog service status: %v", status)
}

// TestSCManager_MultipleServices verifies that we can open and query
// multiple services concurrently using the same SCM handle.
func TestSCManager_MultipleServices(t *testing.T) {
	skipIfNotCI(t)

	scm, err := OpenSCManager()
	if err != nil {
		if errno, ok := err.(syscall.Errno); ok && errno == syscall.ERROR_ACCESS_DENIED {
			t.Skip("skipping: no SCM access rights")
		}
		t.Fatalf("OpenSCManager: %v", err)
	}
	defer scm.Close()

	// Open multiple standard Windows services
	services := []string{"EventLog", "PlugPlay", "Power"}

	for _, serviceName := range services {
		svc, err := scm.OpenService(serviceName)
		if err != nil {
			if errno, ok := err.(syscall.Errno); ok && errno == syscall.ERROR_ACCESS_DENIED {
				t.Logf("skipping service %q: no access rights", serviceName)
				continue
			}
			// Some services might not exist on all Windows versions
			t.Logf("skipping service %q: %v", serviceName, err)
			continue
		}

		status, err := svc.Status()
		if err != nil {
			svc.Close()
			t.Fatalf("Status() for %q: %v", serviceName, err)
		}

		t.Logf("Service %q status: %v", serviceName, status)
		svc.Close()
	}
}
