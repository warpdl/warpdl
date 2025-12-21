package server

import (
	"encoding/json"

	"github.com/warpdl/warpdl/common"
)

// Response represents an RPC response sent to CLI clients.
// A successful response has Ok set to true and contains an Update.
// A failed response has Ok set to false and contains an Error message.
type Response struct {
	Ok     bool    `json:"ok"`
	Error  string  `json:"error,omitempty"`
	Update *Update `json:"update,omitempty"`
}

// Update contains the payload of a successful response.
// It wraps the update type and the associated message data.
type Update struct {
	Type    common.UpdateType `json:"type"`
	Message any               `json:"message,omitempty"`
}

// MakeResult creates a JSON-encoded success response with the given update type and result data.
// The returned byte slice is ready to be sent over the connection.
func MakeResult(utype common.UpdateType, res any) []byte {
	b, _ := json.Marshal(Response{
		Ok: true,
		Update: &Update{
			Type:    utype,
			Message: res,
		},
	})
	return b
}

// InitError creates a JSON-encoded error response from the given error.
// If err is nil, it returns an error response with "Unknown" as the message.
func InitError(err error) []byte {
	if err == nil {
		return CreateError("Unknown")
	}
	return CreateError(err.Error())
}

// CreateError creates a JSON-encoded error response with the given error message.
// The returned byte slice is ready to be sent over the connection.
func CreateError(err string) []byte {
	b, _ := json.Marshal(Response{
		Ok:    false,
		Error: err,
	})
	return b
}
