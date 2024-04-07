package server

import (
	"encoding/json"

	"github.com/warpdl/warpdl/common"
)

type Request struct {
	Method  common.UpdateType `json:"method"`
	Message json.RawMessage   `json:"message,omitempty"`
}

func ParseRequest(b []byte) (*Request, error) {
	var r Request
	return &r, json.Unmarshal(b, &r)
}
