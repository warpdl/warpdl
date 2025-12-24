//go:build windows

package cmd

import (
	"context"
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
