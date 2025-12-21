package server

import (
	"bytes"
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
	ws := NewWebServer(log.New(io.Discard, "", 0), m, pool, 0)
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
	ws := NewWebServer(log.New(io.Discard, "", 0), m, pool, 0)
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
	ws := NewWebServer(log.New(io.Discard, "", 0), nil, pool, 8080)
	h := ws.handler()
	if h == nil {
		t.Fatalf("expected non-nil handler")
	}
}

func TestWebServerAddr(t *testing.T) {
	pool := NewPool(log.New(io.Discard, "", 0))
	ws := NewWebServer(log.New(io.Discard, "", 0), nil, pool, 9999)
	addr := ws.addr()
	if addr != ":9999" {
		t.Fatalf("expected :9999, got %s", addr)
	}
}

func TestWebServerHandleConnectionInvalidJSON(t *testing.T) {
	pool := NewPool(log.New(io.Discard, "", 0))
	ws := NewWebServer(log.New(io.Discard, "", 0), nil, pool, 0)
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
	ws := NewWebServer(log.New(io.Discard, "", 0), m, pool, 0)
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
	ws := NewWebServer(log.New(io.Discard, "", 0), m, pool, 0)
	// Test with malformed URL
	err = ws.processDownload(&capturedDownload{Url: "://invalid"})
	if err == nil {
		t.Fatalf("expected error for invalid URL")
	}
}
