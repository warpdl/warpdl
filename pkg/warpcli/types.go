package warpcli

import (
	"encoding/json"

	"github.com/warpdl/warpdl/common"
)

// Request represents a JSON-RPC style request sent from the client to the daemon.
// It contains the method name and an optional message payload.
type Request struct {
	Method  common.UpdateType `json:"method"`
	Message any               `json:"message,omitempty"`
}

// Response represents a JSON-RPC style response received from the daemon.
// It indicates success or failure and contains an optional update payload.
type Response struct {
	Ok     bool    `json:"ok"`
	Error  string  `json:"error,omitempty"`
	Update *Update `json:"update,omitempty"`
}

// Update represents an asynchronous update received from the daemon.
// It contains the update type for routing and a raw JSON message payload.
type Update struct {
	Type    common.UpdateType `json:"type"`
	Message json.RawMessage   `json:"message"`
}
