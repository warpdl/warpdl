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
  "path/filepath"
  "strings"
  "testing"
  "time"

  cws "github.com/coder/websocket"
  "github.com/warpdl/warpdl/pkg/warplib"
)

const integrationSecret = "integration-test-secret-42"

// startIntegrationServer starts a WebServer with RPC enabled and a mock HTTP
// download target. Returns the server URL, target server URL, download directory,
// manager, and cleanup. The dlDir is a temp directory where downloads are saved,
// preventing files from being written to the source tree.
func startIntegrationServer(t *testing.T) (serverURL, targetURL, dlDir string, m *warplib.Manager, cleanup func()) {
  t.Helper()
  base := t.TempDir()
  if err := warplib.SetConfigDir(base); err != nil {
    t.Fatalf("SetConfigDir: %v", err)
  }
  var err error
  m, err = warplib.InitManager()
  if err != nil {
    t.Fatalf("InitManager: %v", err)
  }

  dlDir = filepath.Join(base, "downloads")
  if err := os.MkdirAll(dlDir, 0755); err != nil {
    t.Fatalf("MkdirAll downloads: %v", err)
  }

  content := bytes.Repeat([]byte("integration-"), 1024) // ~12KB file
  targetSrv := newRangeServer(content)

  pool := NewPool(log.New(io.Discard, "", 0))
  // Pre-set CheckRedirect to avoid a race in NewDownloader which mutates
  // the shared client when concurrent download.add calls are made.
  client := &http.Client{
    CheckRedirect: warplib.RedirectPolicy(warplib.DefaultMaxRedirects),
  }

  rpcCfg := &RPCConfig{
    Secret:    integrationSecret,
    Version:   "1.0.0-test",
    Commit:    "abc123",
    BuildType: "integration",
  }

  l := log.New(io.Discard, "", 0)
  ws := NewWebServer(l, m, pool, 0, client, nil, rpcCfg)
  httpSrv := httptest.NewServer(ws.handler())

  cleanup = func() {
    httpSrv.Close()
    if ws.rpc != nil {
      ws.rpc.Close()
    }
    m.Close()
    targetSrv.Close()
  }
  return httpSrv.URL, targetSrv.URL, dlDir, m, cleanup
}

// rpcPost sends a JSON-RPC request via HTTP POST with auth and returns the response.
func rpcPost(t *testing.T, serverURL, method string, params any) (int, map[string]any) {
  t.Helper()
  reqBody := map[string]any{
    "jsonrpc": "2.0",
    "method":  method,
    "id":      1,
  }
  if params != nil {
    reqBody["params"] = params
  }
  data, _ := json.Marshal(reqBody)
  req, _ := http.NewRequest("POST", serverURL+"/jsonrpc", bytes.NewReader(data))
  req.Header.Set("Content-Type", "application/json")
  req.Header.Set("Authorization", "Bearer "+integrationSecret)
  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    t.Fatalf("HTTP request failed: %v", err)
  }
  defer resp.Body.Close()
  body, _ := io.ReadAll(resp.Body)
  var result map[string]any
  if len(body) > 0 {
    if err := json.Unmarshal(body, &result); err != nil {
      t.Fatalf("unmarshal: %v (body: %s)", err, string(body))
    }
  }
  return resp.StatusCode, result
}

// rpcPostRaw sends raw bytes to the RPC endpoint with auth.
func rpcPostRaw(t *testing.T, serverURL string, body []byte, authToken string) (int, map[string]any) {
  t.Helper()
  req, _ := http.NewRequest("POST", serverURL+"/jsonrpc", bytes.NewReader(body))
  req.Header.Set("Content-Type", "application/json")
  if authToken != "" {
    req.Header.Set("Authorization", "Bearer "+authToken)
  }
  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    t.Fatalf("HTTP request failed: %v", err)
  }
  defer resp.Body.Close()
  respBody, _ := io.ReadAll(resp.Body)
  var result map[string]any
  if len(respBody) > 0 {
    _ = json.Unmarshal(respBody, &result)
  }
  return resp.StatusCode, result
}

// wsConnectIntegration connects a WebSocket client with auth to the test server.
func wsConnectIntegration(t *testing.T, serverURL string) *cws.Conn {
  t.Helper()
  wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/jsonrpc/ws"
  ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
  defer cancel()
  conn, _, err := cws.Dial(ctx, wsURL, &cws.DialOptions{
    HTTPHeader: http.Header{
      "Authorization": []string{"Bearer " + integrationSecret},
    },
  })
  if err != nil {
    t.Fatalf("WebSocket dial failed: %v", err)
  }
  return conn
}

