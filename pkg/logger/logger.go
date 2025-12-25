// Package logger provides a platform-agnostic logging interface for WarpDL.
// It supports multiple backends including console output and Windows Event Log.
package logger

import (
	"fmt"
	"log"
)

// Logger defines the interface for structured logging across all WarpDL components.
// Implementations may log to console, files, Windows Event Log, or syslog.
type Logger interface {
	// Info logs an informational message (e.g., "Service started").
	Info(format string, args ...interface{})

	// Warning logs a warning message (e.g., "Retry attempt 2/3").
	Warning(format string, args ...interface{})

	// Error logs an error message (e.g., "Failed to start server: connection refused").
	Error(format string, args ...interface{})

	// Close releases resources held by the logger (e.g., Windows Event Log handle).
	// Safe to call multiple times. Returns nil for loggers without resources.
	Close() error
}

// StandardLogger wraps the stdlib *log.Logger for console/file output.
// Used when running as a console application (non-service mode).
type StandardLogger struct {
	logger *log.Logger
}

// NewStandardLogger creates a logger that wraps the given *log.Logger.
// Enables gradual migration from existing log.Default() usage.
func NewStandardLogger(l *log.Logger) *StandardLogger {
	return &StandardLogger{logger: l}
}

// Info logs an informational message with [INFO] prefix.
func (s *StandardLogger) Info(format string, args ...interface{}) {
	s.logger.Printf("[INFO] "+format, args...)
}

// Warning logs a warning message with [WARNING] prefix.
func (s *StandardLogger) Warning(format string, args ...interface{}) {
	s.logger.Printf("[WARNING] "+format, args...)
}

// Error logs an error message with [ERROR] prefix.
func (s *StandardLogger) Error(format string, args ...interface{}) {
	s.logger.Printf("[ERROR] "+format, args...)
}

// Close is a no-op for StandardLogger (no resources to release).
func (s *StandardLogger) Close() error {
	return nil
}

// NopLogger is a logger that discards all messages.
// Useful for testing or when logging should be disabled.
type NopLogger struct{}

// NewNopLogger creates a logger that discards all messages.
func NewNopLogger() *NopLogger {
	return &NopLogger{}
}

// Info discards the message.
func (n *NopLogger) Info(format string, args ...interface{}) {}

// Warning discards the message.
func (n *NopLogger) Warning(format string, args ...interface{}) {}

// Error discards the message.
func (n *NopLogger) Error(format string, args ...interface{}) {}

// Close is a no-op.
func (n *NopLogger) Close() error {
	return nil
}

// Ensure implementations satisfy the Logger interface.
var (
	_ Logger = (*StandardLogger)(nil)
	_ Logger = (*NopLogger)(nil)
)

// MockLogger implements Logger for testing purposes.
// It records all log calls for verification in tests.
type MockLogger struct {
	InfoCalls    []string
	WarningCalls []string
	ErrorCalls   []string
	CloseCalled  bool
}

// NewMockLogger creates a new MockLogger for testing.
func NewMockLogger() *MockLogger {
	return &MockLogger{
		InfoCalls:    make([]string, 0),
		WarningCalls: make([]string, 0),
		ErrorCalls:   make([]string, 0),
	}
}

// Info records the formatted message.
func (m *MockLogger) Info(format string, args ...interface{}) {
	m.InfoCalls = append(m.InfoCalls, fmt.Sprintf(format, args...))
}

// Warning records the formatted message.
func (m *MockLogger) Warning(format string, args ...interface{}) {
	m.WarningCalls = append(m.WarningCalls, fmt.Sprintf(format, args...))
}

// Error records the formatted message.
func (m *MockLogger) Error(format string, args ...interface{}) {
	m.ErrorCalls = append(m.ErrorCalls, fmt.Sprintf(format, args...))
}

// Close records that Close was called.
func (m *MockLogger) Close() error {
	m.CloseCalled = true
	return nil
}

// Ensure MockLogger satisfies the Logger interface.
var _ Logger = (*MockLogger)(nil)
