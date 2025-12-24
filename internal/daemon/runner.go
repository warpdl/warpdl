// Package daemon provides the core daemon runner for WarpDL.
// It manages the lifecycle of the download service including start, stop,
// and graceful shutdown capabilities.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// Sentinel errors for the daemon runner.
var (
	// ErrAlreadyRunning is returned when Start() is called on a running daemon.
	ErrAlreadyRunning = errors.New("daemon is already running")

	// ErrNotRunning is returned when Shutdown() is called on a stopped daemon.
	ErrNotRunning = errors.New("daemon is not running")

	// ErrShutdownTimeout is returned when shutdown exceeds the configured timeout.
	ErrShutdownTimeout = errors.New("shutdown timed out")
)

// Service name constants for Windows service registration.
const (
	// DefaultServiceName is the default Windows service name.
	DefaultServiceName = "WarpDL"

	// DefaultDisplayName is the default Windows service display name.
	DefaultDisplayName = "WarpDL Download Manager"

	// DefaultDescription is the default Windows service description.
	DefaultDescription = "High-performance parallel download manager"
)

// Config holds the configuration for the daemon runner.
type Config struct {
	// ServiceName is the Windows service name.
	ServiceName string

	// DisplayName is the Windows service display name.
	DisplayName string

	// Port is the TCP port for fallback connections.
	// Use 0 for an ephemeral port.
	Port int

	// ConfigDir is the directory for configuration files.
	ConfigDir string

	// ShutdownTimeout is the maximum time to wait for graceful shutdown.
	// A zero value means no timeout.
	ShutdownTimeout time.Duration
}

// Dependencies holds the external dependencies for the daemon runner.
// This enables dependency injection for testing.
type Dependencies struct {
	// ListenerFactory creates network listeners.
	// If nil, net.Listen is used.
	ListenerFactory func(network, address string) (net.Listener, error)

	// ShutdownFunc is called during shutdown to clean up resources.
	// If nil, no cleanup function is called.
	ShutdownFunc func() error
}

// Runner manages the daemon lifecycle.
type Runner struct {
	config   *Config
	deps     *Dependencies
	running  bool
	mu       sync.Mutex
	cancel   context.CancelFunc
	listener net.Listener
}

// New creates a new daemon runner with the given configuration and dependencies.
// If config is nil, default values are used.
// If deps is nil, default dependencies (using net.Listen) are used.
func New(config *Config, deps *Dependencies) *Runner {
	cfg := applyConfigDefaults(config)
	d := applyDependencyDefaults(deps)

	return &Runner{
		config: cfg,
		deps:   d,
	}
}

// applyConfigDefaults returns a Config with default values applied for nil fields.
func applyConfigDefaults(config *Config) *Config {
	if config == nil {
		return &Config{
			ServiceName: DefaultServiceName,
			DisplayName: DefaultDisplayName,
		}
	}
	return config
}

// applyDependencyDefaults returns Dependencies with default values applied.
func applyDependencyDefaults(deps *Dependencies) *Dependencies {
	if deps == nil {
		deps = &Dependencies{}
	}
	if deps.ListenerFactory == nil {
		deps.ListenerFactory = net.Listen
	}
	return deps
}

// Config returns the runner's configuration.
func (r *Runner) Config() *Config {
	return r.config
}

// Start begins the daemon and blocks until the context is canceled.
// Returns ErrAlreadyRunning if the daemon is already started.
func (r *Runner) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return ErrAlreadyRunning
	}

	// Create cancellable context
	ctx, r.cancel = context.WithCancel(ctx)

	// Create listener BEFORE setting running=true to avoid race condition
	addr := formatListenAddress(r.config.Port)
	listener, err := r.deps.ListenerFactory("tcp", addr)
	if err != nil {
		r.mu.Unlock()
		return err
	}
	r.listener = listener

	// Only set running after listener is successfully created
	r.running = true
	r.mu.Unlock()

	// Wait for context cancellation
	<-ctx.Done()

	// Cleanup on exit
	r.cleanupOnStop()

	return ctx.Err()
}

// formatListenAddress returns the address string for the given port.
// Port 0 results in an ephemeral port assignment.
func formatListenAddress(port int) string {
	if port <= 0 {
		return ":0"
	}
	return fmt.Sprintf(":%d", port)
}

// cleanupOnStop performs cleanup when the daemon stops.
func (r *Runner) cleanupOnStop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.running = false
	r.closeListener()
}

// closeListener closes the listener if it exists.
// Caller must hold the mutex.
// Listener close errors are intentionally ignored as this is cleanup code.
func (r *Runner) closeListener() {
	if r.listener != nil {
		_ = r.listener.Close()
		r.listener = nil
	}
}

// Shutdown gracefully stops the daemon.
// Returns ErrNotRunning if the daemon is not running.
// Returns ErrShutdownTimeout if the shutdown function exceeds the configured timeout.
func (r *Runner) Shutdown() error {
	if err := r.validateRunning(); err != nil {
		return err
	}

	// Execute shutdown function if configured
	if err := r.executeShutdownFunc(); err != nil {
		return err
	}

	// Perform final cleanup
	r.performShutdown()

	return nil
}

// validateRunning checks if the daemon is running.
// Returns ErrNotRunning if not running.
func (r *Runner) validateRunning() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.running {
		return ErrNotRunning
	}
	return nil
}

// executeShutdownFunc runs the shutdown function with timeout if configured.
// Returns ErrShutdownTimeout if the function exceeds the timeout.
func (r *Runner) executeShutdownFunc() error {
	if r.deps.ShutdownFunc == nil {
		return nil
	}

	if r.config.ShutdownTimeout > 0 {
		return r.executeWithTimeout(r.deps.ShutdownFunc, r.config.ShutdownTimeout)
	}

	// Shutdown function error is intentionally ignored during cleanup.
	// The shutdown must proceed regardless of cleanup errors.
	_ = r.deps.ShutdownFunc()
	return nil
}

// executeWithTimeout runs a function with a timeout.
// Returns ErrShutdownTimeout if the function exceeds the timeout.
// Returns the function's error if it completes within the timeout.
func (r *Runner) executeWithTimeout(fn func() error, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() {
		done <- fn()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		r.forceStop()
		return ErrShutdownTimeout
	}
}

// forceStop forces the daemon to stop without waiting for cleanup.
func (r *Runner) forceStop() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.running = false
	if r.cancel != nil {
		r.cancel()
	}
}

// performShutdown performs the final shutdown operations.
func (r *Runner) performShutdown() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.running = false
	if r.cancel != nil {
		r.cancel()
	}
	r.closeListener()
}

// IsRunning returns true if the daemon is currently running.
func (r *Runner) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}
