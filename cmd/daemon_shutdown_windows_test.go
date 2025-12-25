//go:build windows

package cmd

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestSetupShutdownHandler_ReturnsValidContextAndCancel verifies that
// setupShutdownHandler returns non-nil context and cancel function.
func TestSetupShutdownHandler_ReturnsValidContextAndCancel(t *testing.T) {
	ctx, cancel := setupShutdownHandler()

	if ctx == nil {
		t.Error("setupShutdownHandler() returned nil context")
	}

	if cancel == nil {
		t.Error("setupShutdownHandler() returned nil cancel function")
	}

	// Clean up goroutine
	if cancel != nil {
		cancel()
	}
}

// TestSetupShutdownHandler_ContextNotInitiallyCanceled verifies that the
// returned context is not initially canceled when created.
func TestSetupShutdownHandler_ContextNotInitiallyCanceled(t *testing.T) {
	ctx, cancel := setupShutdownHandler()
	defer cancel()

	if ctx.Err() != nil {
		t.Errorf("setupShutdownHandler() context.Err() = %v, want nil", ctx.Err())
	}

	// Verify context is not done initially
	select {
	case <-ctx.Done():
		t.Error("context should not be done initially")
	default:
		// Expected: context is not done
	}
}

// TestSetupShutdownHandler_CancelCancelsContext verifies that calling the
// returned cancel function properly cancels the context.
func TestSetupShutdownHandler_CancelCancelsContext(t *testing.T) {
	ctx, cancel := setupShutdownHandler()

	// Call cancel
	cancel()

	// Wait briefly for cancellation to propagate
	select {
	case <-ctx.Done():
		// Expected: context is canceled
	case <-time.After(100 * time.Millisecond):
		t.Error("context was not canceled after calling cancel()")
	}

	// Verify context error is set
	if ctx.Err() == nil {
		t.Error("context.Err() should not be nil after cancel()")
	}

	if ctx.Err() != context.Canceled {
		t.Errorf("context.Err() = %v, want %v", ctx.Err(), context.Canceled)
	}
}

func TestSetupShutdownHandler_SignalCancelsContext(t *testing.T) {
	var captured chan<- os.Signal
	notifyCalled := make(chan struct{})
	stopCalled := make(chan struct{})

	overrideSignalHooks(t, func(ch chan<- os.Signal, sig ...os.Signal) {
		if len(sig) != 1 || sig[0] != os.Interrupt {
			t.Fatalf("signalNotify called with %v, want os.Interrupt", sig)
		}
		captured = ch
		close(notifyCalled)
	}, func(ch chan<- os.Signal) {
		close(stopCalled)
	})

	ctx, cancel := setupShutdownHandler()
	defer cancel()

	select {
	case <-notifyCalled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("signalNotify was not invoked")
	}

	captured <- os.Interrupt

	select {
	case <-ctx.Done():
	case <-time.After(500 * time.Millisecond):
		t.Fatal("context was not canceled after signal")
	}

	select {
	case <-stopCalled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("signalStop was not invoked")
	}
}

func overrideSignalHooks(t *testing.T, notify func(chan<- os.Signal, ...os.Signal), stop func(chan<- os.Signal)) {
	t.Helper()
	origNotify := signalNotify
	origStop := signalStop
	signalNotify = notify
	signalStop = stop
	t.Cleanup(func() {
		signalNotify = origNotify
		signalStop = origStop
	})
}
