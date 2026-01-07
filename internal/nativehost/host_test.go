package nativehost

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/warpdl/warpdl/common"
)

// mockClient implements a mock warpcli.Client for testing
type mockClient struct {
	downloadResponse *common.DownloadResponse
	listResponse     *common.ListResponse
	versionResponse  *common.VersionResponse
	err              error
}

func (m *mockClient) Download(url, fileName, downloadDirectory string, opts *DownloadOptions) (*common.DownloadResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.downloadResponse, nil
}

func (m *mockClient) List(opts *ListOptions) (*common.ListResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.listResponse, nil
}

func (m *mockClient) GetDaemonVersion() (*common.VersionResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.versionResponse, nil
}

func (m *mockClient) StopDownload(downloadId string) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	return true, nil
}

func (m *mockClient) Resume(downloadId string, opts *ResumeOptions) (*common.ResumeResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &common.ResumeResponse{FileName: "test.zip"}, nil
}

func (m *mockClient) Flush(downloadId string) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	return true, nil
}

func (m *mockClient) Close() error {
	return nil
}

// TestHostHandleRequest verifies request handling
func TestHostHandleRequest(t *testing.T) {
	tests := []struct {
		name    string
		request Request
		client  *mockClient
		wantOk  bool
	}{
		{
			name: "version request",
			request: Request{
				ID:     1,
				Method: "version",
			},
			client: &mockClient{
				versionResponse: &common.VersionResponse{
					Version: "1.0.0",
					Commit:  "abc123",
				},
			},
			wantOk: true,
		},
		{
			name: "list request",
			request: Request{
				ID:     2,
				Method: "list",
			},
			client: &mockClient{
				listResponse: &common.ListResponse{},
			},
			wantOk: true,
		},
		{
			name: "download request",
			request: Request{
				ID:      3,
				Method:  "download",
				Message: json.RawMessage(`{"url":"https://example.com/file.zip","fileName":"file.zip","downloadDirectory":"/tmp"}`),
			},
			client: &mockClient{
				downloadResponse: &common.DownloadResponse{
					DownloadId: "test-123",
				},
			},
			wantOk: true,
		},
		{
			name: "stop request",
			request: Request{
				ID:      4,
				Method:  "stop",
				Message: json.RawMessage(`{"downloadId":"test-123"}`),
			},
			client: &mockClient{},
			wantOk: true,
		},
		{
			name: "unknown method",
			request: Request{
				ID:     5,
				Method: "invalid_method",
			},
			client: &mockClient{},
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := &Host{client: tt.client}
			resp := host.handleRequest(&tt.request)

			var r Response
			if err := json.Unmarshal(resp, &r); err != nil {
				t.Fatalf("Failed to unmarshal response: %v", err)
			}

			if r.ID != tt.request.ID {
				t.Errorf("Response ID = %d, want %d", r.ID, tt.request.ID)
			}
			if r.Ok != tt.wantOk {
				t.Errorf("Response Ok = %v, want %v (error: %s)", r.Ok, tt.wantOk, r.Error)
			}
		})
	}
}

