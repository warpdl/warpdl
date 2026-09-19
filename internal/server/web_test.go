package server

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
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
		w.Header().Set("Content-Range",
			"bytes "+strconv.Itoa(start)+"-"+strconv.Itoa(end)+"/"+strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(chunk)
	}))
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
	rpcCfg := &RPCConfig{ListenAll: true}
	ws := NewWebServer(log.New(io.Discard, "", 0), nil, pool, 9999, nil, nil, rpcCfg)
	addr := ws.addr()
	if addr != ":9999" {
		t.Fatalf("expected :9999 with listenAll, got %s", addr)
	}
}

func TestWebServerHandler_WithRPC(t *testing.T) {
	pool := NewPool(log.New(io.Discard, "", 0))
	rpcCfg := &RPCConfig{
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

	// The legacy capture WebSocket at "/" is gone: unregistered paths 404.
	rootResp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("root request failed: %v", err)
	}
	defer rootResp.Body.Close()
	if rootResp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET / status = %d, want 404", rootResp.StatusCode)
	}
}

func TestWebServerHandler_WithoutRPC(t *testing.T) {
	pool := NewPool(log.New(io.Discard, "", 0))
	ws := NewWebServer(log.New(io.Discard, "", 0), nil, pool, 8080, nil, nil, nil)

	srv := httptest.NewServer(ws.handler())
	defer srv.Close()

	body := []byte(`{"jsonrpc":"2.0","method":"system.getVersion","id":1}`)
	for _, path := range []string{"/jsonrpc", "/jsonrpc/ws"} {
		req, _ := http.NewRequest("POST", srv.URL+path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s request failed: %v", path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404 without RPC", path, resp.StatusCode)
		}
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
