//go:build windows

package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/warpdl/warpdl/pkg/logger"
	"golang.org/x/sys/windows/svc"
)

// MockRunner implements a test double for the daemon.Runner interface.
type MockRunner struct {
	mu             sync.Mutex
	startCalled    bool
	shutdownCalled bool
	running        bool
	startErr       error
	shutdownErr    error
}

func (m *MockRunner) Start(ctx context.Context) error {
	m.mu.Lock()
	m.startCalled = true
	if m.startErr != nil {
		err := m.startErr
		m.mu.Unlock()
		return err
	}
	m.running = true
	m.mu.Unlock()

	// Block until context is canceled (simulating real runner behavior)
	<-ctx.Done()

	m.mu.Lock()
	m.running = false
	m.mu.Unlock()
	return ctx.Err()
}

func (m *MockRunner) Shutdown() error {
	m.mu.Lock()
	m.shutdownCalled = true
	m.running = false
	err := m.shutdownErr
	m.mu.Unlock()
	return err
}

func (m *MockRunner) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// waitForState waits for a specific state on the changes channel, returning all states seen.
// Returns states collected and whether the target state was reached before timeout.
func waitForState(t *testing.T, changes <-chan svc.Status, target svc.State, timeout time.Duration) ([]svc.State, bool) {
	t.Helper()
	var states []svc.State
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case status := <-changes:
			states = append(states, status.State)
			if status.State == target {
				return states, true
			}
		case <-timer.C:
			return states, false
		}
	}
}

// TestWindowsHandler_Execute_StateTransitions tests that Execute() transitions
// through the correct states: StartPending -> Running -> StopPending -> Stopped.
func TestWindowsHandler_Execute_StateTransitions(t *testing.T) {
	mock := &MockRunner{}
	handler := NewWindowsHandler(mock, nil)

	changes := make(chan svc.Status, 10)
	requests := make(chan svc.ChangeRequest, 2)

	done := make(chan struct{})
	go func() {
		_, _ = handler.Execute(nil, requests, changes)
		close(done)
	}()

	// Wait for Running state before sending Stop (no arbitrary sleep)
	states, ok := waitForState(t, changes, svc.Running, 500*time.Millisecond)
	if !ok {
		t.Fatal("timeout waiting for Running state")
	}

	// Send stop command
	requests <- svc.ChangeRequest{Cmd: svc.Stop}

	// Collect remaining states until Stopped
	moreStates, ok := waitForState(t, changes, svc.Stopped, 500*time.Millisecond)
	if !ok {
		t.Fatal("timeout waiting for Stopped state")
	}
	states = append(states, moreStates...)

	<-done

	expectedStates := []svc.State{svc.StartPending, svc.Running, svc.StopPending, svc.Stopped}
	if len(states) != len(expectedStates) {
		t.Errorf("got %d state transitions, want %d", len(states), len(expectedStates))
	}
	for i, want := range expectedStates {
		if i >= len(states) {
			t.Errorf("missing state transition %d: want %v", i, want)
			continue
		}
		if states[i] != want {
			t.Errorf("state[%d] = %v, want %v", i, states[i], want)
		}
	}
}

// TestWindowsHandler_Execute_HandlesInterrogate tests that Execute() responds
// to Interrogate commands correctly.
func TestWindowsHandler_Execute_HandlesInterrogate(t *testing.T) {
	mock := &MockRunner{}
	handler := NewWindowsHandler(mock, nil)

	changes := make(chan svc.Status, 10)
	requests := make(chan svc.ChangeRequest, 10)

	done := make(chan struct{})
	go func() {
		_, _ = handler.Execute(nil, requests, changes)
		close(done)
	}()

	// Wait for Running state
	_, ok := waitForState(t, changes, svc.Running, 500*time.Millisecond)
	if !ok {
		t.Fatal("timeout waiting for Running state")
	}

	// Send interrogate (handler responds with current state)
	requests <- svc.ChangeRequest{Cmd: svc.Interrogate}

	// Verify we get Running status again (response to Interrogate)
	states, ok := waitForState(t, changes, svc.Running, 500*time.Millisecond)
	if !ok || len(states) == 0 {
		t.Error("Execute() did not respond to Interrogate command")
	}

	// Send stop
	requests <- svc.ChangeRequest{Cmd: svc.Stop}
	<-done
}