// TestHostProcessMessages verifies end-to-end message processing
func TestHostProcessMessages(t *testing.T) {
	client := &mockClient{
		versionResponse: &common.VersionResponse{
			Version: "1.0.0",
			Commit:  "abc123",
		},
	}

	// Create a request
	req := Request{ID: 1, Method: "version"}
	reqJSON, _ := json.Marshal(req)

	// Create input with length prefix
	var input bytes.Buffer
	if err := WriteMessage(&input, reqJSON); err != nil {
		t.Fatalf("Failed to write message: %v", err)
	}

	// Create output buffer
	var output bytes.Buffer

	host := &Host{
		client: client,
		stdin:  &input,
		stdout: &output,
	}

	// Process one message
	if err := host.processOneMessage(); err != nil {
		t.Fatalf("processOneMessage failed: %v", err)
	}

	// Read response
	respData, err := ReadMessage(&output)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	var resp Response
	if err := json.Unmarshal(respData, &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if !resp.Ok {
		t.Errorf("Response not ok: %s", resp.Error)
	}
	if resp.ID != 1 {
		t.Errorf("Response ID = %d, want 1", resp.ID)
	}
}

// TestHostEOFHandling verifies graceful EOF handling
func TestHostEOFHandling(t *testing.T) {
	host := &Host{
		client: &mockClient{},
		stdin:  bytes.NewReader(nil), // Empty reader triggers EOF
		stdout: &bytes.Buffer{},
	}

	err := host.processOneMessage()
	if err != io.EOF {
		t.Errorf("Expected EOF, got: %v", err)
	}
}

// TestDownloadParams verifies download parameter parsing
func TestDownloadParams(t *testing.T) {
	tests := []struct {
		name    string
		message json.RawMessage
		wantErr bool
	}{
		{
			name:    "valid params",
			message: json.RawMessage(`{"url":"https://example.com","fileName":"test.zip","downloadDirectory":"/tmp"}`),
			wantErr: false,
		},
		{
			name:    "missing url",
			message: json.RawMessage(`{"fileName":"test.zip","downloadDirectory":"/tmp"}`),
			wantErr: true,
		},
		{
			name:    "invalid json",
			message: json.RawMessage(`{invalid`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p DownloadParams
			err := json.Unmarshal(tt.message, &p)
			if err != nil {
				if !tt.wantErr {
					t.Errorf("Unexpected error: %v", err)
				}
				return
			}
			if tt.wantErr && p.URL == "" {
				// Missing required field treated as error
				return
			}
			if p.URL == "" && !tt.wantErr {
				t.Error("URL should not be empty for valid params")
			}
		})
	}
}

// TestResumeParams verifies resume parameter parsing
func TestResumeParams(t *testing.T) {
	msg := json.RawMessage(`{"downloadId":"test-123"}`)
	var p ResumeParams
	if err := json.Unmarshal(msg, &p); err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}
	if p.DownloadID != "test-123" {
		t.Errorf("DownloadID = %s, want test-123", p.DownloadID)
	}
}

// TestStopParams verifies stop parameter parsing
func TestStopParams(t *testing.T) {
	msg := json.RawMessage(`{"downloadId":"test-456"}`)
	var p StopParams
	if err := json.Unmarshal(msg, &p); err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}
	if p.DownloadID != "test-456" {
		t.Errorf("DownloadID = %s, want test-456", p.DownloadID)
	}
}

// TestFlushParams verifies flush parameter parsing
func TestFlushParams(t *testing.T) {
	msg := json.RawMessage(`{"downloadId":"test-789"}`)
	var p FlushParams
	if err := json.Unmarshal(msg, &p); err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}
	if p.DownloadID != "test-789" {
		t.Errorf("DownloadID = %s, want test-789", p.DownloadID)
	}
}

// TestListParams verifies list parameter parsing
func TestListParams(t *testing.T) {
	msg := json.RawMessage(`{"includeHidden":true}`)
	var p ListParams
	if err := json.Unmarshal(msg, &p); err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}
	if !p.IncludeHidden {
		t.Error("IncludeHidden should be true")
	}
}

// TestResumeRequest verifies resume request handling
func TestResumeRequest(t *testing.T) {
	client := &mockClient{}
	host := &Host{client: client}

	req := &Request{
		ID:      1,
		Method:  "resume",
		Message: json.RawMessage(`{"downloadId":"test-123"}`),
	}
	resp := host.handleRequest(req)

	var r Response
	if err := json.Unmarshal(resp, &r); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if !r.Ok {
		t.Errorf("Response not ok: %s", r.Error)
	}
}

