//go:build windows

package server

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"
)

// TestCreatePipeListener_Success verifies that the server can create a named pipe listener
// with the correct pipe address on Windows.
func TestCreatePipeListener_Success(t *testing.T) {
	// Create a test server
	logger := log.New(os.Stdout, "[TEST] ", log.LstdFlags)
	manager, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager failed: %v", err)
	}
	server := NewServer(logger, manager, common.DefaultTCPPort)

	// Ensure we're not forcing TCP
	t.Setenv(common.ForceTCPEnv, "")

	listener, err := server.createListener()
	if err != nil {
		t.Fatalf("createListener() failed: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()
	expectedPrefix := `\\.\pipe\`

	if !strings.HasPrefix(addr, expectedPrefix) {
		t.Errorf("listener address = %q; want prefix %q", addr, expectedPrefix)
	}

	// Verify it's actually a pipe listener
	if listener.Addr().Network() != "pipe" {
		t.Errorf("listener network = %q; want %q", listener.Addr().Network(), "pipe")
	}
}

// TestCreatePipeListener_CustomPipeName verifies that the server uses a custom pipe name
// when the WARPDL_PIPE_NAME environment variable is set.
func TestCreatePipeListener_CustomPipeName(t *testing.T) {
	customName := "warpdl-test-custom"
	t.Setenv(common.PipeNameEnv, customName)
	t.Setenv(common.ForceTCPEnv, "")

	logger := log.New(os.Stdout, "[TEST] ", log.LstdFlags)
	manager, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager failed: %v", err)
	}
	server := NewServer(logger, manager, common.DefaultTCPPort)

	listener, err := server.createListener()
	if err != nil {
		t.Fatalf("createListener() failed: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()
	expectedAddr := fmt.Sprintf(`\\.\pipe\%s`, customName)

	if addr != expectedAddr {
		t.Errorf("listener address = %q; want %q", addr, expectedAddr)
	}
}

// TestCreatePipeListener_TCPFallback verifies that when named pipe creation fails,
// the server falls back to TCP listener.
func TestCreatePipeListener_TCPFallback(t *testing.T) {
	// Force TCP mode to simulate pipe failure
	t.Setenv(common.ForceTCPEnv, "1")

	logger := log.New(os.Stdout, "[TEST] ", log.LstdFlags)
	manager, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager failed: %v", err)
	}
	server := NewServer(logger, manager, common.DefaultTCPPort)

	listener, err := server.createListener()
	if err != nil {
		t.Fatalf("createListener() failed: %v", err)
	}
	defer listener.Close()

	// Should be TCP listener
	if listener.Addr().Network() != "tcp" {
		t.Errorf("listener network = %q; want %q", listener.Addr().Network(), "tcp")
	}

	addr := listener.Addr().String()
	// Go may resolve "localhost" to "127.0.0.1" in the listener address
	if !strings.Contains(addr, common.TCPHost) && !strings.Contains(addr, "127.0.0.1") {
		t.Errorf("TCP listener address = %q; want to contain %q or %q", addr, common.TCPHost, "127.0.0.1")
	}
}

// TestCreatePipeListener_AcceptsConnections verifies that a pipe listener can accept
// client connections successfully.
func TestCreatePipeListener_AcceptsConnections(t *testing.T) {
	pipeName := fmt.Sprintf(`\\.\pipe\warpdl-test-%d`, time.Now().UnixNano())
	t.Setenv(common.PipeNameEnv, pipeName)
	t.Setenv(common.ForceTCPEnv, "")

	logger := log.New(os.Stdout, "[TEST] ", log.LstdFlags)
	manager, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager failed: %v", err)
	}
	server := NewServer(logger, manager, common.DefaultTCPPort)

	listener, err := server.createListener()
	if err != nil {
		t.Fatalf("createListener() failed: %v", err)
	}
	defer listener.Close()

	// Channel to signal connection acceptance
	accepted := make(chan bool, 1)
	errChan := make(chan error, 1)

	// Accept connection in goroutine
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			errChan <- err
			return
		}
		defer conn.Close()
		accepted <- true
	}()

	// Give listener time to start accepting
	time.Sleep(100 * time.Millisecond)

	// Dial the pipe
	clientConn, err := winio.DialPipe(listener.Addr().String(), nil)
	if err != nil {
		t.Fatalf("failed to dial pipe: %v", err)
	}
	defer clientConn.Close()

	// Wait for acceptance or timeout
	select {
	case <-accepted:
		// Success
	case err := <-errChan:
		t.Fatalf("listener.Accept() failed: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for connection acceptance")
	}
}

// TestPipeListener_RoundtripMessage verifies that a full request/response cycle works
// through the named pipe listener.
func TestPipeListener_RoundtripMessage(t *testing.T) {
	pipeName := fmt.Sprintf(`\\.\pipe\warpdl-test-roundtrip-%d`, time.Now().UnixNano())
	t.Setenv(common.PipeNameEnv, pipeName)
	t.Setenv(common.ForceTCPEnv, "")

	logger := log.New(os.Stdout, "[TEST] ", log.LstdFlags)
	manager, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager failed: %v", err)
	}
	server := NewServer(logger, manager, common.DefaultTCPPort)

	listener, err := server.createListener()
	if err != nil {
		t.Fatalf("createListener() failed: %v", err)
	}
	defer listener.Close()

	// Simple echo server
	responseChan := make(chan string, 1)
	errChan := make(chan error, 1)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			errChan <- fmt.Errorf("accept failed: %w", err)
			return
		}
		defer conn.Close()

		// Read request
		var buf [1024]byte
		n, err := conn.Read(buf[:])
		if err != nil {
			errChan <- fmt.Errorf("read failed: %w", err)
			return
		}

		request := string(buf[:n])

		// Echo response
		response := fmt.Sprintf("echo: %s", request)
		_, err = conn.Write([]byte(response))
		if err != nil {
			errChan <- fmt.Errorf("write failed: %w", err)
			return
		}

		responseChan <- response
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Client connection
	clientConn, err := winio.DialPipe(listener.Addr().String(), nil)
	if err != nil {
		t.Fatalf("failed to dial pipe: %v", err)
	}
	defer clientConn.Close()

	// Send request
	testMessage := "hello pipe"
	_, err = clientConn.Write([]byte(testMessage))
	if err != nil {
		t.Fatalf("client write failed: %v", err)
	}

	// Read response
	var buf [1024]byte
	n, err := clientConn.Read(buf[:])
	if err != nil {
		t.Fatalf("client read failed: %v", err)
	}

	response := string(buf[:n])
	expectedResponse := fmt.Sprintf("echo: %s", testMessage)

	if response != expectedResponse {
		t.Errorf("response = %q; want %q", response, expectedResponse)
	}

	// Verify server side
	select {
	case serverResponse := <-responseChan:
		if serverResponse != expectedResponse {
			t.Errorf("server response = %q; want %q", serverResponse, expectedResponse)
		}
	case err := <-errChan:
		t.Fatalf("server error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for server response")
	}
}

// TestPipeListener_JSONRoundtrip verifies that JSON-RPC style messages can be sent
// and received through the pipe listener (mimicking real daemon communication).
func TestPipeListener_JSONRoundtrip(t *testing.T) {
	pipeName := fmt.Sprintf(`\\.\pipe\warpdl-test-json-%d`, time.Now().UnixNano())
	t.Setenv(common.PipeNameEnv, pipeName)
	t.Setenv(common.ForceTCPEnv, "")

	logger := log.New(os.Stdout, "[TEST] ", log.LstdFlags)
	manager, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager failed: %v", err)
	}
	server := NewServer(logger, manager, common.DefaultTCPPort)

	listener, err := server.createListener()
	if err != nil {
		t.Fatalf("createListener() failed: %v", err)
	}
	defer listener.Close()

	type Request struct {
		Method string `json:"method"`
		Data   string `json:"data"`
	}

	type Response struct {
		Status string `json:"status"`
		Result string `json:"result"`
	}

	errChan := make(chan error, 1)

	// Server handler
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			errChan <- fmt.Errorf("accept failed: %w", err)
			return
		}
		defer conn.Close()

		// Read JSON request
		decoder := json.NewDecoder(conn)
		var req Request
		if err := decoder.Decode(&req); err != nil {
			errChan <- fmt.Errorf("decode failed: %w", err)
			return
		}

		// Send JSON response
		resp := Response{
			Status: "ok",
			Result: fmt.Sprintf("processed: %s", req.Data),
		}
		encoder := json.NewEncoder(conn)
		if err := encoder.Encode(resp); err != nil {
			errChan <- fmt.Errorf("encode failed: %w", err)
			return
		}
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Client connection
	clientConn, err := winio.DialPipe(listener.Addr().String(), nil)
	if err != nil {
		t.Fatalf("failed to dial pipe: %v", err)
	}
	defer clientConn.Close()

	// Send JSON request
	req := Request{
		Method: "test",
		Data:   "hello",
	}
	encoder := json.NewEncoder(clientConn)
	if err := encoder.Encode(req); err != nil {
		t.Fatalf("client encode failed: %v", err)
	}

	// Read JSON response
	decoder := json.NewDecoder(clientConn)
	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("client decode failed: %v", err)
	}

	if resp.Status != "ok" {
		t.Errorf("response status = %q; want %q", resp.Status, "ok")
	}

	expectedResult := "processed: hello"
	if resp.Result != expectedResult {
		t.Errorf("response result = %q; want %q", resp.Result, expectedResult)
	}

	// Check for server errors
	select {
	case err := <-errChan:
		t.Fatalf("server error: %v", err)
	default:
		// No error, good
	}
}

// TestPipeListener_MultipleConnections verifies that the pipe listener can handle
// multiple sequential connections.
func TestPipeListener_MultipleConnections(t *testing.T) {
	pipeName := fmt.Sprintf(`\\.\pipe\warpdl-test-multi-%d`, time.Now().UnixNano())
	t.Setenv(common.PipeNameEnv, pipeName)
	t.Setenv(common.ForceTCPEnv, "")

	logger := log.New(os.Stdout, "[TEST] ", log.LstdFlags)
	manager, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager failed: %v", err)
	}
	server := NewServer(logger, manager, common.DefaultTCPPort)

	listener, err := server.createListener()
	if err != nil {
		t.Fatalf("createListener() failed: %v", err)
	}
	defer listener.Close()

	// Accept multiple connections
	numConnections := 3
	for i := 0; i < numConnections; i++ {
		i := i // capture
		t.Run(fmt.Sprintf("connection_%d", i), func(t *testing.T) {
			errChan := make(chan error, 1)
			doneChan := make(chan struct{})

			go func() {
				defer close(doneChan)
				conn, err := listener.Accept()
				if err != nil {
					errChan <- err
					return
				}
				defer conn.Close()

				// Fixed-size read instead of io.ReadAll to avoid race condition
				buf := make([]byte, 1024)
				n, err := conn.Read(buf)
				if err != nil {
					errChan <- err
					return
				}
				_, err = conn.Write(buf[:n])
				if err != nil {
					errChan <- err
				}
			}()

			time.Sleep(50 * time.Millisecond)

			// Client
			clientConn, err := winio.DialPipe(listener.Addr().String(), nil)
			if err != nil {
				t.Fatalf("dial failed: %v", err)
			}
			defer clientConn.Close()

			testMsg := fmt.Sprintf("message-%d", i)
			_, err = clientConn.Write([]byte(testMsg))
			if err != nil {
				t.Fatalf("write failed: %v", err)
			}

			// Read echo response BEFORE closing - ensures server Write() completes
			buf := make([]byte, 1024)
			n, err := clientConn.Read(buf)
			if err != nil {
				t.Fatalf("read failed: %v", err)
			}
			if string(buf[:n]) != testMsg {
				t.Errorf("echo = %q; want %q", string(buf[:n]), testMsg)
			}

			// Connection will be closed by defer

			select {
			case err := <-errChan:
				if err != nil {
					t.Fatalf("server error: %v", err)
				}
			case <-doneChan:
				// Success - server completed without error
			case <-time.After(2 * time.Second):
				t.Fatal("timeout")
			}
		})
	}
}
