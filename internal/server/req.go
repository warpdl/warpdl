package server

import (
	"encoding/json"

	"github.com/warpdl/warpdl/common"
)

// Request represents an incoming RPC request from a CLI client.
// It contains the method to invoke and an optional JSON message payload.
type Request struct {
	Method  common.UpdateType `json:"method"`
	Message json.RawMessage   `json:"message,omitempty"`
}

// ParseRequest deserializes a JSON byte slice into a Request struct.
// It returns an error if the JSON is malformed or cannot be unmarshaled.
func ParseRequest(b []byte) (*Request, error) {
	var r Request
	return &r, json.Unmarshal(b, &r)
}