// TestFlushRequest verifies flush request handling
func TestFlushRequest(t *testing.T) {
	client := &mockClient{}
	host := &Host{client: client}

	req := &Request{
		ID:      1,
		Method:  "flush",
		Message: json.RawMessage(`{"downloadId":"test-123"}`),
	}
	resp := host.handleRequest(req)

	var r Response
	if err := json.Unmarshal(resp, &r); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if !r.Ok {
		t.Errorf("Response not ok: %s", r.Error)
	}
}

// TestInvalidDownloadParams verifies error handling for invalid download params
func TestInvalidDownloadParams(t *testing.T) {
	client := &mockClient{}
	host := &Host{client: client}

	req := &Request{
		ID:      1,
		Method:  "download",
		Message: json.RawMessage(`{invalid json`),
	}
	resp := host.handleRequest(req)

	var r Response
	if err := json.Unmarshal(resp, &r); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if r.Ok {
		t.Error("Response should not be ok for invalid params")
	}
}

// TestMissingDownloadURL verifies error when URL is missing
func TestMissingDownloadURL(t *testing.T) {
	client := &mockClient{}
	host := &Host{client: client}

	req := &Request{
		ID:      1,
		Method:  "download",
		Message: json.RawMessage(`{"fileName":"test.zip"}`),
	}
	resp := host.handleRequest(req)

	var r Response
	if err := json.Unmarshal(resp, &r); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if r.Ok {
		t.Error("Response should not be ok when URL is missing")
	}
	if r.Error != "url is required" {
		t.Errorf("Error = %s, want 'url is required'", r.Error)
	}
}

// TestMissingStopDownloadID verifies error when downloadId is missing for stop
func TestMissingStopDownloadID(t *testing.T) {
	client := &mockClient{}
	host := &Host{client: client}

	req := &Request{
		ID:      1,
		Method:  "stop",
		Message: json.RawMessage(`{}`),
	}
	resp := host.handleRequest(req)

	var r Response
	if err := json.Unmarshal(resp, &r); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if r.Ok {
		t.Error("Response should not be ok when downloadId is missing")
	}
}

// TestMissingResumeDownloadID verifies error when downloadId is missing for resume
func TestMissingResumeDownloadID(t *testing.T) {
	client := &mockClient{}
	host := &Host{client: client}

	req := &Request{
		ID:      1,
		Method:  "resume",
		Message: json.RawMessage(`{}`),
	}
	resp := host.handleRequest(req)

	var r Response
	if err := json.Unmarshal(resp, &r); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if r.Ok {
		t.Error("Response should not be ok when downloadId is missing")
	}
}

// TestMissingFlushDownloadID verifies error when downloadId is missing for flush
func TestMissingFlushDownloadID(t *testing.T) {
	client := &mockClient{}
	host := &Host{client: client}

	req := &Request{
		ID:      1,
		Method:  "flush",
		Message: json.RawMessage(`{}`),
	}
	resp := host.handleRequest(req)

	var r Response
	if err := json.Unmarshal(resp, &r); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if r.Ok {
		t.Error("Response should not be ok when downloadId is missing")
	}
}

// TestInvalidListParams verifies error handling for invalid list params
func TestInvalidListParams(t *testing.T) {
	client := &mockClient{}
	host := &Host{client: client}

	req := &Request{
		ID:      1,
		Method:  "list",
		Message: json.RawMessage(`{invalid`),
	}
	resp := host.handleRequest(req)

	var r Response
	if err := json.Unmarshal(resp, &r); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if r.Ok {
		t.Error("Response should not be ok for invalid params")
	}
}

// TestInvalidStopParams verifies error handling for invalid stop params
func TestInvalidStopParams(t *testing.T) {
	client := &mockClient{}
	host := &Host{client: client}

	req := &Request{
		ID:      1,
		Method:  "stop",
		Message: json.RawMessage(`{invalid`),
	}
	resp := host.handleRequest(req)

	var r Response
	if err := json.Unmarshal(resp, &r); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if r.Ok {
		t.Error("Response should not be ok for invalid params")
	}
}

