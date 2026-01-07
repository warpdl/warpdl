package nativehost

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
)

// TestReadMessage verifies that messages with 4-byte length prefix are correctly read
func TestReadMessage(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    []byte
		wantErr bool
	}{
		{
			name:    "simple message",
			input:   append([]byte{5, 0, 0, 0}, []byte("hello")...),
			want:    []byte("hello"),
			wantErr: false,
		},
		{
			name:    "empty message",
			input:   []byte{0, 0, 0, 0},
			want:    []byte{},
			wantErr: false,
		},
		{
			name:    "json message",
			input:   append([]byte{8, 0, 0, 0}, []byte(`{"id":1}`)...),
			want:    []byte(`{"id":1}`),
			wantErr: false,
		},
		{
			name:    "incomplete header",
			input:   []byte{5, 0},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "incomplete body",
			input:   append([]byte{10, 0, 0, 0}, []byte("short")...),
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bytes.NewReader(tt.input)
			got, err := ReadMessage(reader)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadMessage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !bytes.Equal(got, tt.want) {
				t.Errorf("ReadMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestWriteMessage verifies that messages are written with correct 4-byte length prefix
func TestWriteMessage(t *testing.T) {
	tests := []struct {
		name    string
		message []byte
		want    []byte
		wantErr bool
	}{
		{
			name:    "simple message",
			message: []byte("hello"),
			want:    append([]byte{5, 0, 0, 0}, []byte("hello")...),
			wantErr: false,
		},
		{
			name:    "empty message",
			message: []byte{},
			want:    []byte{0, 0, 0, 0},
			wantErr: false,
		},
		{
			name:    "json message",
			message: []byte(`{"id":1}`),
			want:    append([]byte{8, 0, 0, 0}, []byte(`{"id":1}`)...),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := WriteMessage(&buf, tt.message)
			if (err != nil) != tt.wantErr {
				t.Errorf("WriteMessage() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !bytes.Equal(buf.Bytes(), tt.want) {
				t.Errorf("WriteMessage() = %v, want %v", buf.Bytes(), tt.want)
			}
		})
	}
}

// TestRoundTrip verifies message can be written and read back correctly
func TestRoundTrip(t *testing.T) {
	messages := [][]byte{
		[]byte("hello world"),
		[]byte(`{"method":"download","message":{"url":"https://example.com/file.zip"}}`),
		[]byte{},
	}

	for _, msg := range messages {
		t.Run(string(msg), func(t *testing.T) {
			var buf bytes.Buffer
			if err := WriteMessage(&buf, msg); err != nil {
				t.Fatalf("WriteMessage() error = %v", err)
			}
			got, err := ReadMessage(&buf)
			if err != nil {
				t.Fatalf("ReadMessage() error = %v", err)
			}
			if !bytes.Equal(got, msg) {
				t.Errorf("RoundTrip failed: got %v, want %v", got, msg)
			}
		})
	}
}

// TestRequest represents a native messaging request from browser
type TestRequest struct {
	ID      int             `json:"id"`
	Method  string          `json:"method"`
	Message json.RawMessage `json:"message,omitempty"`
}

// TestResponse represents a native messaging response to browser
type TestResponse struct {
	ID     int    `json:"id"`
	Ok     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Result any    `json:"result,omitempty"`
}

// TestParseRequest verifies request parsing from JSON
func TestParseRequest(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    *Request
		wantErr bool
	}{
		{
			name:  "download request",
			input: []byte(`{"id":1,"method":"download","message":{"url":"https://example.com"}}`),
			want: &Request{
				ID:      1,
				Method:  "download",
				Message: json.RawMessage(`{"url":"https://example.com"}`),
			},
			wantErr: false,
		},
		{
			name:  "list request without message",
			input: []byte(`{"id":2,"method":"list"}`),
			want: &Request{
				ID:     2,
				Method: "list",
			},
			wantErr: false,
		},
		{
			name:    "invalid json",
			input:   []byte(`{invalid`),
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRequest(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseRequest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got.ID != tt.want.ID || got.Method != tt.want.Method {
				t.Errorf("ParseRequest() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestMakeResponse verifies response creation
func TestMakeResponse(t *testing.T) {
	tests := []struct {
		name   string
		id     int
		result any
		err    error
	}{
		{
			name:   "success response",
			id:     1,
			result: map[string]string{"status": "ok"},
			err:    nil,
		},
		{
			name:   "error response",
			id:     2,
			result: nil,
			err:    io.EOF,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp []byte
			if tt.err != nil {
				resp = MakeErrorResponse(tt.id, tt.err)
			} else {
				resp = MakeSuccessResponse(tt.id, tt.result)
			}

			var r Response
			if err := json.Unmarshal(resp, &r); err != nil {
				t.Fatalf("Failed to unmarshal response: %v", err)
			}

			if r.ID != tt.id {
				t.Errorf("Response ID = %d, want %d", r.ID, tt.id)
			}
			if tt.err != nil && r.Ok {
				t.Errorf("Response Ok = true, want false for error")
			}
			if tt.err == nil && !r.Ok {
				t.Errorf("Response Ok = false, want true for success")
			}
		})
	}
}

// TestMessageTooLarge verifies that oversized messages are rejected
func TestMessageTooLarge(t *testing.T) {
	// Create a message header claiming 1GB size
	header := []byte{0, 0, 0, 64} // 64 << 24 = 1073741824 bytes
	reader := bytes.NewReader(header)
	_, err := ReadMessage(reader)
	if err == nil {
		t.Error("ReadMessage() should reject oversized message")
	}
}
