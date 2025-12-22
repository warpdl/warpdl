package warpcli

import (
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/warpdl/warpdl/common"
)

func TestBufioRoundTrip(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	msg := []byte("hello")
	go func() {
		_ = write(c1, msg)
	}()
	got, err := read(c2)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(msg) {
		t.Fatalf("unexpected payload: %s", string(got))
	}
}

func TestDispatcherProcess(t *testing.T) {
	d := &Dispatcher{Handlers: make(map[common.UpdateType][]Handler)}
	if err := d.process([]byte(`{"ok":true,"update":{"type":"download","message":{}}}`)); err == nil {
		t.Fatalf("expected error for missing handler")
	}
	called := false
	d.AddHandler(common.UPDATE_DOWNLOAD, HandlerFunc(func(b json.RawMessage) error {
		called = true
		return nil
	}))
	if err := d.process([]byte(`{"ok":true,"update":{"type":"download","message":{}}}`)); err != nil {
		t.Fatalf("process: %v", err)
	}
	if !called {
		t.Fatalf("expected handler to be called")
	}
}

type HandlerFunc func(json.RawMessage) error

func (h HandlerFunc) Handle(b json.RawMessage) error { return h(b) }

func TestDownloadingHandler(t *testing.T) {
	called := false
	h := NewDownloadingHandler(common.DownloadProgress, func(dr *common.DownloadingResponse) error {
		called = true
		return nil
	})
	msg := []byte(`{"action":"download_progress","value":5}`)
	if err := h.Handle(msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !called {
		t.Fatalf("expected callback to be called")
	}
}

func TestClientInvokeDownload(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	client := &Client{
		conn: c1,
		mu:   &sync.RWMutex{},
		d: &Dispatcher{
			Handlers: make(map[common.UpdateType][]Handler),
		},
	}
	go func() {
		reqBytes, err := read(c2)
		if err != nil {
			return
		}
		var req Request
		_ = json.Unmarshal(reqBytes, &req)
		respMsg, _ := json.Marshal(common.DownloadResponse{DownloadId: "id", FileName: "file", DownloadDirectory: "."})
		respBytes, _ := json.Marshal(Response{Ok: true, Update: &Update{Type: req.Method, Message: respMsg}})
		_ = write(c2, respBytes)
	}()

	resp, err := client.Download("http://example.com", "file", ".", nil)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if resp.DownloadId != "id" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestNewClientWithURI_EmptyUsesDefault(t *testing.T) {
	// Mock functions to avoid spawning daemon and connecting
	originalEnsureDaemon := ensureDaemonFunc
	originalDial := dialFunc
	defer func() {
		ensureDaemonFunc = originalEnsureDaemon
		dialFunc = originalDial
	}()

	ensureDaemonFunc = func() error { return nil }
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	dialFunc = func(network, address string) (net.Conn, error) {
		if network != "unix" {
			t.Errorf("Expected network 'unix', got '%s'", network)
		}
		return c1, nil
	}

	client, err := NewClientWithURI("")
	if err != nil {
		t.Fatalf("NewClientWithURI with empty string should succeed: %v", err)
	}
	if client == nil {
		t.Fatal("Expected client to be created")
	}
}

func TestNewClientWithURI_TCP(t *testing.T) {
	// Mock dial function
	originalDial := dialFunc
	defer func() { dialFunc = originalDial }()

	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	dialFunc = func(network, address string) (net.Conn, error) {
		if network != "tcp" {
			t.Errorf("Expected network 'tcp', got '%s'", network)
		}
		if address != "localhost:9090" {
			t.Errorf("Expected address 'localhost:9090', got '%s'", address)
		}
		return c1, nil
	}

	client, err := NewClientWithURI("tcp://localhost:9090")
	if err != nil {
		t.Fatalf("NewClientWithURI with TCP URI failed: %v", err)
	}
	if client == nil {
		t.Fatal("Expected client to be created")
	}
}

func TestNewClientWithURI_InvalidURI(t *testing.T) {
	// Mock dial function to avoid actual connection attempts
	originalDial := dialFunc
	defer func() { dialFunc = originalDial }()

	dialFunc = func(network, address string) (net.Conn, error) {
		t.Error("dial should not be called for invalid URI")
		return nil, nil
	}

	_, err := NewClientWithURI("tcp://")
	if err == nil {
		t.Fatal("NewClientWithURI with invalid URI should return error")
	}
}

func TestClientListenDisconnect(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	client := &Client{
		conn: c1,
		mu:   &sync.RWMutex{},
		d: &Dispatcher{
			Handlers: make(map[common.UpdateType][]Handler),
		},
	}
	client.AddHandler(common.UPDATE_DOWNLOADING, HandlerFunc(func(b json.RawMessage) error {
		return ErrDisconnect
	}))
	go func() {
		respBytes, _ := json.Marshal(Response{Ok: true, Update: &Update{Type: common.UPDATE_DOWNLOADING, Message: json.RawMessage(`{"action":"download_progress"}`)}})
		_ = write(c2, respBytes)
	}()
	if err := client.Listen(); err != nil {
		t.Fatalf("Listen: %v", err)
	}
}

// errorConn is a net.Conn that returns errors on read/write
type errorConn struct {
	readErr  error
	writeErr error
	readN    int // number of successful reads before error
	writeN   int // number of successful writes before error
}

func (e *errorConn) Read(b []byte) (int, error) {
	if e.readN > 0 {
		e.readN--
		// Return valid header for first read
		copy(b, intToBytes(5))
		return 4, nil
	}
	return 0, e.readErr
}

func (e *errorConn) Write(b []byte) (int, error) {
	if e.writeN > 0 {
		e.writeN--
		return len(b), nil
	}
	return 0, e.writeErr
}

func (e *errorConn) Close() error                       { return nil }
func (e *errorConn) LocalAddr() net.Addr                { return nil }
func (e *errorConn) RemoteAddr() net.Addr               { return nil }
func (e *errorConn) SetDeadline(_ time.Time) error      { return nil }
func (e *errorConn) SetReadDeadline(_ time.Time) error  { return nil }
func (e *errorConn) SetWriteDeadline(_ time.Time) error { return nil }

func TestBufioWrite_HeaderWriteFails(t *testing.T) {
	conn := &errorConn{writeErr: errors.New("header write error"), writeN: 0}
	err := write(conn, []byte("test"))
	if err == nil {
		t.Fatal("expected error on header write")
	}
	if !strings.Contains(err.Error(), "header write error") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBufioWrite_PayloadWriteFails(t *testing.T) {
	conn := &errorConn{writeErr: errors.New("payload write error"), writeN: 1}
	err := write(conn, []byte("test"))
	if err == nil {
		t.Fatal("expected error on payload write")
	}
	if !strings.Contains(err.Error(), "payload write error") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBufioRead_HeaderReadFails(t *testing.T) {
	conn := &errorConn{readErr: errors.New("header read error"), readN: 0}
	_, err := read(conn)
	if err == nil {
		t.Fatal("expected error on header read")
	}
	if !strings.Contains(err.Error(), "header read error") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBufioRead_PayloadReadFails(t *testing.T) {
	conn := &errorConn{readErr: errors.New("payload read error"), readN: 1}
	_, err := read(conn)
	if err == nil {
		t.Fatal("expected error on payload read")
	}
	if !strings.Contains(err.Error(), "payload read error") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInvoke_WriteError(t *testing.T) {
	conn := &errorConn{writeErr: errors.New("write failed")}
	client := &Client{
		conn: conn,
		mu:   &sync.RWMutex{},
		d:    &Dispatcher{Handlers: make(map[common.UpdateType][]Handler)},
	}

	_, err := client.invoke(common.UPDATE_LIST, nil)
	if err == nil {
		t.Fatal("expected error on write")
	}
	if !strings.Contains(err.Error(), "failed to invoke") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInvoke_ReadError(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()

	client := &Client{
		conn: c1,
		mu:   &sync.RWMutex{},
		d:    &Dispatcher{Handlers: make(map[common.UpdateType][]Handler)},
	}

	go func() {
		// Read the request but then close without sending response
		_, _ = read(c2)
		c2.Close()
	}()

	_, err := client.invoke(common.UPDATE_LIST, nil)
	if err == nil {
		t.Fatal("expected error on read")
	}
}

func TestInvoke_UnmarshalError(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	client := &Client{
		conn: c1,
		mu:   &sync.RWMutex{},
		d:    &Dispatcher{Handlers: make(map[common.UpdateType][]Handler)},
	}

	go func() {
		_, _ = read(c2)
		// Write invalid JSON
		_ = write(c2, []byte("invalid json{{{"))
	}()

	_, err := client.invoke(common.UPDATE_LIST, nil)
	if err == nil {
		t.Fatal("expected error on unmarshal")
	}
	if !strings.Contains(err.Error(), "failed to read") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInvoke_ErrorResponse(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	client := &Client{
		conn: c1,
		mu:   &sync.RWMutex{},
		d:    &Dispatcher{Handlers: make(map[common.UpdateType][]Handler)},
	}

	go func() {
		_, _ = read(c2)
		respBytes, _ := json.Marshal(Response{Ok: false, Error: "server error message"})
		_ = write(c2, respBytes)
	}()

	_, err := client.invoke(common.UPDATE_LIST, nil)
	if err == nil {
		t.Fatal("expected error from server")
	}
	if err.Error() != "server error message" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListen_ReadError(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()

	client := &Client{
		conn: c1,
		mu:   &sync.RWMutex{},
		d:    &Dispatcher{Handlers: make(map[common.UpdateType][]Handler)},
	}

	// Close the other end to cause read error
	c2.Close()

	err := client.Listen()
	if err == nil {
		t.Fatal("expected error on read")
	}
	if !strings.Contains(err.Error(), "error reading") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListen_ProcessError(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	client := &Client{
		conn: c1,
		mu:   &sync.RWMutex{},
		d:    &Dispatcher{Handlers: make(map[common.UpdateType][]Handler)},
	}

	// Register a handler that returns an error
	client.AddHandler(common.UPDATE_DOWNLOADING, HandlerFunc(func(b json.RawMessage) error {
		return errors.New("handler error")
	}))

	go func() {
		respBytes, _ := json.Marshal(Response{Ok: true, Update: &Update{Type: common.UPDATE_DOWNLOADING, Message: json.RawMessage(`{}`)}})
		_ = write(c2, respBytes)
	}()

	err := client.Listen()
	if err == nil {
		t.Fatal("expected error from handler")
	}
	if !strings.Contains(err.Error(), "error processing") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDownloadingHandler_UnmarshalError(t *testing.T) {
	h := NewDownloadingHandler("", func(dr *common.DownloadingResponse) error {
		return nil
	})

	err := h.Handle([]byte("invalid json{{{"))
	if err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestDownloadingHandler_ActionMismatch(t *testing.T) {
	called := false
	h := NewDownloadingHandler(common.DownloadComplete, func(dr *common.DownloadingResponse) error {
		called = true
		return nil
	})

	// Send a different action
	msg := []byte(`{"action":"download_progress","value":5}`)
	if err := h.Handle(msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if called {
		t.Fatal("callback should not be called for mismatched action")
	}
}

func TestDispatcherProcess_ErrorResponse(t *testing.T) {
	d := &Dispatcher{Handlers: make(map[common.UpdateType][]Handler)}
	err := d.process([]byte(`{"ok":false,"error":"some error"}`))
	if err == nil {
		t.Fatal("expected error for error response")
	}
	if err.Error() != "some error" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDispatcherProcess_InvalidJSON(t *testing.T) {
	d := &Dispatcher{Handlers: make(map[common.UpdateType][]Handler)}
	err := d.process([]byte(`invalid json{{{`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "failed to parse") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDispatcherProcess_HandlerError(t *testing.T) {
	d := &Dispatcher{Handlers: make(map[common.UpdateType][]Handler)}
	d.AddHandler(common.UPDATE_DOWNLOAD, HandlerFunc(func(b json.RawMessage) error {
		return errors.New("handler failed")
	}))
	err := d.process([]byte(`{"ok":true,"update":{"type":"download","message":{}}}`))
	if err == nil {
		t.Fatal("expected error from handler")
	}
	if err.Error() != "handler failed" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewClientEnsureDaemonError(t *testing.T) {
	oldEnsure := ensureDaemonFunc
	ensureDaemonFunc = func() error { return errors.New("daemon error") }
	defer func() { ensureDaemonFunc = oldEnsure }()

	if _, err := NewClient(); err == nil {
		t.Fatal("expected error from ensureDaemon")
	}
}

func TestNewClientDialError(t *testing.T) {
	oldEnsure := ensureDaemonFunc
	oldDial := dialFunc
	ensureDaemonFunc = func() error { return nil }
	dialFunc = func(string, string) (net.Conn, error) {
		return nil, errors.New("dial error")
	}
	defer func() {
		ensureDaemonFunc = oldEnsure
		dialFunc = oldDial
	}()

	if _, err := NewClient(); err == nil {
		t.Fatal("expected error from dial")
	}
}