// waitForStatus polls download.status until the expected status or timeout.
func waitForStatus(t *testing.T, serverURL, gid, expectedStatus string, timeout time.Duration) {
  t.Helper()
  deadline := time.Now().Add(timeout)
  var lastStatus string
  var lastCompleted, lastTotal float64
  for time.Now().Before(deadline) {
    _, resp := rpcPost(t, serverURL, "download.status", map[string]any{"gid": gid})
    if result, ok := resp["result"].(map[string]any); ok {
      lastStatus, _ = result["status"].(string)
      lastCompleted, _ = result["completedLength"].(float64)
      lastTotal, _ = result["totalLength"].(float64)
      if lastStatus == expectedStatus {
        return
      }
    }
    if resp["error"] != nil {
      t.Fatalf("status error: %v", resp["error"])
    }
    time.Sleep(50 * time.Millisecond)
  }
  t.Fatalf("timed out waiting for status %q on gid %s (last: status=%q completed=%.0f total=%.0f)",
    expectedStatus, gid, lastStatus, lastCompleted, lastTotal)
}

// --- RPC-01: HTTP endpoint returns JSON-RPC 2.0 response ---

func TestIntegration_HTTPEndpoint(t *testing.T) {
  serverURL, _, _, _, cleanup := startIntegrationServer(t)
  defer cleanup()

  code, resp := rpcPost(t, serverURL, "system.getVersion", nil)
  if code != http.StatusOK {
    t.Fatalf("expected 200, got %d", code)
  }
  if resp["jsonrpc"] != "2.0" {
    t.Fatalf("expected jsonrpc 2.0, got %v", resp["jsonrpc"])
  }
  // id should match what we sent
  if resp["id"].(float64) != 1 {
    t.Fatalf("expected id 1, got %v", resp["id"])
  }
  if resp["result"] == nil {
    t.Fatal("expected result in response")
  }
}

// --- RPC-02: WebSocket endpoint ---

func TestIntegration_WebSocketEndpoint(t *testing.T) {
  serverURL, _, _, _, cleanup := startIntegrationServer(t)
  defer cleanup()

  conn := wsConnectIntegration(t, serverURL)
  defer conn.Close(cws.StatusNormalClosure, "")

  ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
  defer cancel()

  req := map[string]any{"jsonrpc": "2.0", "method": "system.getVersion", "id": 1}
  data, _ := json.Marshal(req)
  if err := conn.Write(ctx, cws.MessageText, data); err != nil {
    t.Fatalf("write: %v", err)
  }

  _, respData, err := conn.Read(ctx)
  if err != nil {
    t.Fatalf("read: %v", err)
  }
  var resp map[string]any
  if err := json.Unmarshal(respData, &resp); err != nil {
    t.Fatalf("unmarshal: %v", err)
  }
  if resp["result"] == nil {
    t.Fatalf("expected result, got error: %v", resp["error"])
  }
}

// --- RPC-03: Auth enforcement ---

func TestIntegration_AuthEnforcement_HTTP(t *testing.T) {
  serverURL, _, _, _, cleanup := startIntegrationServer(t)
  defer cleanup()

  // No auth
  code, _ := rpcPostRaw(t, serverURL, []byte(`{"jsonrpc":"2.0","method":"system.getVersion","id":1}`), "")
  if code != http.StatusUnauthorized {
    t.Fatalf("expected 401 without auth, got %d", code)
  }

  // Wrong token
  code, _ = rpcPostRaw(t, serverURL, []byte(`{"jsonrpc":"2.0","method":"system.getVersion","id":1}`), "wrong-token")
  if code != http.StatusUnauthorized {
    t.Fatalf("expected 401 with wrong token, got %d", code)
  }

  // Correct token
  code, _ = rpcPostRaw(t, serverURL, []byte(`{"jsonrpc":"2.0","method":"system.getVersion","id":1}`), integrationSecret)
  if code != http.StatusOK {
    t.Fatalf("expected 200 with correct token, got %d", code)
  }
}

