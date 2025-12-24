//go:build windows

package service

import (
	"fmt"
	"log"

	"golang.org/x/sys/windows/svc/eventlog"
)

// EventLogger defines an interface for logging service events.
// This allows for different implementations (Windows Event Log vs console).
type EventLogger interface {
	// Info logs an informational message.
	Info(msg string) error
	// Warning logs a warning message.
	Warning(msg string) error
	// Error logs an error message.
	Error(msg string) error
	// Close closes the logger and releases resources.
	Close() error
}

// WindowsEventLogger implements EventLogger using Windows Event Log.
type WindowsEventLogger struct {
	log *eventlog.Log
}

// NewWindowsEventLogger creates a new Windows Event Log logger.
// It installs the event source if needed and opens the event log.
func NewWindowsEventLogger(serviceName string) (*WindowsEventLogger, error) {
	// Try to install the event source. If it already exists, this will fail
	// but we can ignore the error and proceed to open the log.
	_ = eventlog.InstallAsEventCreate(serviceName, eventlog.Error|eventlog.Warning|eventlog.Info)

	// Open the event log for the service
	elog, err := eventlog.Open(serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to open event log: %w", err)
	}

	return &WindowsEventLogger{log: elog}, nil
}

// Info logs an informational message to the Windows Event Log.
func (w *WindowsEventLogger) Info(msg string) error {
	return w.log.Info(1, msg)
}

// Warning logs a warning message to the Windows Event Log.
func (w *WindowsEventLogger) Warning(msg string) error {
	return w.log.Warning(2, msg)
}

// Error logs an error message to the Windows Event Log.
func (w *WindowsEventLogger) Error(msg string) error {
	return w.log.Error(3, msg)
}

// Close closes the event log.
func (w *WindowsEventLogger) Close() error {
	return w.log.Close()
}

// ConsoleEventLogger implements EventLogger using standard Go logging.
// This is used when running in console mode (not as a service).
type ConsoleEventLogger struct {
	logger *log.Logger
}

// NewConsoleEventLogger creates a new console logger.
func NewConsoleEventLogger(logger *log.Logger) *ConsoleEventLogger {
	if logger == nil {
		logger = log.Default()
	}
	return &ConsoleEventLogger{logger: logger}
}

// Info logs an informational message to the console.
func (c *ConsoleEventLogger) Info(msg string) error {
	c.logger.Printf("[INFO] %s", msg)
	return nil
}

// Warning logs a warning message to the console.
func (c *ConsoleEventLogger) Warning(msg string) error {
	c.logger.Printf("[WARNING] %s", msg)
	return nil
}

// Error logs an error message to the console.
func (c *ConsoleEventLogger) Error(msg string) error {
	c.logger.Printf("[ERROR] %s", msg)
	return nil
}

// Close is a no-op for console logger.
func (c *ConsoleEventLogger) Close() error {
	return nil
}

// RemoveEventSource removes the event source from the Windows Event Log.
// This should be called during service uninstallation.
func RemoveEventSource(serviceName string) error {
	return eventlog.Remove(serviceName)
}

// RegisterEventSource registers the event source with Windows Event Log.
// This should be called during service installation.
func RegisterEventSource(serviceName string) error {
	return eventlog.InstallAsEventCreate(serviceName, eventlog.Error|eventlog.Warning|eventlog.Info)
}

// UnregisterEventSource is an alias for RemoveEventSource for consistency.
func UnregisterEventSource(serviceName string) error {
	return RemoveEventSource(serviceName)
}
