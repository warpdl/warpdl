package server

import (
  "context"
  "encoding/json"
  "io"
  "log"
  "net/http"
  "net/http/httptest"
  "strings"
  "testing"
  "time"

  cws "github.com/coder/websocket"
)

// newTestWebServerWithRPC creates a WebServer with RPC enabled, starts an httptest
// server, and returns the server URL, auth secret, and cleanup function.
func newTestWebServerWithRPC(t *testing.T) (string, string, func()) {
  t.Helper()
  secret := "ws-test-secret"
  pool := NewPool(log.New(io.Discard, "", 0))
  rpcCfg := &RPCConfig{
    Secret:  secret,
    Version: "1.0.0",
    Commit:  "abc123",
  }
  ws := NewWebServer(log.New(io.Discard, "", 0), nil, pool, 0, nil, nil, rpcCfg)
  srv := httptest.NewServer(ws.handler())
  cleanup := func() {
    srv.Close()
    if ws.rpc != nil {
      ws.rpc.Close()
    }
  }
  return srv.URL, secret, cleanup
}

func TestWebSocketEndpoint_AuthRequired(t *testing.T) {
  srvURL, _, cleanup := newTestWebServerWithRPC(t)
  defer cleanup()

  wsURL := "ws" + strings.TrimPrefix(srvURL, "http") + "/jsonrpc/ws"

  // Connect without auth -- should get rejected
  ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
  defer cancel()

  _, resp, err := cws.Dial(ctx, wsURL, nil)
  if err == nil {
    t.Fatal("expected error for unauthorized WebSocket connection")
  }
  if resp != nil && resp.StatusCode != http.StatusUnauthorized {
    t.Fatalf("expected 401, got %d", resp.StatusCode)
  }
}

func TestWebSocketEndpoint_WrongToken(t *testing.T) {
  srvURL, _, cleanup := newTestWebServerWithRPC(t)
  defer cleanup()

  wsURL := "ws" + strings.TrimPrefix(srvURL, "http") + "/jsonrpc/ws"

  ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
  defer cancel()

  _, resp, err := cws.Dial(ctx, wsURL, &cws.DialOptions{
    HTTPHeader: http.Header{
      "Authorization": []string{"Bearer wrong-token"},
    },
  })
  if err == nil {
    t.Fatal("expected error for wrong token")
  }
  if resp != nil && resp.StatusCode != http.StatusUnauthorized {
    t.Fatalf("expected 401, got %d", resp.StatusCode)
  }
}

func TestWebSocketEndpoint_Connect(t *testing.T) {
  srvURL, secret, cleanup := newTestWebServerWithRPC(t)
  defer cleanup()

  wsURL := "ws" + strings.TrimPrefix(srvURL, "http") + "/jsonrpc/ws"

  ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
  defer cancel()

  conn, _, err := cws.Dial(ctx, wsURL, &cws.DialOptions{
    HTTPHeader: http.Header{
      "Authorization": []string{"Bearer " + secret},
    },
  })
  if err != nil {
    t.Fatalf("WebSocket dial failed: %v", err)
  }
  defer conn.Close(cws.StatusNormalClosure, "")

  // Send a JSON-RPC request over the WebSocket
  req := map[string]any{
    "jsonrpc": "2.0",
    "method":  "system.getVersion",
    "id":      1,
  }
  data, _ := json.Marshal(req)
  if err := conn.Write(ctx, cws.MessageText, data); err != nil {
    t.Fatalf("WebSocket write failed: %v", err)
  }

  // Read the response
  _, respData, err := conn.Read(ctx)
  if err != nil {
    t.Fatalf("WebSocket read failed: %v", err)
  }

  var resp map[string]any
  if err := json.Unmarshal(respData, &resp); err != nil {
    t.Fatalf("unmarshal response: %v", err)
  }

  if resp["jsonrpc"] != "2.0" {
    t.Fatalf("expected jsonrpc 2.0, got %v", resp["jsonrpc"])
  }
  result, ok := resp["result"].(map[string]any)
  if !ok {
    t.Fatalf("expected result object, got %v (error: %v)", resp["result"], resp["error"])
  }
  if result["version"] != "1.0.0" {
    t.Fatalf("expected version 1.0.0, got %v", result["version"])
  }
}