// TestInvalidResumeParams verifies error handling for invalid resume params
func TestInvalidResumeParams(t *testing.T) {
	client := &mockClient{}
	host := &Host{client: client}

	req := &Request{
		ID:      1,
		Method:  "resume",
		Message: json.RawMessage(`{invalid`),
	}
	resp := host.handleRequest(req)

	var r Response
	if err := json.Unmarshal(resp, &r); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if r.Ok {
		t.Error("Response should not be ok for invalid params")
	}
}

// TestInvalidFlushParams verifies error handling for invalid flush params
func TestInvalidFlushParams(t *testing.T) {
	client := &mockClient{}
	host := &Host{client: client}

	req := &Request{
		ID:      1,
		Method:  "flush",
		Message: json.RawMessage(`{invalid`),
	}
	resp := host.handleRequest(req)

	var r Response
	if err := json.Unmarshal(resp, &r); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if r.Ok {
		t.Error("Response should not be ok for invalid params")
	}
}

// TestClientError verifies error propagation from client
func TestClientError(t *testing.T) {
	client := &mockClient{err: io.EOF}
	host := &Host{client: client}

	req := &Request{
		ID:     1,
		Method: "version",
	}
	resp := host.handleRequest(req)

	var r Response
	if err := json.Unmarshal(resp, &r); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
	if r.Ok {
		t.Error("Response should not be ok when client returns error")
	}
}

// TestHostRun verifies the Run method
func TestHostRun(t *testing.T) {
	client := &mockClient{
		versionResponse: &common.VersionResponse{Version: "1.0.0"},
	}

	// Create input with single request then EOF
	req := Request{ID: 1, Method: "version"}
	reqJSON, _ := json.Marshal(req)
	var input bytes.Buffer
	if err := WriteMessage(&input, reqJSON); err != nil {
		t.Fatalf("Failed to write message: %v", err)
	}

	var output bytes.Buffer
	host := &Host{
		client: client,
		stdin:  &input,
		stdout: &output,
	}

	// Run should return nil when input is exhausted (EOF)
	if err := host.Run(); err != nil {
		t.Errorf("Run() returned error: %v", err)
	}
}

// TestNewHost verifies NewHost creates a host with os.Stdin/Stdout
func TestNewHost(t *testing.T) {
	host := NewHost(&mockClient{})
	if host == nil {
		t.Fatal("NewHost returned nil")
	}
	if host.client == nil {
		t.Error("Host client is nil")
	}
}

// TestMultipleMessages verifies processing multiple sequential messages
func TestMultipleMessages(t *testing.T) {
	client := &mockClient{
		versionResponse: &common.VersionResponse{Version: "1.0.0"},
		listResponse:    &common.ListResponse{},
	}

	// Create multiple requests
	var input bytes.Buffer
	for i := 1; i <= 3; i++ {
		req := Request{ID: i, Method: "version"}
		reqJSON, _ := json.Marshal(req)
		if err := WriteMessage(&input, reqJSON); err != nil {
			t.Fatalf("Failed to write message %d: %v", i, err)
		}
	}

	var output bytes.Buffer
	host := &Host{
		client: client,
		stdin:  &input,
		stdout: &output,
	}

	// Process all messages
	for i := 1; i <= 3; i++ {
		if err := host.processOneMessage(); err != nil {
			t.Fatalf("processOneMessage %d failed: %v", i, err)
		}
	}

	// Read all responses
	for i := 1; i <= 3; i++ {
		respData, err := ReadMessage(&output)
		if err != nil {
			t.Fatalf("Failed to read response %d: %v", i, err)
		}

		var resp Response
		if err := json.Unmarshal(respData, &resp); err != nil {
			t.Fatalf("Failed to unmarshal response %d: %v", i, err)
		}
		if resp.ID != i {
			t.Errorf("Response %d ID = %d, want %d", i, resp.ID, i)
		}
	}
}
