package server

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"testing"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func TestHandlerWrapperUnknownMethod(t *testing.T) {
	s := &Server{handler: make(map[common.UpdateType]HandlerFunc), pool: NewPool(nil)}
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	req, _ := json.Marshal(Request{Method: common.UpdateType("nope")})
	go func() {
		_ = s.handlerWrapper(NewSyncConn(c1), req)
	}()
	respBytes, err := NewSyncConn(c2).Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var resp Response
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if resp.Ok {
		t.Fatalf("expected error response")
	}
}

func TestHandlerWrapperError(t *testing.T) {
	s := &Server{handler: make(map[common.UpdateType]HandlerFunc), pool: NewPool(nil)}
	s.handler[common.UPDATE_LIST] = func(conn *SyncConn, pool *Pool, body json.RawMessage) (common.UpdateType, any, error) {
		return common.UPDATE_LIST, nil, errors.New("boom")
	}
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	req, _ := json.Marshal(Request{Method: common.UPDATE_LIST})
	go func() {
		_ = s.handlerWrapper(NewSyncConn(c1), req)
	}()
	respBytes, err := NewSyncConn(c2).Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var resp Response
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if resp.Ok || resp.Error == "" {
		t.Fatalf("expected error response")
	}
}

func TestHandlerWrapperSuccess(t *testing.T) {
	s := &Server{handler: make(map[common.UpdateType]HandlerFunc), pool: NewPool(nil)}
	s.handler[common.UPDATE_LIST] = func(conn *SyncConn, pool *Pool, body json.RawMessage) (common.UpdateType, any, error) {
		return common.UPDATE_LIST, map[string]string{"ok": "1"}, nil
	}
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	req, _ := json.Marshal(Request{Method: common.UPDATE_LIST})
	go func() {
		_ = s.handlerWrapper(NewSyncConn(c1), req)
	}()
	respBytes, err := NewSyncConn(c2).Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var resp Response
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !resp.Ok || resp.Update == nil || resp.Update.Type != common.UPDATE_LIST {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestResponseHelpers(t *testing.T) {
	b := MakeResult(common.UPDATE_LIST, map[string]string{"ok": "1"})
	var resp Response
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !resp.Ok || resp.Update == nil || resp.Update.Type != common.UPDATE_LIST {
		t.Fatalf("unexpected response: %+v", resp)
	}
	b = InitError(errors.New("boom"))
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if resp.Ok || resp.Error != "boom" {
		t.Fatalf("unexpected error response: %+v", resp)
	}
	b = InitError(nil)
	if err := json.Unmarshal(b, &resp); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if resp.Ok || resp.Error == "" {
		t.Fatalf("expected unknown error response")
	}
}

func TestErrorString(t *testing.T) {
	e := &Error{Type: ErrorTypeWarning, Message: "warn"}
	if e.Error() != "warn" {
		t.Fatalf("unexpected Error output: %s", e.Error())
	}
}

func TestNewServerRegisterHandler(t *testing.T) {
	if err := warplib.SetConfigDir(t.TempDir()); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	m, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()
	s := NewServer(log.New(io.Discard, "", 0), m, 0)
	called := false
	s.RegisterHandler(common.UPDATE_LIST, func(*SyncConn, *Pool, json.RawMessage) (common.UpdateType, any, error) {
		called = true
		return common.UPDATE_LIST, map[string]string{"ok": "1"}, nil
	})
	if _, ok := s.handler[common.UPDATE_LIST]; !ok {
		t.Fatalf("expected handler to be registered")
	}
	if called {
		t.Fatalf("handler should not be called during registration")
	}
}

func TestHandleConnection(t *testing.T) {
	s := &Server{
		handler: make(map[common.UpdateType]HandlerFunc),
		pool:    NewPool(nil),
		log:     log.New(io.Discard, "", 0),
	}
	s.handler[common.UPDATE_LIST] = func(conn *SyncConn, pool *Pool, body json.RawMessage) (common.UpdateType, any, error) {
		return common.UPDATE_LIST, map[string]string{"ok": "1"}, nil
	}
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	go s.handleConnection(c1)
	req, _ := json.Marshal(Request{Method: common.UPDATE_LIST})
	sconn := NewSyncConn(c2)
	if err := sconn.Write(req); err != nil {
		t.Fatalf("Write: %v", err)
	}
	respBytes, err := sconn.Read()
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var resp Response
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !resp.Ok {
		t.Fatalf("expected ok response")
	}
}

func TestCreateListenerUnixSocket(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := tmpDir + "/test.sock"
	t.Setenv(socketPathEnv, sockPath)

	s := &Server{
		log:  log.New(io.Discard, "", 0),
		port: 0,
	}
	l, err := s.createListener()
	if err != nil {
		t.Fatalf("createListener: %v", err)
	}
	defer l.Close()

	if l.Addr().Network() != "unix" {
		t.Fatalf("expected unix socket, got %s", l.Addr().Network())
	}
}

func TestCreateListenerTCPFallback(t *testing.T) {
	// Use an invalid path to force TCP fallback
	t.Setenv(socketPathEnv, "/nonexistent/path/test.sock")

	s := &Server{
		log:  log.New(io.Discard, "", 0),
		port: 0, // port 0 lets OS pick available port
	}
	l, err := s.createListener()
	if err != nil {
		t.Fatalf("createListener: %v", err)
	}
	defer l.Close()

	if l.Addr().Network() != "tcp" {
		t.Fatalf("expected tcp socket, got %s", l.Addr().Network())
	}
}