func TestIntegration_AuthEnforcement_WS(t *testing.T) {
  serverURL, _, _, _, cleanup := startIntegrationServer(t)
  defer cleanup()

  wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/jsonrpc/ws"
  ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
  defer cancel()

  // No auth
  _, resp, err := cws.Dial(ctx, wsURL, nil)
  if err == nil {
    t.Fatal("expected error for WS without auth")
  }
  if resp != nil && resp.StatusCode != http.StatusUnauthorized {
    t.Fatalf("expected 401, got %d", resp.StatusCode)
  }

  // Wrong auth
  _, resp, err = cws.Dial(ctx, wsURL, &cws.DialOptions{
    HTTPHeader: http.Header{
      "Authorization": []string{"Bearer wrong"},
    },
  })
  if err == nil {
    t.Fatal("expected error for WS with wrong auth")
  }
  if resp != nil && resp.StatusCode != http.StatusUnauthorized {
    t.Fatalf("expected 401, got %d", resp.StatusCode)
  }

  // Correct auth
  conn, _, err := cws.Dial(ctx, wsURL, &cws.DialOptions{
    HTTPHeader: http.Header{
      "Authorization": []string{"Bearer " + integrationSecret},
    },
  })
  if err != nil {
    t.Fatalf("expected successful WS connection, got %v", err)
  }
  conn.Close(cws.StatusNormalClosure, "")
}

// --- RPC-04: Localhost binding ---

func TestIntegration_LocalhostBinding(t *testing.T) {
  pool := NewPool(log.New(io.Discard, "", 0))
  ws := NewWebServer(log.New(io.Discard, "", 0), nil, pool, 9999, nil, nil, nil)
  addr := ws.addr()
  if !strings.HasPrefix(addr, "127.0.0.1:") {
    t.Fatalf("expected 127.0.0.1 binding, got %s", addr)
  }
}

// --- RPC-05: download.add ---

func TestIntegration_DownloadAdd(t *testing.T) {
  serverURL, targetURL, dlDir, m, cleanup := startIntegrationServer(t)
  defer cleanup()

  code, resp := rpcPost(t, serverURL, "download.add", map[string]any{
    "url": targetURL + "/integration-test.bin",
    "dir": dlDir,
  })
  if code != http.StatusOK {
    t.Fatalf("expected 200, got %d", code)
  }
  result := resp["result"].(map[string]any)
  gid := result["gid"].(string)
  if gid == "" {
    t.Fatal("expected non-empty gid")
  }

  // Verify manager has the item
  deadline := time.Now().Add(2 * time.Second)
  for time.Now().Before(deadline) {
    if m.GetItem(gid) != nil {
      return
    }
    time.Sleep(10 * time.Millisecond)
  }
  t.Fatal("download item not found in manager")
}

// --- RPC-07: download.remove ---

func TestIntegration_DownloadRemove(t *testing.T) {
  serverURL, targetURL, dlDir, m, cleanup := startIntegrationServer(t)
  defer cleanup()

  // Add a download
  _, addResp := rpcPost(t, serverURL, "download.add", map[string]any{
    "url": targetURL + "/remove-integration.bin",
    "dir": dlDir,
  })
  gid := addResp["result"].(map[string]any)["gid"].(string)

  // Wait for download to complete
  waitForStatus(t, serverURL, gid, "complete", 10*time.Second)

  // Stop the download (clear dAlloc so FlushOne accepts)
  item := m.GetItem(gid)
  if item != nil {
    _ = item.StopDownload()
  }

  // Remove
  code, resp := rpcPost(t, serverURL, "download.remove", map[string]any{"gid": gid})
  if code != http.StatusOK {
    t.Fatalf("expected 200, got %d", code)
  }
  if resp["error"] != nil {
    t.Fatalf("expected no error, got %v", resp["error"])
  }

  // Verify removed: status should return error
  _, statusResp := rpcPost(t, serverURL, "download.status", map[string]any{"gid": gid})
  if statusResp["error"] == nil {
    t.Fatal("expected error for removed download status")
  }
}

// --- RPC-08: download.status ---

func TestIntegration_DownloadStatus(t *testing.T) {
  serverURL, targetURL, dlDir, _, cleanup := startIntegrationServer(t)
  defer cleanup()

  // Add a download
  _, addResp := rpcPost(t, serverURL, "download.add", map[string]any{
    "url": targetURL + "/status-integration.bin",
    "dir": dlDir,
  })
  gid := addResp["result"].(map[string]any)["gid"].(string)

  // Wait for it to appear in the manager
  time.Sleep(100 * time.Millisecond)

  code, resp := rpcPost(t, serverURL, "download.status", map[string]any{"gid": gid})
  if code != http.StatusOK {
    t.Fatalf("expected 200, got %d", code)
  }
  result := resp["result"].(map[string]any)
  if result["gid"] != gid {
    t.Fatalf("expected gid %s, got %v", gid, result["gid"])
  }
  status, ok := result["status"].(string)
  if !ok {
    t.Fatalf("expected status string, got %v", result["status"])
  }
  switch status {
  case "active", "waiting", "complete":
    // valid
  default:
    t.Fatalf("unexpected status %q", status)
  }
  if result["fileName"] == nil {
    t.Fatal("expected fileName in status")
  }
}

