package warplib

import (
	"bytes"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSafeGoNormalCompletion verifies that safeGo runs a function normally
// and decrements the WaitGroup when the function completes successfully.
func TestSafeGoNormalCompletion(t *testing.T) {
	var wg sync.WaitGroup
	var executed atomic.Bool

	wg.Add(1)
	safeGo(nil, &wg, "test-normal", nil, func() {
		executed.Store(true)
	})

	// Wait for goroutine to complete with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("safeGo did not decrement WaitGroup after normal completion")
	}

	if !executed.Load() {
		t.Error("safeGo did not execute the provided function")
	}
}

// TestSafeGoPanicRecovery verifies that safeGo recovers from panics
// without crashing the program.
func TestSafeGoPanicRecovery(t *testing.T) {
	var wg sync.WaitGroup

	wg.Add(1)
	safeGo(nil, &wg, "test-panic-recovery", nil, func() {
		panic("test panic")
	})

	// Wait for goroutine to complete with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - panic was recovered and WaitGroup was decremented
	case <-time.After(1 * time.Second):
		t.Fatal("safeGo did not decrement WaitGroup after panic")
	}
}

// TestSafeGoPanicLogsStackTrace verifies that safeGo logs panic details
// including the panic value, stack trace, and context string.
func TestSafeGoPanicLogsStackTrace(t *testing.T) {
	var logBuf bytes.Buffer
	logger := log.New(&logBuf, "", 0)
	var wg sync.WaitGroup

	panicMsg := "test panic message"
	context := "download-segment-5"

	wg.Add(1)
	safeGo(logger, &wg, context, nil, func() {
		panic(panicMsg)
	})

	// Wait for goroutine to complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("safeGo did not complete after panic")
	}

	logOutput := logBuf.String()

	// Verify log contains panic value
	if !strings.Contains(logOutput, panicMsg) {
		t.Errorf("log output missing panic message %q, got: %s", panicMsg, logOutput)
	}

	// Verify log contains context string
	if !strings.Contains(logOutput, context) {
		t.Errorf("log output missing context %q, got: %s", context, logOutput)
	}

	// Verify log contains stack trace indicators
	// Stack traces should contain "goroutine" or function names from runtime
	hasStackTrace := strings.Contains(logOutput, "goroutine") ||
		strings.Contains(logOutput, "runtime.") ||
		strings.Contains(logOutput, "pkg/warplib")

	if !hasStackTrace {
		t.Errorf("log output missing stack trace, got: %s", logOutput)
	}
}

// TestSafeGoCallsOnPanicCallback verifies that the onPanic callback
// is invoked with the recovered panic value.
func TestSafeGoCallsOnPanicCallback(t *testing.T) {
	var wg sync.WaitGroup
	var callbackCalled atomic.Bool
	var recoveredValue atomic.Value

	panicValue := "critical error"

	onPanic := func(r interface{}) {
		callbackCalled.Store(true)
		recoveredValue.Store(r)
	}

	wg.Add(1)
	safeGo(nil, &wg, "test-callback", onPanic, func() {
		panic(panicValue)
	})

	// Wait for goroutine to complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("safeGo did not complete after panic")
	}

	if !callbackCalled.Load() {
		t.Error("onPanic callback was not called after panic")
	}

	recovered := recoveredValue.Load()
	if recovered != panicValue {
		t.Errorf("onPanic received %v, expected %v", recovered, panicValue)
	}
}

// TestSafeGoWithWaitGroupDecrements verifies that the WaitGroup is always
// decremented even when the function panics, preventing deadlocks.
func TestSafeGoWithWaitGroupDecrements(t *testing.T) {
	testCases := []struct {
		name      string
		fnPanics  bool
		panicWith interface{}
	}{
		{
			name:     "normal completion",
			fnPanics: false,
		},
		{
			name:      "panic with string",
			fnPanics:  true,
			panicWith: "panic message",
		},
		{
			name:      "panic with error",
			fnPanics:  true,
			panicWith: &testError{msg: "custom error"},
		},
		{
			name:      "panic with nil",
			fnPanics:  true,
			panicWith: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var wg sync.WaitGroup

			wg.Add(1)
			safeGo(nil, &wg, tc.name, nil, func() {
				if tc.fnPanics {
					panic(tc.panicWith)
				}
			})

			// Wait for goroutine with timeout
			done := make(chan struct{})
			go func() {
				wg.Wait()
				close(done)
			}()

			select {
			case <-done:
				// Success - WaitGroup was decremented
			case <-time.After(1 * time.Second):
				t.Fatalf("WaitGroup.Wait() timed out - WaitGroup was not decremented")
			}
		})
	}
}

