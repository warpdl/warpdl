package server

type ErrorType int

const (
	ErrorTypeCritical = iota
	ErrorTypeWarning
)

type Error struct {
	Type    ErrorType `json:"error_type"`
	Message string    `json:"message"`
}

func (e *Error) Error() string {
	return e.Message
}