// --- RPC-09: download.list ---

func TestIntegration_DownloadList(t *testing.T) {
  serverURL, targetURL, dlDir, _, cleanup := startIntegrationServer(t)
  defer cleanup()

  // Add two downloads
  _, resp1 := rpcPost(t, serverURL, "download.add", map[string]any{
    "url": targetURL + "/list-1.bin",
    "dir": dlDir,
  })
  gid1 := resp1["result"].(map[string]any)["gid"].(string)

  _, resp2 := rpcPost(t, serverURL, "download.add", map[string]any{
    "url": targetURL + "/list-2.bin",
    "dir": dlDir,
  })
  gid2 := resp2["result"].(map[string]any)["gid"].(string)

  // Wait for both to complete
  waitForStatus(t, serverURL, gid1, "complete", 10*time.Second)
  waitForStatus(t, serverURL, gid2, "complete", 10*time.Second)

  // List all
  code, listResp := rpcPost(t, serverURL, "download.list", map[string]any{"status": "all"})
  if code != http.StatusOK {
    t.Fatalf("expected 200, got %d", code)
  }
  result := listResp["result"].(map[string]any)
  downloads := result["downloads"].([]any)
  if len(downloads) < 2 {
    t.Fatalf("expected at least 2 downloads, got %d", len(downloads))
  }

  // List complete
  _, completeResp := rpcPost(t, serverURL, "download.list", map[string]any{"status": "complete"})
  completeResult := completeResp["result"].(map[string]any)
  completeDls := completeResult["downloads"].([]any)
  if len(completeDls) < 2 {
    t.Fatalf("expected at least 2 complete downloads, got %d", len(completeDls))
  }
}

// --- RPC-10: system.getVersion ---

func TestIntegration_SystemGetVersion(t *testing.T) {
  serverURL, _, _, _, cleanup := startIntegrationServer(t)
  defer cleanup()

  code, resp := rpcPost(t, serverURL, "system.getVersion", nil)
  if code != http.StatusOK {
    t.Fatalf("expected 200, got %d", code)
  }
  result := resp["result"].(map[string]any)
  if result["version"] != "1.0.0-test" {
    t.Fatalf("expected version 1.0.0-test, got %v", result["version"])
  }
  if result["commit"] != "abc123" {
    t.Fatalf("expected commit abc123, got %v", result["commit"])
  }
  if result["buildType"] != "integration" {
    t.Fatalf("expected buildType integration, got %v", result["buildType"])
  }
}

// --- RPC-11: WebSocket notifications ---

func TestIntegration_WebSocketNotifications(t *testing.T) {
  serverURL, targetURL, dlDir, _, cleanup := startIntegrationServer(t)
  defer cleanup()

  // Connect WS client
  conn := wsConnectIntegration(t, serverURL)
  defer conn.Close(cws.StatusNormalClosure, "")

  // Wait for registration
  time.Sleep(100 * time.Millisecond)

  // Add a download (the started notification should be pushed)
  _, addResp := rpcPost(t, serverURL, "download.add", map[string]any{
    "url": targetURL + "/ws-notify.bin",
    "dir": dlDir,
  })
  if addResp["error"] != nil {
    t.Fatalf("download.add error: %v", addResp["error"])
  }

  // Collect notifications from the WebSocket
  ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
  defer cancel()

  methods := make(map[string]bool)
  for {
    _, msgData, err := conn.Read(ctx)
    if err != nil {
      break
    }
    var msg map[string]any
    if err := json.Unmarshal(msgData, &msg); err != nil {
      continue
    }
    if method, ok := msg["method"].(string); ok {
      methods[method] = true
      // Check params has gid
      if params, ok := msg["params"].(map[string]any); ok {
        if params["gid"] == nil || params["gid"] == "" {
          t.Fatalf("expected gid in notification params for %s", method)
        }
      }
    }
    // Stop once we see download.complete
    if methods["download.complete"] {
      break
    }
  }

  // Verify we received download.started
  if !methods["download.started"] {
    t.Fatal("expected download.started notification")
  }
  // Verify we received download.complete
  if !methods["download.complete"] {
    t.Fatal("expected download.complete notification")
  }
}

// --- RPC-12: Error codes ---

