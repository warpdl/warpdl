package server

import (
	"bytes"
	"io"
	"log"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

type synchronizedLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *synchronizedLogBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(data)
}

func (b *synchronizedLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestSanitizedURLRemovesCredentialsAndSecrets(t *testing.T) {
	const raw = "https://user:password@example.com/download/file.bin?token=secret#private"
	if got, want := sanitizedURL(raw), "https://example.com/download/file.bin"; got != want {
		t.Fatalf("sanitizedURL() = %q, want %q", got, want)
	}
	if got := sanitizedURL("://malformed"); got != "<invalid-url>" {
		t.Fatalf("sanitized malformed URL = %q", got)
	}
}

func TestWebSocketLogsDoNotExposeInvalidPayload(t *testing.T) {
	var logs synchronizedLogBuffer
	logger := log.New(&logs, "", 0)
	webServer := NewWebServer(logger, nil, NewPool(log.New(io.Discard, "", 0)), 0, nil, nil, nil)
	server := httptest.NewServer(websocket.Handler(webServer.handleConnection))
	defer server.Close()

	websocketURL := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, err := websocket.Dial(websocketURL, "", server.URL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	const secret = "raw-cookie-secret"
	payload := []byte(`{"headers":[{"key":"Cookie","value":"` + secret + `"}`)
	if err := websocket.Message.Send(conn, payload); err != nil {
		t.Fatalf("Send: %v", err)
	}
	defer conn.Close()

	deadline := time.Now().Add(time.Second)
	for !strings.Contains(logs.String(), "Error unmarshalling") && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	got := logs.String()
	if !strings.Contains(got, "Error unmarshalling") {
		t.Fatalf("missing decode error log: %s", got)
	}
	if strings.Contains(got, secret) || strings.Contains(got, string(payload)) {
		t.Fatalf("log exposed raw websocket payload: %s", got)
	}
}
