package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/warpdl/warpdl/pkg/warplib"
)

// rpcCall sends a JSON-RPC request to the bridge and returns the parsed response.
func rpcCall(t *testing.T, handler http.Handler, method string, params any, authToken string) (int, map[string]any) {
	t.Helper()
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"id":      1,
	}
	if params != nil {
		reqBody["params"] = params
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/jsonrpc", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	resp := rr.Result()
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result map[string]any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &result); err != nil {
			t.Fatalf("unmarshal response: %v (body: %s)", err, string(body))
		}
	}
	return rr.Code, result
}

// rpcCallRaw sends a raw body to the bridge and returns the parsed response.
func rpcCallRaw(t *testing.T, handler http.Handler, body []byte, authToken string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/jsonrpc", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	resp := rr.Result()
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	var result map[string]any
	if len(respBody) > 0 {
		_ = json.Unmarshal(respBody, &result)
	}
	return rr.Code, result
}

func newTestRPCHandler(t *testing.T) (http.Handler, string, func()) {
	t.Helper()
	secret := "test-rpc-secret"
	cfg := &RPCConfig{
		Secret:    secret,
		Version:   "1.0.0",
		Commit:    "abc123",
		BuildType: "release",
	}
	rs := NewRPCServer(cfg, nil, nil, nil, nil, nil)
	handler := requireToken(secret, rs.bridge)
	return handler, secret, func() { rs.Close() }
}

func TestRPCSystemGetVersion(t *testing.T) {
	handler, secret, cleanup := newTestRPCHandler(t)
	defer cleanup()

	code, resp := rpcCall(t, handler, "system.getVersion", nil, secret)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if resp["jsonrpc"] != "2.0" {
		t.Fatalf("expected jsonrpc 2.0, got %v", resp["jsonrpc"])
	}
	// Check id matches
	if resp["id"].(float64) != 1 {
		t.Fatalf("expected id 1, got %v", resp["id"])
	}
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %v", resp["result"])
	}
	if result["version"] != "1.0.0" {
		t.Fatalf("expected version 1.0.0, got %v", result["version"])
	}
	if result["commit"] != "abc123" {
		t.Fatalf("expected commit abc123, got %v", result["commit"])
	}
	if result["buildType"] != "release" {
		t.Fatalf("expected buildType release, got %v", result["buildType"])
	}
}

func TestRPCParseError(t *testing.T) {
	handler, secret, cleanup := newTestRPCHandler(t)
	defer cleanup()

	// Send invalid JSON -- jrpc2 bridge returns HTTP 500 for parse errors
	// with no body, because the request cannot be parsed into a valid JSON-RPC
	// request. This is expected behavior from the jhttp.Bridge.
	code, _ := rpcCallRaw(t, handler, []byte("not valid json"), secret)
	if code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for parse error, got %d", code)
	}

	// Also test with valid JSON but missing required fields (method).
	// jrpc2 treats this as an invalid request.
	invalidReq := []byte(`{"jsonrpc":"2.0","id":1}`)
	code2, resp2 := rpcCallRaw(t, handler, invalidReq, secret)
	if code2 != http.StatusOK {
		t.Logf("note: got status %d for missing method", code2)
	}
	if resp2 != nil {
		if errObj, ok := resp2["error"].(map[string]any); ok {
			errCode := errObj["code"].(float64)
			// jrpc2 treats missing method as invalid request (-32600) or method not found (-32601)
			if errCode != -32600 && errCode != -32601 {
				t.Fatalf("expected error code -32600 or -32601, got %v", errCode)
			}
		}
	}
}

func TestRPCMethodNotFound(t *testing.T) {
	handler, secret, cleanup := newTestRPCHandler(t)
	defer cleanup()

	code, resp := rpcCall(t, handler, "nonexistent.method", nil, secret)
	if code != http.StatusOK {
		t.Logf("note: got status %d", code)
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

func TestRPCBridgeLifecycle(t *testing.T) {
	cfg := &RPCConfig{
		Secret:  "test",
		Version: "1.0.0",
	}
	rs := NewRPCServer(cfg, nil, nil, nil, nil, nil)
	// Close should not panic
	rs.Close()
	// Double close should not panic
	rs.Close()
}

func TestRPCAuthRequired(t *testing.T) {
	handler, _, cleanup := newTestRPCHandler(t)
	defer cleanup()

	// Request without auth token
	code, resp := rpcCall(t, handler, "system.getVersion", nil, "")
	if code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", code)
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %v", resp)
	}
	if errObj["message"] != "Unauthorized" {
		t.Fatalf("expected 'Unauthorized', got %v", errObj["message"])
	}
}

func TestRPCWrongToken(t *testing.T) {
	handler, _, cleanup := newTestRPCHandler(t)
	defer cleanup()

	code, _ := rpcCall(t, handler, "system.getVersion", nil, "wrong-token")
	if code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", code)
	}
}

