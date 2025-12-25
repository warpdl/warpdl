//go:build windows

package logger

// EventLogWriter abstracts the Windows Event Log API for testability.
// The interface matches the methods of eventlog.Log that EventLogger uses.
type EventLogWriter interface {
	// Info writes an information event to the event log.
	Info(eid uint32, msg string) error

	// Warning writes a warning event to the event log.
	Warning(eid uint32, msg string) error

	// Error writes an error event to the event log.
	Error(eid uint32, msg string) error

	// Close closes the event log handle.
	Close() error
}
