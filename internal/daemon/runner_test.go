package daemon

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// TestNewRunner_CreatesWithCorrectConfig tests that New() creates a runner with
// the correct configuration values.
func TestNewRunner_CreatesWithCorrectConfig(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		want   *Config
	}{
		{
			name: "default configuration",
			config: &Config{
				ServiceName: "WarpDL",
				DisplayName: "WarpDL Download Manager",
				Port:        3849,
			},
			want: &Config{
				ServiceName: "WarpDL",
				DisplayName: "WarpDL Download Manager",
				Port:        3849,
			},
		},
		{
			name: "custom port",
			config: &Config{
				ServiceName: "WarpDL",
				DisplayName: "WarpDL Download Manager",
				Port:        4000,
			},
			want: &Config{
				ServiceName: "WarpDL",
				DisplayName: "WarpDL Download Manager",
				Port:        4000,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := New(tt.config, nil)
			if runner == nil {
				t.Fatal("New() returned nil runner")
			}
			if runner.Config().ServiceName != tt.want.ServiceName {
				t.Errorf("ServiceName = %q, want %q", runner.Config().ServiceName, tt.want.ServiceName)
			}
			if runner.Config().DisplayName != tt.want.DisplayName {
				t.Errorf("DisplayName = %q, want %q", runner.Config().DisplayName, tt.want.DisplayName)
			}
			if runner.Config().Port != tt.want.Port {
				t.Errorf("Port = %d, want %d", runner.Config().Port, tt.want.Port)
			}
		})
	}
}

// TestNewRunner_NilConfig tests that New() handles nil config gracefully.
func TestNewRunner_NilConfig(t *testing.T) {
	runner := New(nil, nil)
	if runner == nil {
		t.Fatal("New() with nil config returned nil runner")
	}
	// Should use default values
	cfg := runner.Config()
	if cfg.ServiceName != DefaultServiceName {
		t.Errorf("ServiceName = %q, want default %q", cfg.ServiceName, DefaultServiceName)
	}
}

