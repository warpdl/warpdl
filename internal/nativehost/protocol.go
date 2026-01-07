// Package nativehost implements the native messaging host protocol for browser extensions.
// It provides stdin/stdout communication using Chrome/Firefox native messaging format:
// 4-byte little-endian length prefix followed by JSON payload.
package nativehost

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"

	"github.com/warpdl/warpdl/common"
)

// MaxMessageSize limits native messaging payloads.
// Browser native messaging has a 1MB limit, but we use common.MaxMessageSize for consistency.
const MaxMessageSize = common.MaxMessageSize

// Request represents an incoming native messaging request from a browser extension.
// It includes an ID for request-response correlation, which the daemon protocol lacks.
type Request struct {
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Message json.RawMessage `json:"message,omitempty"`
}

// Response represents a native messaging response sent back to the browser extension.
type Response struct {
	ID     int    `json:"id"`
	Ok     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Result any    `json:"result,omitempty"`
}

// ReadMessage reads a native messaging format message from the reader.
// Format: 4-byte little-endian length prefix followed by the message bytes.
func ReadMessage(r io.Reader) ([]byte, error) {
	var length uint32
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return nil, err
	}
	if length > uint32(MaxMessageSize) {
		return nil, fmt.Errorf("message too large: %d bytes (max %d)", length, MaxMessageSize)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// WriteMessage writes a message in native messaging format to the writer.
// Format: 4-byte little-endian length prefix followed by the message bytes.
func WriteMessage(w io.Writer, msg []byte) error {
	if len(msg) > MaxMessageSize {
		return fmt.Errorf("message too large: %d bytes (max %d)", len(msg), MaxMessageSize)
	}
	length := uint32(len(msg))
	if err := binary.Write(w, binary.LittleEndian, length); err != nil {
		return err
	}
	_, err := w.Write(msg)
	return err
}

// ParseRequest parses a JSON byte slice into a Request struct.
func ParseRequest(b []byte) (*Request, error) {
	var r Request
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// MakeSuccessResponse creates a JSON-encoded success response.
func MakeSuccessResponse(id int, result any) []byte {
	b, _ := json.Marshal(Response{
		ID:     id,
		Ok:     true,
		Result: result,
	})
	return b
}

// MakeErrorResponse creates a JSON-encoded error response.
func MakeErrorResponse(id int, err error) []byte {
	msg := "unknown error"
	if err != nil {
		msg = err.Error()
	}
	b, _ := json.Marshal(Response{
		ID:    id,
		Ok:    false,
		Error: msg,
	})
	return b
}
