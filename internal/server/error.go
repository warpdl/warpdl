package server

// ErrorType represents the severity level of a download error.
type ErrorType int

const (
	// ErrorTypeCritical indicates a fatal error that stops the download.
	ErrorTypeCritical ErrorType = iota
	// ErrorTypeWarning indicates a non-fatal error that allows the download to continue.
	ErrorTypeWarning
)

// Error represents a download error with a severity type and descriptive message.
// It implements the error interface for use with standard Go error handling.
type Error struct {
	Type    ErrorType `json:"error_type"`
	Message string    `json:"message"`
}

// Error returns the error message string, implementing the error interface.
func (e *Error) Error() string {
	return e.Message
}