func TestIntegration_ErrorCodes(t *testing.T) {
  serverURL, _, _, _, cleanup := startIntegrationServer(t)
  defer cleanup()

  // Invalid JSON -> -32700 (parse error) or HTTP 500 from jhttp bridge
  code, _ := rpcPostRaw(t, serverURL, []byte("not valid json"), integrationSecret)
  if code != http.StatusInternalServerError && code != http.StatusOK {
    t.Fatalf("expected 500 or 200 for invalid JSON, got %d", code)
  }

  // Unknown method -> -32601
  _, resp := rpcPost(t, serverURL, "nonexistent.method", nil)
  errObj := resp["error"].(map[string]any)
  if errObj["code"].(float64) != -32601 {
    t.Fatalf("expected -32601 for unknown method, got %v", errObj["code"])
  }

  // download.status with unknown GID -> -32001
  _, resp = rpcPost(t, serverURL, "download.status", map[string]any{"gid": "fake-gid"})
  errObj = resp["error"].(map[string]any)
  if errObj["code"].(float64) != float64(codeDownloadNotFound) {
    t.Fatalf("expected %d for unknown gid, got %v", codeDownloadNotFound, errObj["code"])
  }

  // download.add with missing url -> -32602
  _, resp = rpcPost(t, serverURL, "download.add", map[string]any{})
  errObj = resp["error"].(map[string]any)
  if errObj["code"].(float64) != float64(codeInvalidParams) {
    t.Fatalf("expected %d for missing url, got %v", codeInvalidParams, errObj["code"])
  }
}

// --- Full lifecycle test ---

func TestIntegration_FullLifecycle(t *testing.T) {
  serverURL, targetURL, dlDir, m, cleanup := startIntegrationServer(t)
  defer cleanup()

  // Add download
  _, addResp := rpcPost(t, serverURL, "download.add", map[string]any{
    "url": targetURL + "/lifecycle.bin",
    "dir": dlDir,
  })
  gid := addResp["result"].(map[string]any)["gid"].(string)

  // Wait for completion (small file, should be fast)
  waitForStatus(t, serverURL, gid, "complete", 10*time.Second)

  // Verify status shows complete
  _, statusResp := rpcPost(t, serverURL, "download.status", map[string]any{"gid": gid})
  result := statusResp["result"].(map[string]any)
  if result["status"] != "complete" {
    t.Fatalf("expected complete, got %v", result["status"])
  }

  // Stop and remove
  item := m.GetItem(gid)
  if item != nil {
    _ = item.StopDownload()
  }
  _, removeResp := rpcPost(t, serverURL, "download.remove", map[string]any{"gid": gid})
  if removeResp["error"] != nil {
    t.Fatalf("remove error: %v", removeResp["error"])
  }

  // Status should now return error
  _, statusResp2 := rpcPost(t, serverURL, "download.status", map[string]any{"gid": gid})
  if statusResp2["error"] == nil {
    t.Fatal("expected error for removed download")
  }
  errObj := statusResp2["error"].(map[string]any)
  if errObj["code"].(float64) != float64(codeDownloadNotFound) {
    t.Fatalf("expected %d, got %v", codeDownloadNotFound, errObj["code"])
  }
}

// --- Concurrent downloads test ---

func TestIntegration_ConcurrentDownloads(t *testing.T) {
  serverURL, targetURL, dlDir, _, cleanup := startIntegrationServer(t)
  defer cleanup()

  type addResult struct {
    gid string
    err error
  }

  results := make(chan addResult, 2)

  for i := 0; i < 2; i++ {
    go func(i int) {
      _, resp := rpcPost(t, serverURL, "download.add", map[string]any{
        "url": targetURL + "/concurrent-" + string(rune('a'+i)) + ".bin",
        "dir": dlDir,
      })
      if resp["error"] != nil {
        results <- addResult{err: nil}
        return
      }
      gid := resp["result"].(map[string]any)["gid"].(string)
      results <- addResult{gid: gid}
    }(i)
  }

  gids := make([]string, 0, 2)
  for i := 0; i < 2; i++ {
    r := <-results
    if r.gid != "" {
      gids = append(gids, r.gid)
    }
  }

  if len(gids) != 2 {
    t.Fatalf("expected 2 successful downloads, got %d", len(gids))
  }

  // Wait for both to complete
  for _, gid := range gids {
    waitForStatus(t, serverURL, gid, "complete", 10*time.Second)
  }

  // List all should return at least 2
  _, listResp := rpcPost(t, serverURL, "download.list", map[string]any{"status": "all"})
  result := listResp["result"].(map[string]any)
  downloads := result["downloads"].([]any)
  if len(downloads) < 2 {
    t.Fatalf("expected at least 2 downloads in list, got %d", len(downloads))
  }
}
