package logger

// MultiLogger broadcasts log messages to multiple Logger backends.
// Useful for logging to both console and Windows Event Log simultaneously.
type MultiLogger struct {
	loggers []Logger
}

// NewMultiLogger creates a logger that writes to all provided backends.
// Messages are written to each logger in order. Errors from individual
// loggers are ignored to ensure all backends receive the message.
func NewMultiLogger(loggers ...Logger) *MultiLogger {
	return &MultiLogger{loggers: loggers}
}

// Info logs an informational message to all backends.
func (m *MultiLogger) Info(format string, args ...interface{}) {
	for _, l := range m.loggers {
		l.Info(format, args...)
	}
}

// Warning logs a warning message to all backends.
func (m *MultiLogger) Warning(format string, args ...interface{}) {
	for _, l := range m.loggers {
		l.Warning(format, args...)
	}
}

// Error logs an error message to all backends.
func (m *MultiLogger) Error(format string, args ...interface{}) {
	for _, l := range m.loggers {
		l.Error(format, args...)
	}
}

// Close closes all logger backends.
// Returns the first error encountered, but attempts to close all loggers.
func (m *MultiLogger) Close() error {
	var firstErr error
	for _, l := range m.loggers {
		if err := l.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Ensure MultiLogger satisfies the Logger interface.
var _ Logger = (*MultiLogger)(nil)
