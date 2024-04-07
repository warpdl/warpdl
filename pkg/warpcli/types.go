package warpcli

import (
	"encoding/json"

	"github.com/warpdl/warpdl/common"
)

type Request struct {
	Method  common.UpdateType `json:"method"`
	Message any               `json:"message,omitempty"`
}

type Response struct {
	Ok     bool    `json:"ok"`
	Error  string  `json:"error,omitempty"`
	Update *Update `json:"update,omitempty"`
}

type Update struct {
	Type    common.UpdateType `json:"type"`
	Message json.RawMessage   `json:"message"`
}
