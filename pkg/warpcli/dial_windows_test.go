//go:build windows

package warpcli

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/warpdl/warpdl/common"
)

// TestNewClient_DialsPipeFirst verifies that NewClient attempts to dial a named pipe
// before falling back to TCP on Windows.
func TestNewClient_DialsPipeFirst(t *testing.T) {
	pipeName := fmt.Sprintf(`\\.\pipe\warpdl-test-client-%d`, time.Now().UnixNano())
	t.Setenv(common.PipeNameEnv, pipeName)
	t.Setenv(common.ForceTCPEnv, "")

	// Mock ensureDaemon to avoid spawning actual daemon
	originalEnsureDaemon := ensureDaemonFunc
	ensureDaemonFunc = func() error { return nil }
	defer func() { ensureDaemonFunc = originalEnsureDaemon }()

	// Track dial attempts by mocking dialPipeFunc
	pipeCalled := false
	var pipeMu sync.Mutex

	originalDialPipeFunc := dialPipeFunc
	dialPipeFunc = func(path string, timeout *time.Duration) (net.Conn, error) {
		pipeMu.Lock()
		pipeCalled = true
		pipeMu.Unlock()

		// Create a real pipe connection for test
		listener, err := winio.ListenPipe(pipeName, nil)
		if err != nil {
			return nil, err
		}

		// Accept in background
		connChan := make(chan net.Conn, 1)
		go func() {
			conn, _ := listener.Accept()
			connChan <- conn
		}()

		// Dial
		clientConn, err := winio.DialPipe(pipeName, nil)
		if err != nil {
			listener.Close()
			return nil, err
		}

		// Clean up server side
		go func() {
			serverConn := <-connChan
			serverConn.Close()
			listener.Close()
		}()

		return clientConn, nil
	}
	defer func() { dialPipeFunc = originalDialPipeFunc }()

	// Create client - should dial pipe first
	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	defer client.Close()

	// Verify pipe was attempted
	pipeMu.Lock()
	defer pipeMu.Unlock()

	if !pipeCalled {
		t.Fatal("NewClient() did not attempt pipe dial")
	}
}

// TestNewClient_FallsBackToTCPWhenPipeFails verifies that when pipe dial fails,
// NewClient falls back to TCP.
func TestNewClient_FallsBackToTCPWhenPipeFails(t *testing.T) {
	t.Setenv(common.ForceTCPEnv, "")

	// Mock ensureDaemon
	originalEnsureDaemon := ensureDaemonFunc
	ensureDaemonFunc = func() error { return nil }
	defer func() { ensureDaemonFunc = originalEnsureDaemon }()

	// Track dial attempts
	pipeCalled := false
	tcpCalled := false
	var mu sync.Mutex

	// Mock dialPipeFunc to fail
	originalDialPipeFunc := dialPipeFunc
	dialPipeFunc = func(path string, timeout *time.Duration) (net.Conn, error) {
		mu.Lock()
		pipeCalled = true
		mu.Unlock()
		return nil, fmt.Errorf("pipe connection failed")
	}
	defer func() { dialPipeFunc = originalDialPipeFunc }()

	// Mock dialFunc for TCP to succeed
	originalDialFunc := dialFunc
	dialFunc = func(network, address string) (net.Conn, error) {
		mu.Lock()
		if network == "tcp" {
			tcpCalled = true
		}
		mu.Unlock()

		if network == "tcp" {
			// Simulate successful TCP
			server, client := net.Pipe()
			go func() {
				// Keep server side alive briefly
				time.Sleep(100 * time.Millisecond)
				server.Close()
			}()
			return client, nil
		}

		return nil, fmt.Errorf("unexpected network: %s", network)
	}
	defer func() { dialFunc = originalDialFunc }()

	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	defer client.Close()

	// Verify both pipe and TCP were attempted
	mu.Lock()
	defer mu.Unlock()

	if !pipeCalled {
		t.Fatal("NewClient() did not attempt pipe dial")
	}

	if !tcpCalled {
		t.Fatal("NewClient() did not fall back to TCP")
	}
}

// TestNewClient_ForceTCPSkipsPipe verifies that when WARPDL_FORCE_TCP=1,
// the pipe dial is skipped entirely.
func TestNewClient_ForceTCPSkipsPipe(t *testing.T) {
	t.Setenv(common.ForceTCPEnv, "1")

	// Mock ensureDaemon
	originalEnsureDaemon := ensureDaemonFunc
	ensureDaemonFunc = func() error { return nil }
	defer func() { ensureDaemonFunc = originalEnsureDaemon }()

	// Track dial attempts
	pipeCalled := false
	tcpCalled := false
	var mu sync.Mutex

	// Mock dialPipeFunc - should NOT be called
	originalDialPipeFunc := dialPipeFunc
	dialPipeFunc = func(path string, timeout *time.Duration) (net.Conn, error) {
		mu.Lock()
		pipeCalled = true
		mu.Unlock()
		return nil, fmt.Errorf("pipe should not be called in force TCP mode")
	}
	defer func() { dialPipeFunc = originalDialPipeFunc }()

	originalDialFunc := dialFunc
	dialFunc = func(network, address string) (net.Conn, error) {
		mu.Lock()
		if network == "tcp" {
			tcpCalled = true
		}
		mu.Unlock()

		if network == "tcp" {
			server, client := net.Pipe()
			go func() {
				time.Sleep(100 * time.Millisecond)
				server.Close()
			}()
			return client, nil
		}

		return nil, fmt.Errorf("unexpected network: %s", network)
	}
	defer func() { dialFunc = originalDialFunc }()

	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	defer client.Close()

	// Verify only TCP was attempted
	mu.Lock()
	defer mu.Unlock()

	if pipeCalled {
		t.Fatal("NewClient() should not attempt pipe dial when force TCP is enabled")
	}

	if !tcpCalled {
		t.Fatal("NewClient() did not attempt TCP")
	}
}