// TestWindowsHandler_Execute_HandlesStop tests that Execute() handles Stop command.
func TestWindowsHandler_Execute_HandlesStop(t *testing.T) {
	mock := &MockRunner{}
	handler := NewWindowsHandler(mock, nil)

	changes := make(chan svc.Status, 10)
	requests := make(chan svc.ChangeRequest, 2)

	done := make(chan struct {
		ssec  bool
		errno uint32
	}, 1)
	go func() {
		ssec, errno := handler.Execute(nil, requests, changes)
		done <- struct {
			ssec  bool
			errno uint32
		}{ssec, errno}
	}()

	// Wait for Running state before sending Stop
	_, ok := waitForState(t, changes, svc.Running, 500*time.Millisecond)
	if !ok {
		t.Fatal("timeout waiting for Running state")
	}

	requests <- svc.ChangeRequest{Cmd: svc.Stop}

	select {
	case result := <-done:
		if result.errno != 0 || result.ssec {
			t.Errorf("Execute() returned unexpected exit codes: ssec=%v, errno=%d", result.ssec, result.errno)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Execute() did not stop on Stop command")
	}

	if !mock.shutdownCalled {
		t.Error("Execute() did not call runner.Shutdown()")
	}
}

// TestWindowsHandler_Execute_StartsRunner tests that Execute() starts the runner.
func TestWindowsHandler_Execute_StartsRunner(t *testing.T) {
	mock := &MockRunner{}
	handler := NewWindowsHandler(mock, nil)

	changes := make(chan svc.Status, 10)
	requests := make(chan svc.ChangeRequest, 2)

	done := make(chan struct{})
	go func() {
		_, _ = handler.Execute(nil, requests, changes)
		close(done)
	}()

	// Wait for Running state (proves Start was called)
	_, ok := waitForState(t, changes, svc.Running, 500*time.Millisecond)
	if !ok {
		t.Fatal("timeout waiting for Running state")
	}

	requests <- svc.ChangeRequest{Cmd: svc.Stop}
	<-done

	if !mock.startCalled {
		t.Error("Execute() did not call runner.Start()")
	}
}

// TestWindowsHandler_Execute_HandlesStartError tests error handling when Start fails.
func TestWindowsHandler_Execute_HandlesStartError(t *testing.T) {
	expectedErr := errors.New("start failed")
	mock := &MockRunner{
		startErr: expectedErr,
	}
	handler := NewWindowsHandler(mock, nil)

	changes := make(chan svc.Status, 10)
	requests := make(chan svc.ChangeRequest, 2)

	// Run Execute in a goroutine with timeout protection
	type result struct {
		ssec  bool
		errno uint32
	}
	done := make(chan result, 1)
	go func() {
		ssec, errno := handler.Execute(nil, requests, changes)
		done <- result{ssec, errno}
	}()

	// Wait for completion with timeout
	select {
	case res := <-done:
		// Should indicate failure (ssec=true means use service-specific exit code)
		if res.errno == 0 && !res.ssec {
			t.Error("Execute() should return non-zero exit code on start failure")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Execute() did not return on start failure (timeout)")
	}

	// Verify state transitions: StartPending -> Stopped
	var statuses []svc.State
	for {
		select {
		case status := <-changes:
			statuses = append(statuses, status.State)
		default:
			goto verify
		}
	}
verify:
	expectedStates := []svc.State{svc.StartPending, svc.Stopped}
	if len(statuses) != len(expectedStates) {
		t.Errorf("got %d state transitions, want %d", len(statuses), len(expectedStates))
	}
	for i, want := range expectedStates {
		if i < len(statuses) && statuses[i] != want {
			t.Errorf("state[%d] = %v, want %v", i, statuses[i], want)
		}
	}
}

// TestWindowsHandler_Execute_HandlesShutdownError tests error handling when Shutdown fails.
func TestWindowsHandler_Execute_HandlesShutdownError(t *testing.T) {
	expectedErr := errors.New("shutdown failed")
	mock := &MockRunner{
		shutdownErr: expectedErr,
	}
	handler := NewWindowsHandler(mock, nil)

	changes := make(chan svc.Status, 10)
	requests := make(chan svc.ChangeRequest, 2)

	done := make(chan struct {
		ssec  bool
		errno uint32
	}, 1)
	go func() {
		ssec, errno := handler.Execute(nil, requests, changes)
		done <- struct {
			ssec  bool
			errno uint32
		}{ssec, errno}
	}()

	// Wait for Running state before sending Stop
	_, ok := waitForState(t, changes, svc.Running, 500*time.Millisecond)
	if !ok {
		t.Fatal("timeout waiting for Running state")
	}

	requests <- svc.ChangeRequest{Cmd: svc.Stop}

	select {
	case result := <-done:
		// Should indicate failure due to shutdown error (ssec=true means use service-specific exit code)
		if result.errno == 0 && !result.ssec {
			t.Error("Execute() should return non-zero exit code on shutdown failure")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Execute() did not complete")
	}
}

// TestWindowsHandler_Execute_HandlesChannelClosure tests that Execute() handles
// unexpected channel closure gracefully.
func TestWindowsHandler_Execute_HandlesChannelClosure(t *testing.T) {
	mock := &MockRunner{}
	handler := NewWindowsHandler(mock, nil)

	changes := make(chan svc.Status, 10)
	requests := make(chan svc.ChangeRequest, 2)

	done := make(chan struct {
		ssec  bool
		errno uint32
	}, 1)
	go func() {
		ssec, errno := handler.Execute(nil, requests, changes)
		done <- struct {
			ssec  bool
			errno uint32
		}{ssec, errno}
	}()

	// Wait for Running state before closing channel
	_, ok := waitForState(t, changes, svc.Running, 500*time.Millisecond)
	if !ok {
		t.Fatal("timeout waiting for Running state")
	}

	// Close requests channel to simulate unexpected closure
	close(requests)

	select {
	case result := <-done:
		// Should return successfully even with channel closure
		if result.errno != 0 || result.ssec {
			t.Errorf("Execute() returned unexpected exit codes: ssec=%v, errno=%d", result.ssec, result.errno)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Execute() did not complete on channel closure")
	}
}

// TestWindowsHandler_Execute_HandlesShutdown tests that Execute() handles Shutdown command.
func TestWindowsHandler_Execute_HandlesShutdown(t *testing.T) {
	mock := &MockRunner{}
	handler := NewWindowsHandler(mock, nil)

	changes := make(chan svc.Status, 10)
	requests := make(chan svc.ChangeRequest, 2)

	done := make(chan struct {
		ssec  bool
		errno uint32
	}, 1)
	go func() {
		ssec, errno := handler.Execute(nil, requests, changes)
		done <- struct {
			ssec  bool
			errno uint32
		}{ssec, errno}
	}()

	// Wait for Running state before sending Shutdown
	states, ok := waitForState(t, changes, svc.Running, 500*time.Millisecond)
	if !ok {
		t.Fatal("timeout waiting for Running state")
	}

	requests <- svc.ChangeRequest{Cmd: svc.Shutdown}

	// Wait for Stopped state
	moreStates, ok := waitForState(t, changes, svc.Stopped, 500*time.Millisecond)
	if !ok {
		t.Fatal("timeout waiting for Stopped state")
	}
	states = append(states, moreStates...)

	select {
	case result := <-done:
		if result.errno != 0 || result.ssec {
			t.Errorf("Execute() returned unexpected exit codes on shutdown: ssec=%v, errno=%d", result.ssec, result.errno)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Execute() did not handle Shutdown command")
	}

	if !mock.shutdownCalled {
		t.Error("Execute() did not call runner.Shutdown() on Shutdown command")
	}

	// Verify state transitions include StopPending and Stopped
	foundStopPending := false
	foundStopped := false
	for _, state := range states {
		if state == svc.StopPending {
			foundStopPending = true
		}
		if state == svc.Stopped {
			foundStopped = true
		}
	}

	if !foundStopPending {
		t.Error("Execute() did not transition to StopPending on Shutdown")
	}
	if !foundStopped {
		t.Error("Execute() did not transition to Stopped on Shutdown")
	}
}

// TestWindowsHandler_Execute_IgnoresUnknownCommands tests that Execute() ignores
// unknown service control commands.
func TestWindowsHandler_Execute_IgnoresUnknownCommands(t *testing.T) {
	mock := &MockRunner{}
	handler := NewWindowsHandler(mock, nil)

	changes := make(chan svc.Status, 10)
	requests := make(chan svc.ChangeRequest, 10)

	done := make(chan struct {
		ssec  bool
		errno uint32
	}, 1)
	go func() {
		ssec, errno := handler.Execute(nil, requests, changes)
		done <- struct {
			ssec  bool
			errno uint32
		}{ssec, errno}
	}()

	// Wait for Running state
	states, ok := waitForState(t, changes, svc.Running, 500*time.Millisecond)
	if !ok {
		t.Fatal("timeout waiting for Running state")
	}

	// Send unknown commands (Pause, Continue, and a completely unknown value)
	requests <- svc.ChangeRequest{Cmd: svc.Pause}
	requests <- svc.ChangeRequest{Cmd: svc.Continue}
	requests <- svc.ChangeRequest{Cmd: svc.Cmd(255)}
	// Finally send Stop
	requests <- svc.ChangeRequest{Cmd: svc.Stop}

	// Wait for Stopped state
	moreStates, ok := waitForState(t, changes, svc.Stopped, 500*time.Millisecond)
	if !ok {
		t.Fatal("timeout waiting for Stopped state")
	}
	states = append(states, moreStates...)

	select {
	case result := <-done:
		if result.errno != 0 || result.ssec {
			t.Errorf("Execute() returned unexpected exit codes: ssec=%v, errno=%d", result.ssec, result.errno)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Execute() did not complete after unknown commands")
	}

	// Verify we never transitioned to Paused or any unexpected state
	for _, state := range states {
		if state == svc.Paused || state == svc.PausePending || state == svc.ContinuePending {
			t.Errorf("Execute() incorrectly processed unknown command, transitioned to %v", state)
		}
	}
}

// TestWindowsHandler_AcceptsCorrectCommands tests accepted command mask.
func TestWindowsHandler_AcceptsCorrectCommands(t *testing.T) {
	handler := NewWindowsHandler(&MockRunner{}, nil)

	accepts := handler.AcceptedCommands()

	expectedAccepts := svc.AcceptStop | svc.AcceptShutdown

	if accepts != expectedAccepts {
		t.Errorf("AcceptedCommands() = %v, want %v", accepts, expectedAccepts)
	}
}

// TestWindowsHandler_LogsLifecycleEvents tests that Execute() logs service lifecycle events.
func TestWindowsHandler_LogsLifecycleEvents(t *testing.T) {
	mock := &MockRunner{}
	mockLog := logger.NewMockLogger()
	handler := NewWindowsHandler(mock, mockLog)

	changes := make(chan svc.Status, 10)
	requests := make(chan svc.ChangeRequest, 2)

	done := make(chan struct{})
	go func() {
		_, _ = handler.Execute(nil, requests, changes)
		close(done)
	}()

	// Wait for Running state
	_, ok := waitForState(t, changes, svc.Running, 500*time.Millisecond)
	if !ok {
		t.Fatal("timeout waiting for Running state")
	}

	// Send stop command
	requests <- svc.ChangeRequest{Cmd: svc.Stop}

	// Wait for completion
	_, ok = waitForState(t, changes, svc.Stopped, 500*time.Millisecond)
	if !ok {
		t.Fatal("timeout waiting for Stopped state")
	}

	<-done

	// Verify lifecycle events were logged
	expectedInfoLogs := []string{
		"Service starting",
		"Service running",
		"Service stopping",
		"Service stopped",
	}

	if len(mockLog.InfoCalls) < len(expectedInfoLogs) {
		t.Errorf("expected at least %d info logs, got %d: %v",
			len(expectedInfoLogs), len(mockLog.InfoCalls), mockLog.InfoCalls)
	}

	for i, expected := range expectedInfoLogs {
		if i >= len(mockLog.InfoCalls) {
			t.Errorf("missing log[%d]: expected %q", i, expected)
			continue
		}
		if mockLog.InfoCalls[i] != expected {
			t.Errorf("log[%d] = %q, want %q", i, mockLog.InfoCalls[i], expected)
		}
	}

	// Verify no errors were logged
	if len(mockLog.ErrorCalls) > 0 {
		t.Errorf("unexpected error logs: %v", mockLog.ErrorCalls)
	}
}

// TestWindowsHandler_LogsStartError tests that Execute() logs startup errors.
func TestWindowsHandler_LogsStartError(t *testing.T) {
	expectedErr := errors.New("start failed")
	mock := &MockRunner{
		startErr: expectedErr,
	}
	mockLog := logger.NewMockLogger()
	handler := NewWindowsHandler(mock, mockLog)

	changes := make(chan svc.Status, 10)
	requests := make(chan svc.ChangeRequest, 2)

	done := make(chan struct{})
	go func() {
		_, _ = handler.Execute(nil, requests, changes)
		close(done)
	}()

	// Wait for completion
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Execute() did not return on start failure")
	}

	// Verify "Service starting" was logged
	if len(mockLog.InfoCalls) == 0 || mockLog.InfoCalls[0] != "Service starting" {
		t.Errorf("expected 'Service starting' log, got: %v", mockLog.InfoCalls)
	}

	// Verify error was logged
	if len(mockLog.ErrorCalls) != 1 {
		t.Errorf("expected 1 error log, got %d: %v", len(mockLog.ErrorCalls), mockLog.ErrorCalls)
	} else if mockLog.ErrorCalls[0] != "Service failed to start: start failed" {
		t.Errorf("error log = %q, want %q", mockLog.ErrorCalls[0], "Service failed to start: start failed")
	}
}

// TestWindowsHandler_LogsShutdownError tests that Execute() logs shutdown errors.
func TestWindowsHandler_LogsShutdownError(t *testing.T) {
	expectedErr := errors.New("shutdown failed")
	mock := &MockRunner{
		shutdownErr: expectedErr,
	}
	mockLog := logger.NewMockLogger()
	handler := NewWindowsHandler(mock, mockLog)

	changes := make(chan svc.Status, 10)
	requests := make(chan svc.ChangeRequest, 2)

	done := make(chan struct{})
	go func() {
		_, _ = handler.Execute(nil, requests, changes)
		close(done)
	}()

	// Wait for Running state
	_, ok := waitForState(t, changes, svc.Running, 500*time.Millisecond)
	if !ok {
		t.Fatal("timeout waiting for Running state")
	}

	// Send stop command
	requests <- svc.ChangeRequest{Cmd: svc.Stop}

	// Wait for completion
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Execute() did not complete")
	}

	// Verify "Service stopping" was logged
	foundStopping := false
	for _, msg := range mockLog.InfoCalls {
		if msg == "Service stopping" {
			foundStopping = true
			break
		}
	}
	if !foundStopping {
		t.Errorf("expected 'Service stopping' log, got: %v", mockLog.InfoCalls)
	}

	// Verify error was logged
	if len(mockLog.ErrorCalls) != 1 {
		t.Errorf("expected 1 error log, got %d: %v", len(mockLog.ErrorCalls), mockLog.ErrorCalls)
	} else if mockLog.ErrorCalls[0] != "Service shutdown error: shutdown failed" {
		t.Errorf("error log = %q, want %q", mockLog.ErrorCalls[0], "Service shutdown error: shutdown failed")
	}
}