// TestSafeGoWithoutWaitGroup verifies that safeGo handles nil WaitGroup
// gracefully without panicking.
func TestSafeGoWithoutWaitGroup(t *testing.T) {
	var executed atomic.Bool
	executeDone := make(chan struct{})

	// Call safeGo with nil WaitGroup
	safeGo(nil, nil, "test-no-wg", nil, func() {
		executed.Store(true)
		close(executeDone)
	})

	// Wait for function execution with timeout
	select {
	case <-executeDone:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("safeGo did not execute function when WaitGroup is nil")
	}

	if !executed.Load() {
		t.Error("safeGo did not execute the provided function")
	}
}

// TestSafeGoWithoutLogger verifies that safeGo handles nil logger
// gracefully when a panic occurs.
func TestSafeGoWithoutLogger(t *testing.T) {
	var wg sync.WaitGroup
	var panicRecovered atomic.Bool

	onPanic := func(r interface{}) {
		panicRecovered.Store(true)
	}

	wg.Add(1)
	safeGo(nil, &wg, "test-no-logger", onPanic, func() {
		panic("panic without logger")
	})

	// Wait for goroutine to complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - no crash despite nil logger
	case <-time.After(1 * time.Second):
		t.Fatal("safeGo did not complete after panic with nil logger")
	}

	if !panicRecovered.Load() {
		t.Error("panic was not recovered when logger is nil")
	}
}

// TestSafeGoWithoutOnPanic verifies that safeGo handles nil onPanic callback
// gracefully when a panic occurs.
func TestSafeGoWithoutOnPanic(t *testing.T) {
	var logBuf bytes.Buffer
	logger := log.New(&logBuf, "", 0)
	var wg sync.WaitGroup

	wg.Add(1)
	safeGo(logger, &wg, "test-no-callback", nil, func() {
		panic("panic without onPanic callback")
	})

	// Wait for goroutine to complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - no crash despite nil onPanic
	case <-time.After(1 * time.Second):
		t.Fatal("safeGo did not complete after panic with nil onPanic")
	}

	// Verify logging still occurred even without onPanic callback
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "panic") {
		t.Error("panic was not logged when onPanic is nil")
	}
}

// TestSafeGoMultipleConcurrent verifies that safeGo can handle multiple
// concurrent goroutines with a mix of normal and panicking functions.
func TestSafeGoMultipleConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	var normalCount atomic.Int32
	var panicCount atomic.Int32

	onPanic := func(r interface{}) {
		panicCount.Add(1)
	}

	const numNormal = 10
	const numPanic = 10

	// Launch normal goroutines
	for i := 0; i < numNormal; i++ {
		wg.Add(1)
		safeGo(nil, &wg, "concurrent-normal", nil, func() {
			time.Sleep(10 * time.Millisecond)
			normalCount.Add(1)
		})
	}

	// Launch panicking goroutines
	for i := 0; i < numPanic; i++ {
		wg.Add(1)
		safeGo(nil, &wg, "concurrent-panic", onPanic, func() {
			time.Sleep(10 * time.Millisecond)
			panic("concurrent panic")
		})
	}

	// Wait for all goroutines to complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("not all goroutines completed within timeout")
	}

	if got := normalCount.Load(); got != numNormal {
		t.Errorf("expected %d normal completions, got %d", numNormal, got)
	}

	if got := panicCount.Load(); got != numPanic {
		t.Errorf("expected %d panic recoveries, got %d", numPanic, got)
	}
}

// testError is a custom error type for testing panic recovery with error types.
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
