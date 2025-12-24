package warpcli

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"
)

// TestClientServer_TCPRoundtrip verifies full client-server communication over TCP.
// This test simulates a daemon server listening on TCP and a client connecting to it.
func TestClientServer_TCPRoundtrip(t *testing.T) {
	// Create TCP listener on dynamic port
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to create TCP listener: %v", err)
	}
	defer listener.Close()

	// Extract the dynamically assigned port
	port := listener.Addr().(*net.TCPAddr).Port
	t.Logf("TCP listener started on port %d", port)

	// Set environment variables to force TCP connection
	t.Setenv("WARPDL_TCP_PORT", fmt.Sprintf("%d", port))
	t.Setenv("WARPDL_FORCE_TCP", "1")

	// Mock ensureDaemonFunc to skip daemon spawning
	oldEnsure := ensureDaemonFunc
	ensureDaemonFunc = func() error { return nil }
	defer func() { ensureDaemonFunc = oldEnsure }()

	// Mock dialFunc to connect via TCP instead of Unix socket
	oldDial := dialFunc
	dialFunc = func(network, address string) (net.Conn, error) {
		// When WARPDL_FORCE_TCP=1, dial TCP instead
		if forceTCP() {
			return net.Dial("tcp", tcpAddress())
		}
		return net.Dial(network, address)
	}
	defer func() { dialFunc = oldDial }()

	// Start server goroutine to handle connections
	serverReady := make(chan struct{})
	serverErr := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		close(serverReady)

		conn, err := listener.Accept()
		if err != nil {
			serverErr <- fmt.Errorf("accept failed: %w", err)
			return
		}
		defer conn.Close()

		// Read request
		reqBytes, err := read(conn)
		if err != nil {
			serverErr <- fmt.Errorf("read request failed: %w", err)
			return
		}

		var req Request
		if err := json.Unmarshal(reqBytes, &req); err != nil {
			serverErr <- fmt.Errorf("unmarshal request failed: %w", err)
			return
		}

		// Echo response based on request method
		var respMsg json.RawMessage
		switch req.Method {
		case common.UPDATE_DOWNLOAD:
			respMsg, _ = json.Marshal(common.DownloadResponse{
				DownloadId:        "tcp-test-id",
				FileName:          "test-file.bin",
				DownloadDirectory: "/tmp",
			})
		case common.UPDATE_LIST:
			respMsg, _ = json.Marshal(common.ListResponse{
				Items: []*warplib.Item{
					{Hash: "test-hash-1", Name: "test-file-1.bin", Url: "http://example.com/file1"},
				},
			})
		default:
			respMsg = json.RawMessage(`{}`)
		}

		resp := Response{
			Ok: true,
			Update: &Update{
				Type:    req.Method,
				Message: respMsg,
			},
		}

		respBytes, _ := json.Marshal(resp)
		if err := write(conn, respBytes); err != nil {
			serverErr <- fmt.Errorf("write response failed: %w", err)
			return
		}
	}()

	// Wait for server to be ready
	<-serverReady

	// Create client (should connect via TCP)
	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	defer client.Close()

	// Test Download request
	downloadResp, err := client.Download("http://example.com/file", "test-file.bin", "/tmp", nil)
	if err != nil {
		t.Fatalf("Download() failed: %v", err)
	}

	if downloadResp.DownloadId != "tcp-test-id" {
		t.Errorf("unexpected DownloadId: got %q, want %q", downloadResp.DownloadId, "tcp-test-id")
	}
	if downloadResp.FileName != "test-file.bin" {
		t.Errorf("unexpected FileName: got %q, want %q", downloadResp.FileName, "test-file.bin")
	}

	// Wait for server goroutine to finish
	wg.Wait()

	// Check for server errors
	select {
	case err := <-serverErr:
		t.Fatalf("server error: %v", err)
	default:
	}
}

