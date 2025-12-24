//go:build windows

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/sys/windows/svc"
)

// MockRunner implements a test double for the daemon.Runner interface.
type MockRunner struct {
	startCalled    bool
	shutdownCalled bool
	running        bool
	startErr       error
	shutdownErr    error
}

func (m *MockRunner) Start(ctx context.Context) error {
	m.startCalled = true
	if m.startErr != nil {
		return m.startErr
	}
	m.running = true
	// Block until context is canceled (simulating real runner behavior)
	<-ctx.Done()
	m.running = false
	return ctx.Err()
}

func (m *MockRunner) Shutdown() error {
	m.shutdownCalled = true
	m.running = false
	return m.shutdownErr
}

func (m *MockRunner) IsRunning() bool {
	return m.running
}

// TestWindowsHandler_Execute_StateTransitions tests that Execute() transitions
// through the correct states: StartPending -> Running -> StopPending -> Stopped.
func TestWindowsHandler_Execute_StateTransitions(t *testing.T) {
	mock := &MockRunner{}
	handler := NewWindowsHandler(mock)

	// Create channels for simulating service control
	changes := make(chan svc.Status, 10)
	requests := make(chan svc.ChangeRequest, 2)

	// Send stop after handler starts
	go func() {
		time.Sleep(100 * time.Millisecond)
		requests <- svc.ChangeRequest{Cmd: svc.Stop}
	}()

	done := make(chan struct{})
	go func() {
		_, _ = handler.Execute(nil, requests, changes)
		close(done)
	}()

	// Collect status transitions.
	// We don't use the done channel in the select to avoid a race condition
	// where done might be selected before we collect the final Stopped status.
	var statuses []svc.State
	timeout := time.After(2 * time.Second)

collectLoop:
	for {
		select {
		case status := <-changes:
			statuses = append(statuses, status.State)
			if status.State == svc.Stopped {
				break collectLoop
			}
		case <-timeout:
			t.Fatal("timeout waiting for status transitions")
		}
	}

	// Wait for handler to finish
	<-done

	// Verify state transitions
	expectedStates := []svc.State{
		svc.StartPending,
		svc.Running,
		svc.StopPending,
		svc.Stopped,
	}

	if len(statuses) != len(expectedStates) {
		t.Errorf("got %d state transitions, want %d", len(statuses), len(expectedStates))
	}

	for i, want := range expectedStates {
		if i >= len(statuses) {
			t.Errorf("missing state transition %d: want %v", i, want)
			continue
		}
		if statuses[i] != want {
			t.Errorf("state[%d] = %v, want %v", i, statuses[i], want)
		}
	}
}

// TestWindowsHandler_Execute_HandlesInterrogate tests that Execute() responds
// to Interrogate commands correctly.
func TestWindowsHandler_Execute_HandlesInterrogate(t *testing.T) {
	mock := &MockRunner{}
	handler := NewWindowsHandler(mock)

	changes := make(chan svc.Status, 10)
	requests := make(chan svc.ChangeRequest, 10)

	// Send interrogate then stop
	go func() {
		time.Sleep(50 * time.Millisecond)
		requests <- svc.ChangeRequest{Cmd: svc.Interrogate}
		time.Sleep(50 * time.Millisecond)
		requests <- svc.ChangeRequest{Cmd: svc.Stop}
	}()

	done := make(chan struct{})
	go func() {
		_, _ = handler.Execute(nil, requests, changes)
		close(done)
	}()

	interrogateReceived := false
	timeout := time.After(2 * time.Second)

collectLoop:
	for {
		select {
		case status := <-changes:
			// After Running, Interrogate should report Running again
			if status.State == svc.Running {
				interrogateReceived = true
			}
			if status.State == svc.Stopped {
				break collectLoop
			}
		case <-timeout:
			t.Fatal("timeout waiting for interrogate response")
		case <-done:
			break collectLoop
		}
	}

	if !interrogateReceived {
		t.Error("Execute() did not handle Interrogate command")
	}
}

