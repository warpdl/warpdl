//go:build windows

package logger

import (
	"fmt"

	"golang.org/x/sys/windows/svc/eventlog"
)

// Event IDs for Windows Event Log entries.
const (
	// EventIDInfo is used for informational messages (service started, running, stopped).
	EventIDInfo uint32 = 1

	// EventIDWarning is used for warning messages.
	EventIDWarning uint32 = 2

	// EventIDError is used for error messages (startup failures, shutdown errors).
	EventIDError uint32 = 3
)

// EventLogger writes log messages to Windows Event Log.
// The event source must be registered via eventlog.InstallAsEventCreate()
// before creating an EventLogger.
type EventLogger struct {
	log *eventlog.Log
}

// NewEventLogger creates a logger that writes to Windows Event Log.
// sourceName is the Event Log source (typically the service name).
// Returns error if the source is not registered or cannot be opened.
//
// IMPORTANT: The Event Source must be registered during service installation
// using eventlog.InstallAsEventCreate(). See cmd/service_windows.go.
func NewEventLogger(sourceName string) (*EventLogger, error) {
	elog, err := eventlog.Open(sourceName)
	if err != nil {
		return nil, fmt.Errorf("failed to open event log: %w", err)
	}
	return &EventLogger{log: elog}, nil
}

// Info logs an informational message to Windows Event Log.
// Uses Event ID 1 for informational events.
func (e *EventLogger) Info(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	// Error intentionally ignored - service must continue even if logging fails.
	_ = e.log.Info(EventIDInfo, msg)
}

// Warning logs a warning message to Windows Event Log.
// Uses Event ID 2 for warning events.
func (e *EventLogger) Warning(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	_ = e.log.Warning(EventIDWarning, msg)
}

// Error logs an error message to Windows Event Log.
// Uses Event ID 3 for error events.
func (e *EventLogger) Error(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	_ = e.log.Error(EventIDError, msg)
}

// Close releases the Windows Event Log handle.
func (e *EventLogger) Close() error {
	if e.log != nil {
		return e.log.Close()
	}
	return nil
}

// Ensure EventLogger satisfies the Logger interface.
var _ Logger = (*EventLogger)(nil)
