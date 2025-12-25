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

// TestDialPipe_Success verifies that the client can successfully dial an existing named pipe.
func TestDialPipe_Success(t *testing.T) {
	pipeName := fmt.Sprintf(`\\.\pipe\warpdl-test-dial-%d`, time.Now().UnixNano())

	// Create a test pipe listener
	listener, err := winio.ListenPipe(pipeName, nil)
	if err != nil {
		t.Fatalf("failed to create test pipe listener: %v", err)
	}
	defer listener.Close()

	// Accept connection in background
	errChan := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			errChan <- err
			return
		}
		conn.Close()
	}()

	// Give listener time to start
	time.Sleep(50 * time.Millisecond)

	// Dial the pipe
	conn, err := winio.DialPipe(pipeName, nil)
	if err != nil {
		t.Fatalf("DialPipe() failed: %v", err)
	}
	defer conn.Close()

	// Verify no server errors
	select {
	case err := <-errChan:
		if err != nil {
			t.Errorf("listener.Accept() error: %v", err)
		}
	case <-time.After(1 * time.Second):
		// Timeout is ok, connection was successful
	}
}

// TestDialPipe_Timeout verifies that dialing a nonexistent pipe times out appropriately.
func TestDialPipe_Timeout(t *testing.T) {
	nonexistentPipe := `\\.\pipe\warpdl-nonexistent-pipe-12345`

	// Set a short timeout
	timeout := 500 * time.Millisecond
	start := time.Now()

	conn, err := winio.DialPipe(nonexistentPipe, &timeout)
	elapsed := time.Since(start)

	if err == nil {
		conn.Close()
		t.Fatal("DialPipe() succeeded on nonexistent pipe; want error")
	}

	// Verify timeout occurred
	if elapsed < timeout {
		t.Errorf("DialPipe() returned too quickly: %v; want at least %v", elapsed, timeout)
	}

	// Verify timeout didn't exceed reasonable bounds (timeout + 1 second grace)
	maxDuration := timeout + 1*time.Second
	if elapsed > maxDuration {
		t.Errorf("DialPipe() took too long: %v; want less than %v", elapsed, maxDuration)
	}
}

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

	// Track dial attempts
	dialAttempts := make([]string, 0)
	var dialMu sync.Mutex

	originalDialFunc := dialFunc
	dialFunc = func(network, address string) (net.Conn, error) {
		dialMu.Lock()
		dialAttempts = append(dialAttempts, network)
		dialMu.Unlock()

		// Simulate pipe dial by checking network type
		if network == "pipe" {
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

		return nil, fmt.Errorf("unexpected network type: %s", network)
	}
	defer func() { dialFunc = originalDialFunc }()

	// Create client - should dial pipe first
	client, err := NewClient()
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}
	defer client.Close()

	// Verify pipe was attempted first
	dialMu.Lock()
	defer dialMu.Unlock()

	if len(dialAttempts) == 0 {
		t.Fatal("NewClient() made no dial attempts")
	}

	if dialAttempts[0] != "pipe" {
		t.Errorf("first dial attempt network = %q; want %q", dialAttempts[0], "pipe")
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
	dialAttempts := make([]string, 0)
	var dialMu sync.Mutex

	originalDialFunc := dialFunc
	dialFunc = func(network, address string) (net.Conn, error) {
		dialMu.Lock()
		dialAttempts = append(dialAttempts, network)
		dialMu.Unlock()

		if network == "pipe" {
			// Simulate pipe failure
			return nil, fmt.Errorf("pipe connection failed")
		}

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
	dialMu.Lock()
	defer dialMu.Unlock()

	if len(dialAttempts) < 2 {
		t.Fatalf("NewClient() dial attempts = %d; want at least 2 (pipe + tcp)", len(dialAttempts))
	}

	if dialAttempts[0] != "pipe" {
		t.Errorf("first dial attempt = %q; want %q", dialAttempts[0], "pipe")
	}

	if dialAttempts[1] != "tcp" {
		t.Errorf("second dial attempt = %q; want %q", dialAttempts[1], "tcp")
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
	dialAttempts := make([]string, 0)
	var dialMu sync.Mutex

	originalDialFunc := dialFunc
	dialFunc = func(network, address string) (net.Conn, error) {
		dialMu.Lock()
		dialAttempts = append(dialAttempts, network)
		dialMu.Unlock()

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
	dialMu.Lock()
	defer dialMu.Unlock()

	if len(dialAttempts) != 1 {
		t.Errorf("NewClient() dial attempts = %d; want exactly 1 (tcp only)", len(dialAttempts))
	}

	if len(dialAttempts) > 0 && dialAttempts[0] != "tcp" {
		t.Errorf("dial attempt = %q; want %q", dialAttempts[0], "tcp")
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

	// Server handler
	serverErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()

		// Read request
		decoder := json.NewDecoder(conn)
		var req Request
		if err := decoder.Decode(&req); err != nil {
			serverErr <- fmt.Errorf("decode failed: %w", err)
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

		encoder := json.NewEncoder(conn)
		if err := encoder.Encode(resp); err != nil {
			serverErr <- fmt.Errorf("encode failed: %w", err)
			return
		}
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Override dialFunc to use real pipe
	originalDialFunc := dialFunc
	dialFunc = func(network, address string) (net.Conn, error) {
		if network == "pipe" || network == "unix" {
			return winio.DialPipe(pipeName, nil)
		}
		return originalDialFunc(network, address)
	}
	defer func() { dialFunc = originalDialFunc }()

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

// TestDialPipe_ConnectionRefused verifies error handling when pipe doesn't exist.
func TestDialPipe_ConnectionRefused(t *testing.T) {
	nonexistentPipe := `\\.\pipe\warpdl-does-not-exist`

	timeout := 200 * time.Millisecond
	conn, err := winio.DialPipe(nonexistentPipe, &timeout)

	if err == nil {
		conn.Close()
		t.Fatal("DialPipe() succeeded on nonexistent pipe; want error")
	}

	// Error should indicate connection failure
	if err.Error() == "" {
		t.Error("DialPipe() returned empty error message")
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

	// Mock dial with timeout simulation
	originalDialFunc := dialFunc
	dialFunc = func(network, address string) (net.Conn, error) {
		if network == "pipe" {
			// Simulate timeout
			time.Sleep(100 * time.Millisecond)
			return nil, fmt.Errorf("pipe dial timeout")
		}

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

// TestPipeConnection_WriteRead verifies basic write/read operations on pipe connection.
func TestPipeConnection_WriteRead(t *testing.T) {
	pipeName := fmt.Sprintf(`\\.\pipe\warpdl-test-rw-%d`, time.Now().UnixNano())

	// Create listener
	listener, err := winio.ListenPipe(pipeName, nil)
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	// Server echo handler
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}

		_, _ = conn.Write(buf[:n])
	}()

	time.Sleep(50 * time.Millisecond)

	// Client connection
	conn, err := winio.DialPipe(pipeName, nil)
	if err != nil {
		t.Fatalf("DialPipe() failed: %v", err)
	}
	defer conn.Close()

	// Write test message
	testMsg := []byte("hello pipe")
	n, err := conn.Write(testMsg)
	if err != nil {
		t.Fatalf("Write() failed: %v", err)
	}
	if n != len(testMsg) {
		t.Errorf("Write() wrote %d bytes; want %d", n, len(testMsg))
	}

	// Read echo
	buf := make([]byte, 1024)
	n, err = conn.Read(buf)
	if err != nil {
		t.Fatalf("Read() failed: %v", err)
	}

	if string(buf[:n]) != string(testMsg) {
		t.Errorf("Read() = %q; want %q", string(buf[:n]), string(testMsg))
	}
}