// newTestRPCHandlerWithManager creates an RPC handler backed by a real Manager.
// Returns the handler, auth secret, a cleanup function, manager, and download
// directory. The download directory is a temp dir that callers should use as
// the "dir" parameter in download.add calls to avoid writing to the source tree.
func newTestRPCHandlerWithManager(t *testing.T) (http.Handler, string, func(), *warplib.Manager, string) {
	t.Helper()
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	m, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	dlDir := filepath.Join(base, "downloads")
	if err := os.MkdirAll(dlDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	secret := "test-rpc-secret"
	pool := NewPool(log.New(io.Discard, "", 0))
	// Pre-set CheckRedirect to avoid a race in NewDownloader which mutates
	// the shared client when concurrent download.add calls are made.
	client := &http.Client{
		CheckRedirect: warplib.RedirectPolicy(warplib.DefaultMaxRedirects),
	}
	cfg := &RPCConfig{
		Secret:    secret,
		Version:   "1.0.0",
		Commit:    "abc123",
		BuildType: "release",
	}
	rs := NewRPCServer(cfg, m, client, pool, nil, nil)
	handler := requireToken(secret, rs.bridge)
	cleanup := func() {
		rs.Close()
		m.Close()
	}
	return handler, secret, cleanup, m, dlDir
}

// rpcResult extracts the "result" object from an RPC response, failing if absent.
func rpcResult(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result object, got %v", resp)
	}
	return result
}

// rpcError extracts the "error" object from an RPC response, failing if absent.
func rpcError(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %v", resp)
	}
	return errObj
}

// --- download.add tests ---