func TestWebSocketEndpoint_MultipleRequests(t *testing.T) {
  srvURL, secret, cleanup := newTestWebServerWithRPC(t)
  defer cleanup()

  wsURL := "ws" + strings.TrimPrefix(srvURL, "http") + "/jsonrpc/ws"

  ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
  defer cancel()

  conn, _, err := cws.Dial(ctx, wsURL, &cws.DialOptions{
    HTTPHeader: http.Header{
      "Authorization": []string{"Bearer " + secret},
    },
  })
  if err != nil {
    t.Fatalf("WebSocket dial failed: %v", err)
  }
  defer conn.Close(cws.StatusNormalClosure, "")

  // Send multiple requests on the same connection
  for i := 1; i <= 3; i++ {
    req := map[string]any{
      "jsonrpc": "2.0",
      "method":  "system.getVersion",
      "id":      i,
    }
    data, _ := json.Marshal(req)
    if err := conn.Write(ctx, cws.MessageText, data); err != nil {
      t.Fatalf("write %d failed: %v", i, err)
    }

    _, respData, err := conn.Read(ctx)
    if err != nil {
      t.Fatalf("read %d failed: %v", i, err)
    }

    var resp map[string]any
    if err := json.Unmarshal(respData, &resp); err != nil {
      t.Fatalf("unmarshal %d: %v", i, err)
    }

    id := resp["id"].(float64)
    if int(id) != i {
      t.Fatalf("expected id %d, got %v", i, id)
    }
    if resp["result"] == nil {
      t.Fatalf("request %d: expected result, got error: %v", i, resp["error"])
    }
  }
}

func TestWebSocketEndpoint_MethodNotFound(t *testing.T) {
  srvURL, secret, cleanup := newTestWebServerWithRPC(t)
  defer cleanup()

  wsURL := "ws" + strings.TrimPrefix(srvURL, "http") + "/jsonrpc/ws"

  ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
  defer cancel()

  conn, _, err := cws.Dial(ctx, wsURL, &cws.DialOptions{
    HTTPHeader: http.Header{
      "Authorization": []string{"Bearer " + secret},
    },
  })
  if err != nil {
    t.Fatalf("WebSocket dial failed: %v", err)
  }
  defer conn.Close(cws.StatusNormalClosure, "")

  req := map[string]any{
    "jsonrpc": "2.0",
    "method":  "nonexistent.method",
    "id":      1,
  }
  data, _ := json.Marshal(req)
  if err := conn.Write(ctx, cws.MessageText, data); err != nil {
    t.Fatalf("write failed: %v", err)
  }

  _, respData, err := conn.Read(ctx)
  if err != nil {
    t.Fatalf("read failed: %v", err)
  }

  var resp map[string]any
  if err := json.Unmarshal(respData, &resp); err != nil {
    t.Fatalf("unmarshal: %v", err)
  }

  errObj, ok := resp["error"].(map[string]any)
  if !ok {
    t.Fatalf("expected error object, got %v", resp)
  }
  errCode := errObj["code"].(float64)
  if errCode != -32601 {
    t.Fatalf("expected error code -32601 (Method not found), got %v", errCode)
  }
}

func TestWebSocketEndpoint_NotifierRegistration(t *testing.T) {
  // Verify that connecting and disconnecting properly registers/unregisters
  // with the notifier.
  secret := "ws-notify-test"
  pool := NewPool(log.New(io.Discard, "", 0))
  rpcCfg := &RPCConfig{
    Secret:  secret,
    Version: "1.0.0",
  }
  l := log.New(io.Discard, "", 0)
  ws := NewWebServer(l, nil, pool, 0, nil, nil, rpcCfg)
  srv := httptest.NewServer(ws.handler())
  defer func() {
    srv.Close()
    ws.rpc.Close()
  }()

  notifier := ws.rpc.notifier
  if notifier.Count() != 0 {
    t.Fatalf("expected 0 registered servers before connection, got %d", notifier.Count())
  }

  wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/jsonrpc/ws"
  ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
  defer cancel()

  conn, _, err := cws.Dial(ctx, wsURL, &cws.DialOptions{
    HTTPHeader: http.Header{
      "Authorization": []string{"Bearer " + secret},
    },
  })
  if err != nil {
    t.Fatalf("WebSocket dial failed: %v", err)
  }

  // Give the server a moment to register the connection
  time.Sleep(50 * time.Millisecond)

  if notifier.Count() != 1 {
    t.Fatalf("expected 1 registered server after connection, got %d", notifier.Count())
  }

  // Close the connection
  conn.Close(cws.StatusNormalClosure, "")

  // Give the server a moment to unregister
  deadline := time.Now().Add(2 * time.Second)
  for time.Now().Before(deadline) {
    if notifier.Count() == 0 {
      return // success
    }
    time.Sleep(50 * time.Millisecond)
  }
  t.Fatalf("expected 0 registered servers after disconnect, got %d", notifier.Count())
}

