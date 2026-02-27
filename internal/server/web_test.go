package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/warpdl/warpdl/pkg/warplib"
	"golang.org/x/net/websocket"
)

func newRangeServer(content []byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		if r.Header.Get("Range") == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(content)
			return
		}
		rangeHeader := strings.TrimPrefix(r.Header.Get("Range"), "bytes=")
		parts := strings.SplitN(rangeHeader, "-", 2)
		start, _ := strconv.Atoi(parts[0])
		end := len(content) - 1
		if parts[1] != "" {
			if e, err := strconv.Atoi(parts[1]); err == nil {
				end = e
			}
		}
		if start > end || start < 0 || end >= len(content) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		chunk := content[start : end+1]
		w.Header().Set("Content-Length", strconv.Itoa(len(chunk)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(chunk)
	}))
}

func TestWebServerProcessDownload(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	m, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()

	content := bytes.Repeat([]byte("c"), 1024)
	srv := newRangeServer(content)
	defer srv.Close()

	pool := NewPool(log.New(io.Discard, "", 0))
	ws := NewWebServer(log.New(io.Discard, "", 0), m, pool, 0, nil, nil, nil)
	if err := ws.processDownload(&capturedDownload{Url: srv.URL + "/file.bin"}); err != nil {
		t.Fatalf("processDownload: %v", err)
	}

	var item *warplib.Item
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		items := m.GetItems()
		if len(items) > 0 {
			item = items[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if item == nil {
		t.Fatalf("expected item to be created")
	}
	deadline = time.Now().Add(2 * time.Second)
	complete := false
	for time.Now().Before(deadline) {
		info, err := os.Stat(item.GetSavePath())
		if err == nil && info.Size() == int64(len(content)) {
			complete = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !complete {
		t.Fatalf("download did not complete")
	}
	info, err := os.Stat(item.GetSavePath())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() != int64(len(content)) {
		t.Fatalf("downloaded size mismatch")
	}
}

func TestWebServerHandleConnection(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	m, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()

	content := bytes.Repeat([]byte("z"), 64)
	srv := newRangeServer(content)
	defer srv.Close()

	pool := NewPool(log.New(io.Discard, "", 0))
	ws := NewWebServer(log.New(io.Discard, "", 0), m, pool, 0, nil, nil, nil)
	wsSrv := httptest.NewServer(websocket.Handler(ws.handleConnection))
	defer wsSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(wsSrv.URL, "http")
	conn, err := websocket.Dial(wsURL, "", wsSrv.URL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	payload, _ := json.Marshal(capturedDownload{Url: srv.URL + "/file.bin"})
	if err := websocket.Message.Send(conn, payload); err != nil {
		t.Fatalf("Send: %v", err)
	}
	_ = conn.Close()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(m.GetItems()) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected websocket download to start")
}

func TestWebServerHandler(t *testing.T) {
	pool := NewPool(log.New(io.Discard, "", 0))
	ws := NewWebServer(log.New(io.Discard, "", 0), nil, pool, 8080, nil, nil, nil)
	h := ws.handler()
	if h == nil {
		t.Fatalf("expected non-nil handler")
	}
}

func TestWebServerAddr(t *testing.T) {
	pool := NewPool(log.New(io.Discard, "", 0))
	ws := NewWebServer(log.New(io.Discard, "", 0), nil, pool, 9999, nil, nil, nil)
	addr := ws.addr()
	if addr != "127.0.0.1:9999" {
		t.Fatalf("expected 127.0.0.1:9999, got %s", addr)
	}
}

func TestWebServerAddr_ListenAll(t *testing.T) {
	pool := NewPool(log.New(io.Discard, "", 0))
	rpcCfg := &RPCConfig{Secret: "test", ListenAll: true}
	ws := NewWebServer(log.New(io.Discard, "", 0), nil, pool, 9999, nil, nil, rpcCfg)
	addr := ws.addr()
	if addr != ":9999" {
		t.Fatalf("expected :9999 with listenAll, got %s", addr)
	}
}

func TestWebServerHandler_WithRPC(t *testing.T) {
	pool := NewPool(log.New(io.Discard, "", 0))
	rpcCfg := &RPCConfig{
		Secret:  "test-secret",
		Version: "1.0.0",
	}
	ws := NewWebServer(log.New(io.Discard, "", 0), nil, pool, 8080, nil, nil, rpcCfg)
	defer func() {
		if ws.rpc != nil {
			ws.rpc.Close()
		}
	}()

	// Verify /jsonrpc route exists by making a request
	srv := httptest.NewServer(ws.handler())
	defer srv.Close()

	// Request with auth should reach the RPC endpoint
	body := []byte(`{"jsonrpc":"2.0","method":"system.getVersion","id":1}`)
	req, _ := http.NewRequest("POST", srv.URL+"/jsonrpc", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestWebServerHandler_WithoutRPC(t *testing.T) {
	pool := NewPool(log.New(io.Discard, "", 0))
	ws := NewWebServer(log.New(io.Discard, "", 0), nil, pool, 8080, nil, nil, nil)

	srv := httptest.NewServer(ws.handler())
	defer srv.Close()

	// /jsonrpc should not exist (404 or handled by "/" fallback)
	body := []byte(`{"jsonrpc":"2.0","method":"system.getVersion","id":1}`)
	req, _ := http.NewRequest("POST", srv.URL+"/jsonrpc", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Without RPC, /jsonrpc falls through to "/" handler (websocket handler)
	// which won't handle a plain POST properly -- should not return 200 with RPC response
	if resp.StatusCode == http.StatusOK {
		// Read body to check it's not a valid JSON-RPC response
		respBody, _ := io.ReadAll(resp.Body)
		var rpcResp map[string]any
		if err := json.Unmarshal(respBody, &rpcResp); err == nil {
			if _, hasResult := rpcResp["result"]; hasResult {
				t.Fatal("expected no RPC response when RPC is not configured")
			}
		}
	}
}

func TestWebServerHandleConnectionInvalidJSON(t *testing.T) {
	pool := NewPool(log.New(io.Discard, "", 0))
	ws := NewWebServer(log.New(io.Discard, "", 0), nil, pool, 0, nil, nil, nil)
	wsSrv := httptest.NewServer(websocket.Handler(ws.handleConnection))
	defer wsSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(wsSrv.URL, "http")
	conn, err := websocket.Dial(wsURL, "", wsSrv.URL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	// Send invalid JSON to trigger unmarshal error
	if err := websocket.Message.Send(conn, []byte("not valid json")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	_ = conn.Close()
}

func TestWebServerHandleConnectionInvalidURL(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	m, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()

	pool := NewPool(log.New(io.Discard, "", 0))
	ws := NewWebServer(log.New(io.Discard, "", 0), m, pool, 0, nil, nil, nil)
	wsSrv := httptest.NewServer(websocket.Handler(ws.handleConnection))
	defer wsSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(wsSrv.URL, "http")
	conn, err := websocket.Dial(wsURL, "", wsSrv.URL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	// Send valid JSON with invalid URL to trigger processDownload error
	payload, _ := json.Marshal(capturedDownload{Url: "http://invalid.invalid/file"})
	if err := websocket.Message.Send(conn, payload); err != nil {
		t.Fatalf("Send: %v", err)
	}
	time.Sleep(50 * time.Millisecond) // Give time for error to be processed
	_ = conn.Close()
}

func TestWebServerProcessDownloadInvalidURL(t *testing.T) {
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	m, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()

	pool := NewPool(log.New(io.Discard, "", 0))
	ws := NewWebServer(log.New(io.Discard, "", 0), m, pool, 0, nil, nil, nil)
	// Test with malformed URL
	err = ws.processDownload(&capturedDownload{Url: "://invalid"})
	if err == nil {
		t.Fatalf("expected error for invalid URL")
	}
}

func TestWebServerStart(t *testing.T) {
	pool := NewPool(log.New(io.Discard, "", 0))
	// Use port 0 to get a random available port
	ws := NewWebServer(log.New(io.Discard, "", 0), nil, pool, 0, nil, nil, nil)

	// Start in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- ws.Start()
	}()

	// Wait a bit for server to start
	time.Sleep(100 * time.Millisecond)

	// Shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := ws.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// Check that Start returned without error (ErrServerClosed is expected)
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after shutdown")
	}
}

func TestWebServerShutdown_NilServer(t *testing.T) {
	pool := NewPool(log.New(io.Discard, "", 0))
	ws := NewWebServer(log.New(io.Discard, "", 0), nil, pool, 0, nil, nil, nil)

	// Shutdown without starting should be safe
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := ws.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown with nil server failed: %v", err)
	}
}

func TestWebServerShutdown_MultipleShutdowns(t *testing.T) {
	pool := NewPool(log.New(io.Discard, "", 0))
	ws := NewWebServer(log.New(io.Discard, "", 0), nil, pool, 0, nil, nil, nil)

	go func() {
		_ = ws.Start()
	}()

	time.Sleep(100 * time.Millisecond)

	ctx1, cancel1 := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel1()
	if err := ws.Shutdown(ctx1); err != nil {
		t.Fatalf("First shutdown failed: %v", err)
	}

	// Second shutdown should be safe (server is nil after first shutdown returns ErrServerClosed)
	time.Sleep(50 * time.Millisecond)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel2()
	// Note: This may or may not return error depending on timing, but shouldn't panic
	_ = ws.Shutdown(ctx2)
}

func TestNewWebServer(t *testing.T) {
	pool := NewPool(log.New(io.Discard, "", 0))
	ws := NewWebServer(log.New(io.Discard, "", 0), nil, pool, 8080, nil, nil, nil)
	if ws == nil {
		t.Fatal("expected non-nil WebServer")
	}
	if ws.port != 8080 {
		t.Fatalf("expected port 8080, got %d", ws.port)
	}
}