// TestClientServer_FallbackScenario verifies TCP fallback when Unix socket is unavailable.
// This simulates the scenario where the Unix socket path doesn't exist or is inaccessible,
// and the client falls back to TCP.
func TestClientServer_FallbackScenario(t *testing.T) {
	// Create TCP listener on dynamic port
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to create TCP listener: %v", err)
	}
	defer listener.Close()

	// Extract the dynamically assigned port
	port := listener.Addr().(*net.TCPAddr).Port
	t.Logf("TCP listener started on port %d", port)

	// Set Unix socket path to non-existent location
	t.Setenv("WARPDL_SOCKET_PATH", "/tmp/nonexistent-warpdl-test-socket-12345.sock")
	// Set TCP port to the test server
	t.Setenv("WARPDL_TCP_PORT", fmt.Sprintf("%d", port))

	// Mock ensureDaemonFunc to return nil (simulate daemon is "running" on TCP)
	oldEnsure := ensureDaemonFunc
	ensureDaemonFunc = func() error { return nil }
	defer func() { ensureDaemonFunc = oldEnsure }()

	// Mock dialFunc to implement fallback logic:
	// 1. Try Unix socket first (will fail)
	// 2. Fall back to TCP
	oldDial := dialFunc
	dialFunc = func(network, address string) (net.Conn, error) {
		// First attempt: Unix socket (should fail)
		if network == "unix" {
			conn, err := net.Dial(network, address)
			if err != nil {
				// Fallback to TCP
				debugLog("Unix socket dial failed, falling back to TCP: %v", err)
				return net.Dial("tcp", tcpAddress())
			}
			return conn, nil
		}
		return net.Dial(network, address)
	}
	defer func() { dialFunc = oldDial }()

	// Start server goroutine
	serverReady := make(chan struct{})
	serverErr := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		close(serverReady)

		conn, err := listener.Accept()
		if err != nil {
			serverErr <- fmt.Errorf("accept failed: %w", err)
			return
		}
		defer conn.Close()

		// Read request
		reqBytes, err := read(conn)
		if err != nil {
			serverErr <- fmt.Errorf("read request failed: %w", err)
			return
		}

		var req Request
		if err := json.Unmarshal(reqBytes, &req); err != nil {
			serverErr <- fmt.Errorf("unmarshal request failed: %w", err)
			return
		}

		// Create response
		var respMsg json.RawMessage
		if req.Method == common.UPDATE_LIST {
			respMsg, _ = json.Marshal(common.ListResponse{
				Items: []*warplib.Item{
					{Hash: "fallback-1", Name: "fallback-file.bin", Url: "http://example.com/fallback"},
				},
			})
		} else {
			respMsg = json.RawMessage(`{}`)
		}

		resp := Response{
			Ok: true,
			Update: &Update{
				Type:    req.Method,
				Message: respMsg,
			},
		}

		respBytes, _ := json.Marshal(resp)
		if err := write(conn, respBytes); err != nil {
			serverErr <- fmt.Errorf("write response failed: %w", err)
			return
		}
	}()

	// Wait for server to be ready
	<-serverReady

	// Give server a moment to stabilize
	time.Sleep(50 * time.Millisecond)

	// Create client (should fallback to TCP)
	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	defer client.Close()

	// Test List request to verify TCP connection works
	listResp, err := client.List(nil)
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}

	if listResp.Items == nil || len(listResp.Items) == 0 {
		t.Fatal("expected non-empty list response")
	}
	if listResp.Items[0].Hash != "fallback-1" {
		t.Errorf("unexpected Hash: got %q, want %q", listResp.Items[0].Hash, "fallback-1")
	}

	// Wait for server goroutine to finish
	wg.Wait()

	// Check for server errors
	select {
	case err := <-serverErr:
		t.Fatalf("server error: %v", err)
	default:
	}
}

// TestClientServer_TCPProgressUpdates verifies that progress updates work over TCP.
func TestClientServer_TCPProgressUpdates(t *testing.T) {
	// Create TCP listener on dynamic port
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to create TCP listener: %v", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	t.Setenv("WARPDL_TCP_PORT", fmt.Sprintf("%d", port))
	t.Setenv("WARPDL_FORCE_TCP", "1")

	// Mock functions
	oldEnsure := ensureDaemonFunc
	ensureDaemonFunc = func() error { return nil }
	defer func() { ensureDaemonFunc = oldEnsure }()

	oldDial := dialFunc
	dialFunc = func(network, address string) (net.Conn, error) {
		if forceTCP() {
			return net.Dial("tcp", tcpAddress())
		}
		return net.Dial(network, address)
	}
	defer func() { dialFunc = oldDial }()

	// Start server
	serverReady := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		close(serverReady)

		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Send initial response
		resp := Response{
			Ok: true,
			Update: &Update{
				Type:    common.UPDATE_DOWNLOADING,
				Message: json.RawMessage(`{"action":"download_progress","value":50}`),
			},
		}
		respBytes, _ := json.Marshal(resp)
		_ = write(conn, respBytes)

		// Send completion update
		time.Sleep(100 * time.Millisecond)
		resp.Update.Message = json.RawMessage(`{"action":"download_complete","value":100}`)
		respBytes, _ = json.Marshal(resp)
		_ = write(conn, respBytes)
	}()

	<-serverReady

	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}

	progressReceived := false
	completeReceived := false

	client.AddHandler(common.UPDATE_DOWNLOADING, NewDownloadingHandler(common.DownloadProgress, func(dr *common.DownloadingResponse) error {
		progressReceived = true
		if dr.Value != 50 {
			t.Errorf("unexpected progress value: got %d, want 50", dr.Value)
		}
		return nil
	}))

	client.AddHandler(common.UPDATE_DOWNLOADING, NewDownloadingHandler(common.DownloadComplete, func(dr *common.DownloadingResponse) error {
		completeReceived = true
		if dr.Value != 100 {
			t.Errorf("unexpected complete value: got %d, want 100", dr.Value)
		}
		// Disconnect after receiving completion
		return ErrDisconnect
	}))

	// Start listening (blocks until ErrDisconnect)
	if err := client.Listen(); err != nil {
		t.Fatalf("Listen() failed: %v", err)
	}

	wg.Wait()

	if !progressReceived {
		t.Error("progress update was not received")
	}
	if !completeReceived {
		t.Error("complete update was not received")
	}
}