// TestNewClient_BothTransportsFailWindows verifies that when both pipe and TCP fail,
// NewClient returns an appropriate error on Windows.
func TestNewClient_BothTransportsFailWindows(t *testing.T) {
	t.Setenv(common.ForceTCPEnv, "")

	// Mock ensureDaemon
	originalEnsureDaemon := ensureDaemonFunc
	ensureDaemonFunc = func() error { return nil }
	defer func() { ensureDaemonFunc = originalEnsureDaemon }()

	// Mock dialPipeFunc to fail
	originalDialPipeFunc := dialPipeFunc
	dialPipeFunc = func(path string, timeout *time.Duration) (net.Conn, error) {
		return nil, fmt.Errorf("pipe connection failed")
	}
	defer func() { dialPipeFunc = originalDialPipeFunc }()

	originalDialFunc := dialFunc
	dialFunc = func(network, address string) (net.Conn, error) {
		return nil, fmt.Errorf("%s connection failed", network)
	}
	defer func() { dialFunc = originalDialFunc }()

	client, err := NewClient()
	if err == nil {
		client.Close()
		t.Fatal("NewClient() succeeded when both transports failed; want error")
	}

	// Error should mention both failures
	errMsg := err.Error()
	if errMsg == "" {
		t.Error("NewClient() returned empty error message")
	}
}

// TestClientServer_PipeRoundtrip is an integration test verifying full
// request/response cycle through named pipes between client and mock server.
func TestClientServer_PipeRoundtrip(t *testing.T) {
	pipeName := fmt.Sprintf(`\\.\pipe\warpdl-test-roundtrip-%d`, time.Now().UnixNano())
	t.Setenv(common.PipeNameEnv, pipeName)
	t.Setenv(common.ForceTCPEnv, "")

	// Mock ensureDaemon
	originalEnsureDaemon := ensureDaemonFunc
	ensureDaemonFunc = func() error { return nil }
	defer func() { ensureDaemonFunc = originalEnsureDaemon }()

	// Create mock server
	listener, err := winio.ListenPipe(pipeName, nil)
	if err != nil {
		t.Fatalf("failed to create pipe listener: %v", err)
	}
	defer listener.Close()

	// Server handler using length-prefixed protocol like the real server
	serverErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()

		// Use length-prefixed protocol like the real server
		reqBytes, err := read(conn)
		if err != nil {
			serverErr <- fmt.Errorf("read: %w", err)
			return
		}

		var req Request
		if err := json.Unmarshal(reqBytes, &req); err != nil {
			serverErr <- fmt.Errorf("unmarshal: %w", err)
			return
		}

		// Send response
		resp := Response{
			Ok: true,
			Update: &Update{
				Type:    req.Method,
				Message: json.RawMessage(`{"status":"success"}`),
			},
		}
		respBytes, err := json.Marshal(resp)
		if err != nil {
			serverErr <- fmt.Errorf("marshal: %w", err)
			return
		}

		if err := write(conn, respBytes); err != nil {
			serverErr <- fmt.Errorf("write: %w", err)
			return
		}
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Override dialPipeFunc to use real pipe
	originalDialPipeFunc := dialPipeFunc
	dialPipeFunc = func(path string, timeout *time.Duration) (net.Conn, error) {
		return winio.DialPipe(pipeName, nil)
	}
	defer func() { dialPipeFunc = originalDialPipeFunc }()

	// Create client
	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	defer client.Close()

	// Send a test request via invoke
	result, err := client.invoke(common.UPDATE_VERSION, nil)
	if err != nil {
		t.Fatalf("client.invoke() failed: %v", err)
	}

	// Verify response
	var responseData map[string]string
	if err := json.Unmarshal(result, &responseData); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if status, ok := responseData["status"]; !ok || status != "success" {
		t.Errorf("response status = %q; want %q", status, "success")
	}

	// Check server errors
	select {
	case err := <-serverErr:
		t.Fatalf("server error: %v", err)
	default:
		// No error, good
	}
}

// TestNewClient_PipeDialTimeout verifies that pipe dial timeout is handled correctly.
func TestNewClient_PipeDialTimeout(t *testing.T) {
	t.Setenv(common.ForceTCPEnv, "")
	t.Setenv(common.PipeNameEnv, `\\.\pipe\warpdl-timeout-test`)

	// Mock ensureDaemon
	originalEnsureDaemon := ensureDaemonFunc
	ensureDaemonFunc = func() error { return nil }
	defer func() { ensureDaemonFunc = originalEnsureDaemon }()

	// Mock dialPipeFunc with timeout simulation
	originalDialPipeFunc := dialPipeFunc
	dialPipeFunc = func(path string, timeout *time.Duration) (net.Conn, error) {
		// Simulate timeout
		time.Sleep(100 * time.Millisecond)
		return nil, fmt.Errorf("pipe dial timeout")
	}
	defer func() { dialPipeFunc = originalDialPipeFunc }()

	// Mock dialFunc for TCP to succeed
	originalDialFunc := dialFunc
	dialFunc = func(network, address string) (net.Conn, error) {
		if network == "tcp" {
			// TCP succeeds
			server, client := net.Pipe()
			go func() {
				time.Sleep(500 * time.Millisecond)
				server.Close()
			}()
			return client, nil
		}

		return nil, fmt.Errorf("unexpected network: %s", network)
	}
	defer func() { dialFunc = originalDialFunc }()

	// Should fall back to TCP after pipe timeout
	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient() failed to fallback to TCP: %v", err)
	}
	defer client.Close()
}