// TestWindowsHandler_Execute_HandlesStop tests that Execute() handles Stop command.
func TestWindowsHandler_Execute_HandlesStop(t *testing.T) {
	mock := &MockRunner{}
	handler := NewWindowsHandler(mock)

	changes := make(chan svc.Status, 10)
	requests := make(chan svc.ChangeRequest, 2)

	// Send immediate stop
	go func() {
		time.Sleep(50 * time.Millisecond)
		requests <- svc.ChangeRequest{Cmd: svc.Stop}
	}()

	done := make(chan error, 1)
	go func() {
		_, exitCode := handler.Execute(nil, requests, changes)
		done <- nil
		if exitCode != 0 {
			t.Errorf("Execute() returned exit code %d, want 0", exitCode)
		}
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Execute() did not stop on Stop command")
	}

	if !mock.shutdownCalled {
		t.Error("Execute() did not call runner.Shutdown()")
	}
}

// TestWindowsHandler_Execute_StartsRunner tests that Execute() starts the runner.
func TestWindowsHandler_Execute_StartsRunner(t *testing.T) {
	mock := &MockRunner{}
	handler := NewWindowsHandler(mock)

	changes := make(chan svc.Status, 10)
	requests := make(chan svc.ChangeRequest, 2)

	go func() {
		time.Sleep(50 * time.Millisecond)
		requests <- svc.ChangeRequest{Cmd: svc.Stop}
	}()

	done := make(chan struct{})
	go func() {
		_, _ = handler.Execute(nil, requests, changes)
		close(done)
	}()

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
	handler := NewWindowsHandler(mock)

	changes := make(chan svc.Status, 10)
	requests := make(chan svc.ChangeRequest, 2)

	// Run Execute in a goroutine with timeout protection
	type result struct {
		svcCode  uint32
		exitCode uint32
	}
	done := make(chan result, 1)
	go func() {
		svc, exit := handler.Execute(nil, requests, changes)
		done <- result{svc, exit}
	}()

	// Wait for completion with timeout
	select {
	case res := <-done:
		// Should indicate failure
		if res.exitCode == 0 && res.svcCode == 0 {
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
	handler := NewWindowsHandler(mock)

	changes := make(chan svc.Status, 10)
	requests := make(chan svc.ChangeRequest, 2)

	// Send immediate stop
	go func() {
		time.Sleep(50 * time.Millisecond)
		requests <- svc.ChangeRequest{Cmd: svc.Stop}
	}()

	done := make(chan struct {
		svcCode  uint32
		exitCode uint32
	}, 1)
	go func() {
		svcCode, exitCode := handler.Execute(nil, requests, changes)
		done <- struct {
			svcCode  uint32
			exitCode uint32
		}{svcCode, exitCode}
	}()

	select {
	case result := <-done:
		// Should indicate failure due to shutdown error
		if result.exitCode == 0 && result.svcCode == 0 {
			t.Error("Execute() should return non-zero exit code on shutdown failure")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Execute() did not complete")
	}
}

// TestWindowsHandler_Execute_HandlesChannelClosure tests that Execute() handles
// unexpected channel closure gracefully.
func TestWindowsHandler_Execute_HandlesChannelClosure(t *testing.T) {
	mock := &MockRunner{}
	handler := NewWindowsHandler(mock)

	changes := make(chan svc.Status, 10)
	requests := make(chan svc.ChangeRequest, 2)

	// Close requests channel after handler starts to simulate unexpected closure
	go func() {
		time.Sleep(100 * time.Millisecond)
		close(requests)
	}()

	done := make(chan struct {
		svcCode  uint32
		exitCode uint32
	}, 1)
	go func() {
		svcCode, exitCode := handler.Execute(nil, requests, changes)
		done <- struct {
			svcCode  uint32
			exitCode uint32
		}{svcCode, exitCode}
	}()

	select {
	case result := <-done:
		// Should return successfully even with channel closure
		if result.exitCode != 0 || result.svcCode != 0 {
			t.Errorf("Execute() returned unexpected exit codes: svc=%d, exit=%d", result.svcCode, result.exitCode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Execute() did not complete on channel closure")
	}
}

// TestWindowsHandler_Execute_HandlesShutdown tests that Execute() handles Shutdown command.
func TestWindowsHandler_Execute_HandlesShutdown(t *testing.T) {
	mock := &MockRunner{}
	handler := NewWindowsHandler(mock)

	changes := make(chan svc.Status, 10)
	requests := make(chan svc.ChangeRequest, 2)

	// Send shutdown command instead of stop
	go func() {
		time.Sleep(50 * time.Millisecond)
		requests <- svc.ChangeRequest{Cmd: svc.Shutdown}
	}()

	done := make(chan struct {
		svcCode  uint32
		exitCode uint32
	}, 1)
	go func() {
		svcCode, exitCode := handler.Execute(nil, requests, changes)
		done <- struct {
			svcCode  uint32
			exitCode uint32
		}{svcCode, exitCode}
	}()

	select {
	case result := <-done:
		if result.exitCode != 0 || result.svcCode != 0 {
			t.Errorf("Execute() returned unexpected exit codes on shutdown: svc=%d, exit=%d", result.svcCode, result.exitCode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Execute() did not handle Shutdown command")
	}

	if !mock.shutdownCalled {
		t.Error("Execute() did not call runner.Shutdown() on Shutdown command")
	}

	// Verify state transitions include StopPending and Stopped
	var statuses []svc.State
	timeout := time.After(500 * time.Millisecond)
collectLoop:
	for {
		select {
		case status := <-changes:
			statuses = append(statuses, status.State)
			if status.State == svc.Stopped {
				break collectLoop
			}
		case <-timeout:
			break collectLoop
		}
	}

	foundStopPending := false
	foundStopped := false
	for _, state := range statuses {
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
	handler := NewWindowsHandler(mock)

	changes := make(chan svc.Status, 10)
	requests := make(chan svc.ChangeRequest, 10)

	// Send unknown commands followed by stop
	go func() {
		time.Sleep(50 * time.Millisecond)
		// Send Pause and Continue commands which are not accepted by the handler
		requests <- svc.ChangeRequest{Cmd: svc.Pause}
		time.Sleep(25 * time.Millisecond)
		requests <- svc.ChangeRequest{Cmd: svc.Continue}
		time.Sleep(25 * time.Millisecond)
		// Send a completely unknown command (using a high value)
		requests <- svc.ChangeRequest{Cmd: svc.Cmd(255)}
		time.Sleep(25 * time.Millisecond)
		requests <- svc.ChangeRequest{Cmd: svc.Stop}
	}()

	done := make(chan struct {
		svcCode  uint32
		exitCode uint32
	}, 1)
	go func() {
		svcCode, exitCode := handler.Execute(nil, requests, changes)
		done <- struct {
			svcCode  uint32
			exitCode uint32
		}{svcCode, exitCode}
	}()

	select {
	case result := <-done:
		if result.exitCode != 0 || result.svcCode != 0 {
			t.Errorf("Execute() returned unexpected exit codes: svc=%d, exit=%d", result.svcCode, result.exitCode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Execute() did not complete after unknown commands")
	}

	// Verify the service remained running and ignored unknown commands
	var statuses []svc.State
	timeout := time.After(500 * time.Millisecond)
collectLoop:
	for {
		select {
		case status := <-changes:
			statuses = append(statuses, status.State)
			if status.State == svc.Stopped {
				break collectLoop
			}
		case <-timeout:
			break collectLoop
		}
	}

	// Verify we never transitioned to Paused or any unexpected state
	for _, state := range statuses {
		if state == svc.Paused || state == svc.PausePending || state == svc.ContinuePending {
			t.Errorf("Execute() incorrectly processed unknown command, transitioned to %v", state)
		}
	}
}

// TestWindowsHandler_AcceptsCorrectCommands tests accepted command mask.
func TestWindowsHandler_AcceptsCorrectCommands(t *testing.T) {
	handler := NewWindowsHandler(&MockRunner{})

	accepts := handler.AcceptedCommands()

	expectedAccepts := svc.AcceptStop | svc.AcceptShutdown

	if accepts != expectedAccepts {
		t.Errorf("AcceptedCommands() = %v, want %v", accepts, expectedAccepts)
	}
}