// TestRunner_Start_BeginsListening tests that Start() begins listening for connections.
func TestRunner_Start_BeginsListening(t *testing.T) {
	base := t.TempDir()

	config := &Config{
		ServiceName: "WarpDL",
		DisplayName: "WarpDL Download Manager",
		Port:        0, // Use ephemeral port
		ConfigDir:   base,
	}

	// Use atomic for thread-safe access
	var listenerCreated atomic.Bool
	mockListenerFactory := func(network, address string) (net.Listener, error) {
		listenerCreated.Store(true)
		return net.Listen(network, address)
	}

	runner := New(config, &Dependencies{
		ListenerFactory: mockListenerFactory,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runner.Start(ctx)
	}()

	// Give time for Start to begin
	time.Sleep(50 * time.Millisecond)

	if !listenerCreated.Load() {
		t.Error("Start() did not create listener")
	}

	// Check if runner is in running state
	if !runner.IsRunning() {
		t.Error("Start() did not set running state")
	}

	cancel()
	<-errCh
}

// TestRunner_Start_ReturnsErrorIfAlreadyRunning tests that Start() returns an error
// if the runner is already started.
func TestRunner_Start_ReturnsErrorIfAlreadyRunning(t *testing.T) {
	base := t.TempDir()

	config := &Config{
		ServiceName: "WarpDL",
		Port:        0,
		ConfigDir:   base,
	}

	runner := New(config, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start first instance
	go func() {
		_ = runner.Start(ctx)
	}()

	// Give time for Start to begin
	time.Sleep(50 * time.Millisecond)

	// Attempt to start again
	err := runner.Start(ctx)
	if err == nil {
		t.Error("Start() should return error when already running")
	}
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("Start() error = %v, want ErrAlreadyRunning", err)
	}
}

// TestRunner_Shutdown_GracefullyStops tests that Shutdown() gracefully stops the runner.
func TestRunner_Shutdown(t *testing.T) {
	base := t.TempDir()

	config := &Config{
		ServiceName: "WarpDL",
		Port:        0,
		ConfigDir:   base,
	}

	var shutdownCalled atomic.Bool
	mockShutdownFunc := func() error {
		shutdownCalled.Store(true)
		return nil
	}

	runner := New(config, &Dependencies{
		ShutdownFunc: mockShutdownFunc,
	})

	ctx, cancel := context.WithCancel(context.Background())

	// Start the runner
	go func() {
		_ = runner.Start(ctx)
	}()

	// Give time for Start to begin
	time.Sleep(50 * time.Millisecond)

	// Shutdown
	err := runner.Shutdown()
	if err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	if !shutdownCalled.Load() {
		t.Error("Shutdown() did not call shutdown function")
	}

	if runner.IsRunning() {
		t.Error("Shutdown() did not stop the runner")
	}

	cancel()
}

// TestRunner_Shutdown_WithTimeout tests that Shutdown() respects timeout.
func TestRunner_Shutdown_WithTimeout(t *testing.T) {
	base := t.TempDir()

	config := &Config{
		ServiceName:     "WarpDL",
		Port:            0,
		ConfigDir:       base,
		ShutdownTimeout: 100 * time.Millisecond,
	}

	// Mock shutdown that takes too long
	mockShutdownFunc := func() error {
		time.Sleep(500 * time.Millisecond)
		return nil
	}

	runner := New(config, &Dependencies{
		ShutdownFunc: mockShutdownFunc,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = runner.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	err := runner.Shutdown()
	if err == nil {
		t.Error("Shutdown() should return timeout error")
	}
	if !errors.Is(err, ErrShutdownTimeout) {
		t.Errorf("Shutdown() error = %v, want ErrShutdownTimeout", err)
	}
}

// TestRunner_Shutdown_NotRunning tests that Shutdown() handles not-running state.
func TestRunner_Shutdown_NotRunning(t *testing.T) {
	config := &Config{
		ServiceName: "WarpDL",
	}

	runner := New(config, nil)

	err := runner.Shutdown()
	if err == nil {
		t.Error("Shutdown() should return error when not running")
	}
	if !errors.Is(err, ErrNotRunning) {
		t.Errorf("Shutdown() error = %v, want ErrNotRunning", err)
	}
}

// TestNewRunner_MissingDependencies tests that New() handles missing dependencies.
func TestNewRunner_MissingDependencies(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		deps    *Dependencies
		wantErr bool
	}{
		{
			name: "nil dependencies uses defaults",
			config: &Config{
				ServiceName: "WarpDL",
			},
			deps:    nil,
			wantErr: false,
		},
		{
			name:    "nil config uses defaults",
			config:  nil,
			deps:    nil,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := New(tt.config, tt.deps)
			if tt.wantErr && runner != nil {
				t.Error("New() should return nil on error")
			}
			if !tt.wantErr && runner == nil {
				t.Error("New() should not return nil")
			}
		})
	}
}

// TestRunner_Context_CancellationStopsRunner tests context cancellation stops the runner.
func TestRunner_Context_CancellationStopsRunner(t *testing.T) {
	base := t.TempDir()

	config := &Config{
		ServiceName: "WarpDL",
		Port:        0,
		ConfigDir:   base,
	}

	runner := New(config, nil)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- runner.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait for Start to return
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("Start() returned unexpected error: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("Start() did not return after context cancellation")
	}

	if runner.IsRunning() {
		t.Error("Runner should not be running after context cancellation")
	}
}

// TestRunner_ExecuteWithTimeout_ReturnsError tests that executeWithTimeout returns
// errors from the function when it completes within the timeout.
func TestRunner_ExecuteWithTimeout_ReturnsError(t *testing.T) {
	config := &Config{
		ServiceName:     "WarpDL",
		ShutdownTimeout: 1 * time.Second,
	}

	expectedErr := errors.New("shutdown error")
	runner := New(config, &Dependencies{
		ShutdownFunc: func() error {
			return expectedErr
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = runner.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	err := runner.Shutdown()
	if err == nil {
		t.Error("Shutdown() should return the error from shutdown function")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("Shutdown() error = %v, want %v", err, expectedErr)
	}
}