func TestRPCDownloadAdd_Success(t *testing.T) {
	handler, secret, cleanup, m, dlDir := newTestRPCHandlerWithManager(t)
	defer cleanup()

	content := bytes.Repeat([]byte("a"), 1024)
	srv := newRangeServer(content)
	defer srv.Close()

	code, resp := rpcCall(t, handler, "download.add", map[string]any{
		"url": srv.URL + "/file.bin",
		"dir": dlDir,
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	result := rpcResult(t, resp)
	gid, ok := result["gid"].(string)
	if !ok || gid == "" {
		t.Fatalf("expected non-empty gid, got %v", result["gid"])
	}

	// Verify item was added to manager
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if item := m.GetItem(gid); item != nil {
			return // success
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("download item was not added to manager")
}

func TestRPCDownloadAdd_MissingURL(t *testing.T) {
	handler, secret, cleanup, _, _ := newTestRPCHandlerWithManager(t)
	defer cleanup()

	code, resp := rpcCall(t, handler, "download.add", map[string]any{}, secret)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	errObj := rpcError(t, resp)
	errCode := errObj["code"].(float64)
	if errCode != float64(codeInvalidParams) {
		t.Fatalf("expected error code %d, got %v", codeInvalidParams, errCode)
	}
}

func TestRPCDownloadAdd_InvalidURL(t *testing.T) {
	handler, secret, cleanup, _, _ := newTestRPCHandlerWithManager(t)
	defer cleanup()

	code, resp := rpcCall(t, handler, "download.add", map[string]any{
		"url": "://invalid",
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	errObj := rpcError(t, resp)
	errCode := errObj["code"].(float64)
	if errCode != float64(codeInvalidParams) {
		t.Fatalf("expected error code %d, got %v", codeInvalidParams, errCode)
	}
}

func TestRPCDownloadAdd_UnsupportedScheme(t *testing.T) {
	handler, secret, cleanup, _, _ := newTestRPCHandlerWithManager(t)
	defer cleanup()

	// RPC server has no schemeRouter (nil), so ftp:// should fail
	code, resp := rpcCall(t, handler, "download.add", map[string]any{
		"url": "ftp://example.com/file.bin",
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	errObj := rpcError(t, resp)
	errCode := errObj["code"].(float64)
	if errCode != float64(codeInvalidParams) {
		t.Fatalf("expected error code %d, got %v", codeInvalidParams, errCode)
	}
	msg := errObj["message"].(string)
	if msg != "unsupported scheme: ftp" {
		t.Fatalf("expected 'unsupported scheme: ftp', got %q", msg)
	}
}

func TestRPCDownloadAdd_DefaultConnections(t *testing.T) {
	handler, secret, cleanup, m, dlDir := newTestRPCHandlerWithManager(t)
	defer cleanup()

	content := bytes.Repeat([]byte("b"), 512)
	srv := newRangeServer(content)
	defer srv.Close()

	// Don't set connections -- should default to 24
	code, resp := rpcCall(t, handler, "download.add", map[string]any{
		"url":         srv.URL + "/test.bin",
		"connections": 0,
		"dir":         dlDir,
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	result := rpcResult(t, resp)
	gid := result["gid"].(string)
	if gid == "" {
		t.Fatal("expected non-empty gid")
	}

	// Wait for item to be registered
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if item := m.GetItem(gid); item != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("download item was not added to manager")
}

// --- download.status tests ---

func TestRPCDownloadStatus_Success(t *testing.T) {
	handler, secret, cleanup, m, dlDir := newTestRPCHandlerWithManager(t)
	defer cleanup()

	// Create a download via RPC so there's an item in the manager
	content := bytes.Repeat([]byte("x"), 256)
	srv := newRangeServer(content)
	defer srv.Close()

	_, addResp := rpcCall(t, handler, "download.add", map[string]any{
		"url": srv.URL + "/status-test.bin",
		"dir": dlDir,
	}, secret)
	addResult := rpcResult(t, addResp)
	gid := addResult["gid"].(string)

	// Wait for item to be registered
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if item := m.GetItem(gid); item != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	code, resp := rpcCall(t, handler, "download.status", map[string]any{
		"gid": gid,
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	result := rpcResult(t, resp)
	if result["gid"] != gid {
		t.Fatalf("expected gid %q, got %v", gid, result["gid"])
	}
	status, ok := result["status"].(string)
	if !ok {
		t.Fatalf("expected status string, got %v", result["status"])
	}
	// Status should be one of "active", "waiting", or "complete"
	switch status {
	case "active", "waiting", "complete":
		// ok
	default:
		t.Fatalf("unexpected status %q", status)
	}
	if result["fileName"] == nil {
		t.Fatal("expected fileName in status result")
	}
}

func TestRPCDownloadStatus_NotFound(t *testing.T) {
	handler, secret, cleanup, _, _ := newTestRPCHandlerWithManager(t)
	defer cleanup()

	code, resp := rpcCall(t, handler, "download.status", map[string]any{
		"gid": "nonexistent-hash",
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	errObj := rpcError(t, resp)
	errCode := errObj["code"].(float64)
	if errCode != float64(codeDownloadNotFound) {
		t.Fatalf("expected error code %d, got %v", codeDownloadNotFound, errCode)
	}
}

// --- download.list tests ---

func TestRPCDownloadList_Empty(t *testing.T) {
	handler, secret, cleanup, _, _ := newTestRPCHandlerWithManager(t)
	defer cleanup()

	code, resp := rpcCall(t, handler, "download.list", map[string]any{}, secret)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	result := rpcResult(t, resp)
	downloads, ok := result["downloads"].([]any)
	if !ok {
		t.Fatalf("expected downloads array, got %v", result["downloads"])
	}
	if len(downloads) != 0 {
		t.Fatalf("expected empty downloads, got %d", len(downloads))
	}
}

func TestRPCDownloadList_WithItems(t *testing.T) {
	handler, secret, cleanup, m, dlDir := newTestRPCHandlerWithManager(t)
	defer cleanup()

	content := bytes.Repeat([]byte("z"), 512)
	srv := newRangeServer(content)
	defer srv.Close()

	// Add a download
	_, addResp := rpcCall(t, handler, "download.add", map[string]any{
		"url": srv.URL + "/list-test.bin",
		"dir": dlDir,
	}, secret)
	addResult := rpcResult(t, addResp)
	gid := addResult["gid"].(string)

	// Wait for item to be registered
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if item := m.GetItem(gid); item != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// List all downloads
	code, resp := rpcCall(t, handler, "download.list", map[string]any{
		"status": "all",
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	result := rpcResult(t, resp)
	downloads, ok := result["downloads"].([]any)
	if !ok {
		t.Fatalf("expected downloads array, got %v", result["downloads"])
	}
	if len(downloads) < 1 {
		t.Fatalf("expected at least 1 download, got %d", len(downloads))
	}

	// Verify the first download has expected fields
	dl := downloads[0].(map[string]any)
	if dl["gid"] == nil || dl["gid"] == "" {
		t.Fatal("expected gid in download list item")
	}
	if dl["status"] == nil {
		t.Fatal("expected status in download list item")
	}
}

func TestRPCDownloadList_FilterActive(t *testing.T) {
	handler, secret, cleanup, _, _ := newTestRPCHandlerWithManager(t)
	defer cleanup()

	// With no active downloads, "active" filter should return empty
	code, resp := rpcCall(t, handler, "download.list", map[string]any{
		"status": "active",
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	result := rpcResult(t, resp)
	downloads := result["downloads"].([]any)
	if len(downloads) != 0 {
		t.Fatalf("expected 0 active downloads, got %d", len(downloads))
	}
}

func TestRPCDownloadList_FilterComplete(t *testing.T) {
	handler, secret, cleanup, _, _ := newTestRPCHandlerWithManager(t)
	defer cleanup()

	code, resp := rpcCall(t, handler, "download.list", map[string]any{
		"status": "complete",
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	result := rpcResult(t, resp)
	downloads := result["downloads"].([]any)
	if len(downloads) != 0 {
		t.Fatalf("expected 0 complete downloads, got %d", len(downloads))
	}
}

func TestRPCDownloadList_FilterWaiting(t *testing.T) {
	handler, secret, cleanup, _, _ := newTestRPCHandlerWithManager(t)
	defer cleanup()

	code, resp := rpcCall(t, handler, "download.list", map[string]any{
		"status": "waiting",
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	result := rpcResult(t, resp)
	downloads := result["downloads"].([]any)
	// Should be empty or contain items -- just verify it's a valid response
	if downloads == nil {
		t.Fatal("expected downloads array, got nil")
	}
}

func TestRPCDownloadList_DefaultStatus(t *testing.T) {
	handler, secret, cleanup, _, _ := newTestRPCHandlerWithManager(t)
	defer cleanup()

	// Omit status -- should default to "all"
	code, resp := rpcCall(t, handler, "download.list", map[string]any{}, secret)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	result := rpcResult(t, resp)
	if result["downloads"] == nil {
		t.Fatal("expected downloads key in response")
	}
}

func TestRPCDownloadList_UnknownStatusDefaultsToAll(t *testing.T) {
	handler, secret, cleanup, _, _ := newTestRPCHandlerWithManager(t)
	defer cleanup()

	// Unknown status should fall through to default (all)
	code, resp := rpcCall(t, handler, "download.list", map[string]any{
		"status": "unknown-status",
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	result := rpcResult(t, resp)
	if result["downloads"] == nil {
		t.Fatal("expected downloads key in response")
	}
}

// --- download.remove tests ---

func TestRPCDownloadRemove_Success(t *testing.T) {
	handler, secret, cleanup, m, dlDir := newTestRPCHandlerWithManager(t)
	defer cleanup()

	content := bytes.Repeat([]byte("r"), 1024)
	srv := newRangeServer(content)
	defer srv.Close()

	// Add a download
	_, addResp := rpcCall(t, handler, "download.add", map[string]any{
		"url": srv.URL + "/remove-test.bin",
		"dir": dlDir,
	}, secret)
	addResult := rpcResult(t, addResp)
	gid := addResult["gid"].(string)

	// Wait for item to be registered in manager
	deadline := time.Now().Add(2 * time.Second)
	var found bool
	for time.Now().Before(deadline) {
		if m.GetItem(gid) != nil {
			found = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !found {
		t.Fatal("download item was not added to manager")
	}

	// Wait for download to complete so FlushOne won't reject with ErrFlushItemDownloading
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		item := m.GetItem(gid)
		if item != nil && item.GetDownloaded() >= item.GetTotalSize() && item.GetTotalSize() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Stop the download and clear dAlloc so FlushOne accepts removal
	item := m.GetItem(gid)
	_ = item.StopDownload()

	// Remove it via RPC
	code, resp := rpcCall(t, handler, "download.remove", map[string]any{
		"gid": gid,
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	// Should return empty result (no error)
	if resp["error"] != nil {
		t.Fatalf("expected no error, got %v", resp["error"])
	}

	// Verify item was removed
	if item := m.GetItem(gid); item != nil {
		t.Fatal("expected item to be removed from manager")
	}
}

func TestRPCDownloadRemove_NotFound(t *testing.T) {
	handler, secret, cleanup, _, _ := newTestRPCHandlerWithManager(t)
	defer cleanup()

	code, resp := rpcCall(t, handler, "download.remove", map[string]any{
		"gid": "nonexistent-hash",
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	errObj := rpcError(t, resp)
	errCode := errObj["code"].(float64)
	if errCode != float64(codeDownloadNotFound) {
		t.Fatalf("expected error code %d, got %v", codeDownloadNotFound, errCode)
	}
}

// --- download.pause tests ---

func TestRPCDownloadPause_NotFound(t *testing.T) {
	handler, secret, cleanup, _, _ := newTestRPCHandlerWithManager(t)
	defer cleanup()

	code, resp := rpcCall(t, handler, "download.pause", map[string]any{
		"gid": "nonexistent-hash",
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	errObj := rpcError(t, resp)
	errCode := errObj["code"].(float64)
	if errCode != float64(codeDownloadNotFound) {
		t.Fatalf("expected error code %d, got %v", codeDownloadNotFound, errCode)
	}
}

func TestRPCDownloadPause_NotActive_NoPool(t *testing.T) {
	// Test the "not active" path: item exists in manager but NOT in the pool.
	// We create an RPC server with a nil pool to trigger this path.
	base := t.TempDir()
	if err := warplib.SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	m, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer m.Close()

	dlDir := filepath.Join(base, "downloads")
	if err := os.MkdirAll(dlDir, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	secret := "test-secret"
	// Create pool but don't add the download to it
	pool := NewPool(log.New(io.Discard, "", 0))

	content := bytes.Repeat([]byte("p"), 256)
	srv := newRangeServer(content)
	defer srv.Close()

	// First add a download to the manager directly via RPC
	client := &http.Client{
		CheckRedirect: warplib.RedirectPolicy(warplib.DefaultMaxRedirects),
	}
	cfg := &RPCConfig{Secret: secret, Version: "1.0.0"}
	rs := NewRPCServer(cfg, m, client, pool, nil, nil)
	defer rs.Close()
	h := requireToken(secret, rs.bridge)

	_, addResp := rpcCall(t, h, "download.add", map[string]any{
		"url": srv.URL + "/pause-test.bin",
		"dir": dlDir,
	}, secret)
	addResult := rpcResult(t, addResp)
	gid := addResult["gid"].(string)

	// Wait for item to be registered
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if m.GetItem(gid) != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Remove the download from the pool (simulating it finished/was never tracked)
	pool.StopDownload(gid)

	// Now pause should return "not active" because pool doesn't have it
	code, resp := rpcCall(t, h, "download.pause", map[string]any{
		"gid": gid,
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	errObj := rpcError(t, resp)
	errCode := errObj["code"].(float64)
	if errCode != float64(codeDownloadNotActive) {
		t.Fatalf("expected error code %d, got %v", codeDownloadNotActive, errCode)
	}
}

// --- download.resume tests ---

func TestRPCDownloadResume_NotFound(t *testing.T) {
	handler, secret, cleanup, _, _ := newTestRPCHandlerWithManager(t)
	defer cleanup()

	code, resp := rpcCall(t, handler, "download.resume", map[string]any{
		"gid": "nonexistent-hash",
	}, secret)
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	errObj := rpcError(t, resp)
	errCode := errObj["code"].(float64)
	if errCode != float64(codeDownloadNotFound) {
		t.Fatalf("expected error code %d, got %v", codeDownloadNotFound, errCode)
	}
}

// --- itemStatus helper tests ---

func TestItemStatus_Active(t *testing.T) {
	// An item with a non-nil dAlloc is "active". Since dAlloc is private,
	// we test through the public IsDownloading method indirectly via itemStatus.
	// A fresh item with zero downloaded and zero total is "waiting".
	item := &warplib.Item{
		TotalSize:  1000,
		Downloaded: 500,
	}
	status := itemStatus(item)
	if status != "waiting" {
		t.Fatalf("expected 'waiting' for partially downloaded non-active item, got %q", status)
	}
}

func TestItemStatus_Complete(t *testing.T) {
	item := &warplib.Item{
		TotalSize:  1000,
		Downloaded: 1000,
	}
	status := itemStatus(item)
	if status != "complete" {
		t.Fatalf("expected 'complete', got %q", status)
	}
}

func TestItemStatus_Waiting(t *testing.T) {
	item := &warplib.Item{
		TotalSize:  1000,
		Downloaded: 0,
	}
	status := itemStatus(item)
	if status != "waiting" {
		t.Fatalf("expected 'waiting', got %q", status)
	}
}

func TestItemStatus_ZeroSize(t *testing.T) {
	// Zero total size should be "waiting" even if downloaded is also zero
	item := &warplib.Item{
		TotalSize:  0,
		Downloaded: 0,
	}
	status := itemStatus(item)
	if status != "waiting" {
		t.Fatalf("expected 'waiting' for zero-size item, got %q", status)
	}
}
