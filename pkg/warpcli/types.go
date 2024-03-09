package warpcli

import (
	"encoding/json"
)

type Request struct {
	Method  string `json:"method"`
	Message any    `json:"message,omitempty"`
}

type Response struct {
	Ok     bool    `json:"ok"`
	Error  string  `json:"error,omitempty"`
	Update *Update `json:"update,omitempty"`
}

type Update struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
}