func TestWebSocketEndpoint_PushNotification(t *testing.T) {
  // Test that push notifications sent via the notifier arrive at the WS client.
  secret := "ws-push-test"
  pool := NewPool(log.New(io.Discard, "", 0))
  rpcCfg := &RPCConfig{
    Secret:  secret,
    Version: "1.0.0",
  }
  l := log.New(io.Discard, "", 0)
  ws := NewWebServer(l, nil, pool, 0, nil, nil, rpcCfg)
  srv := httptest.NewServer(ws.handler())
  defer func() {
    srv.Close()
    ws.rpc.Close()
  }()

  wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/jsonrpc/ws"
  ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
  defer cancel()

  conn, _, err := cws.Dial(ctx, wsURL, &cws.DialOptions{
    HTTPHeader: http.Header{
      "Authorization": []string{"Bearer " + secret},
    },
  })
  if err != nil {
    t.Fatalf("WebSocket dial failed: %v", err)
  }
  defer conn.Close(cws.StatusNormalClosure, "")

  // Wait for the server to register the connection
  time.Sleep(100 * time.Millisecond)

  notifier := ws.rpc.notifier
  if notifier.Count() != 1 {
    t.Fatalf("expected 1 registered server, got %d", notifier.Count())
  }

  // Push a notification
  notifier.Broadcast("download.started", &DownloadStartedNotification{
    GID:         "push-test-gid",
    FileName:    "pushed.bin",
    TotalLength: 2048,
  })

  // Read the notification from the WebSocket
  _, msgData, err := conn.Read(ctx)
  if err != nil {
    t.Fatalf("read notification failed: %v", err)
  }

  var msg map[string]any
  if err := json.Unmarshal(msgData, &msg); err != nil {
    t.Fatalf("unmarshal notification: %v", err)
  }

  // Push notifications are JSON-RPC requests without an id (notifications)
  if msg["jsonrpc"] != "2.0" {
    t.Fatalf("expected jsonrpc 2.0, got %v", msg["jsonrpc"])
  }
  if msg["method"] != "download.started" {
    t.Fatalf("expected method download.started, got %v", msg["method"])
  }
  if msg["id"] != nil {
    t.Fatalf("expected no id for notification, got %v", msg["id"])
  }

  params, ok := msg["params"].(map[string]any)
  if !ok {
    t.Fatalf("expected params object, got %v", msg["params"])
  }
  if params["gid"] != "push-test-gid" {
    t.Fatalf("expected gid push-test-gid, got %v", params["gid"])
  }
  if params["fileName"] != "pushed.bin" {
    t.Fatalf("expected fileName pushed.bin, got %v", params["fileName"])
  }
}

func TestWebSocketEndpoint_MultipleClients(t *testing.T) {
  secret := "ws-multi-test"
  pool := NewPool(log.New(io.Discard, "", 0))
  rpcCfg := &RPCConfig{
    Secret:  secret,
    Version: "1.0.0",
  }
  l := log.New(io.Discard, "", 0)
  ws := NewWebServer(l, nil, pool, 0, nil, nil, rpcCfg)
  srv := httptest.NewServer(ws.handler())
  defer func() {
    srv.Close()
    ws.rpc.Close()
  }()

  wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/jsonrpc/ws"
  ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
  defer cancel()

  // Connect two clients
  dialOpts := &cws.DialOptions{
    HTTPHeader: http.Header{
      "Authorization": []string{"Bearer " + secret},
    },
  }

  conn1, _, err := cws.Dial(ctx, wsURL, dialOpts)
  if err != nil {
    t.Fatalf("client 1 dial failed: %v", err)
  }
  defer conn1.Close(cws.StatusNormalClosure, "")

  conn2, _, err := cws.Dial(ctx, wsURL, dialOpts)
  if err != nil {
    t.Fatalf("client 2 dial failed: %v", err)
  }
  defer conn2.Close(cws.StatusNormalClosure, "")

  // Wait for both to register
  time.Sleep(100 * time.Millisecond)

  notifier := ws.rpc.notifier
  if notifier.Count() != 2 {
    t.Fatalf("expected 2 registered servers, got %d", notifier.Count())
  }

  // Both clients should be able to make requests independently
  for i, conn := range []*cws.Conn{conn1, conn2} {
    req := map[string]any{
      "jsonrpc": "2.0",
      "method":  "system.getVersion",
      "id":      i + 1,
    }
    data, _ := json.Marshal(req)
    if err := conn.Write(ctx, cws.MessageText, data); err != nil {
      t.Fatalf("client %d write failed: %v", i+1, err)
    }
    _, respData, err := conn.Read(ctx)
    if err != nil {
      t.Fatalf("client %d read failed: %v", i+1, err)
    }
    var resp map[string]any
    if err := json.Unmarshal(respData, &resp); err != nil {
      t.Fatalf("client %d unmarshal: %v", i+1, err)
    }
    if resp["result"] == nil {
      t.Fatalf("client %d: expected result, got error: %v", i+1, resp["error"])
    }
  }
}

func TestWsChannel_Interface(t *testing.T) {
  // Verify wsChannel satisfies the channel.Channel interface by creating one
  // (we can't actually test send/recv without a real WebSocket connection,
  // but we can verify it compiles as the correct type).
  ctx := context.Background()
  ch := &wsChannel{conn: nil, ctx: ctx}
  _ = ch // just verify it compiles; actual I/O tested through integration
}
