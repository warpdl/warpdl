package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
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
	rs := NewRPCServer(cfg)
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
	rs := NewRPCServer(cfg)
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
