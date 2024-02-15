package server

import "encoding/json"

type Request struct {
	Method  string          `json:"method"`
	Message json.RawMessage `json:"data"`
}

func ParseRequest(b []byte) (*Request, error) {
	var r Request
	return &r, json.Unmarshal(b, &r)
}
