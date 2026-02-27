package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// dummyHandler is a simple handler that returns 200 OK for testing the auth middleware.
var dummyHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
})

func TestRequireToken_ValidToken(t *testing.T) {
	secret := "test-secret-12345"
	handler := requireToken(secret, dummyHandler)

	req := httptest.NewRequest(http.MethodPost, "/jsonrpc", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if rr.Body.String() != "ok" {
		t.Fatalf("expected 'ok' body, got %q", rr.Body.String())
	}
}

func TestRequireToken_MissingToken(t *testing.T) {
	secret := "test-secret-12345"
	handler := requireToken(secret, dummyHandler)

	req := httptest.NewRequest(http.MethodPost, "/jsonrpc", nil)
	// No Authorization header
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if resp["jsonrpc"] != "2.0" {
		t.Fatalf("expected jsonrpc 2.0, got %v", resp["jsonrpc"])
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %v", resp["error"])
	}
	if errObj["code"].(float64) != -32600 {
		t.Fatalf("expected error code -32600, got %v", errObj["code"])
	}
	if errObj["message"] != "Unauthorized" {
		t.Fatalf("expected 'Unauthorized', got %v", errObj["message"])
	}
}

func TestRequireToken_WrongToken(t *testing.T) {
	secret := "test-secret-12345"
	handler := requireToken(secret, dummyHandler)

	req := httptest.NewRequest(http.MethodPost, "/jsonrpc", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestRequireToken_EmptySecret(t *testing.T) {
	// When rpcSecret is empty, requireToken should reject ALL requests.
	// This ensures RPC cannot accidentally run without auth.
	handler := requireToken("", dummyHandler)

	req := httptest.NewRequest(http.MethodPost, "/jsonrpc", nil)
	req.Header.Set("Authorization", "Bearer anything")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 when secret is empty, got %d", rr.Code)
	}
}

func TestRequireToken_BearerPrefix(t *testing.T) {
	secret := "my-secret"
	handler := requireToken(secret, dummyHandler)

	// Without "Bearer " prefix -- should fail because the raw header value
	// doesn't match the secret after prefix stripping
	req := httptest.NewRequest(http.MethodPost, "/jsonrpc", nil)
	req.Header.Set("Authorization", secret) // No "Bearer " prefix
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without Bearer prefix, got %d", rr.Code)
	}

	// With "Bearer " prefix -- should succeed
	req2 := httptest.NewRequest(http.MethodPost, "/jsonrpc", nil)
	req2.Header.Set("Authorization", "Bearer "+secret)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("expected 200 with Bearer prefix, got %d", rr2.Code)
	}
}

func TestValidToken_ConstantTimeComparison(t *testing.T) {
	// Verify the function correctly accepts matching tokens
	// and rejects non-matching tokens. The subtle.ConstantTimeCompare
	// call is verified by the implementation.
	if !validToken("secret", "Bearer secret") {
		t.Fatal("expected matching tokens to return true")
	}
	if validToken("secret", "Bearer wrong") {
		t.Fatal("expected non-matching tokens to return false")
	}
	if validToken("secret", "") {
		t.Fatal("expected empty auth header to return false")
	}
	if validToken("secret", "secret") {
		t.Fatal("expected missing Bearer prefix to return false")
	}
	if validToken("", "Bearer anything") {
		t.Fatal("expected empty secret to return false")
	}
	if validToken("", "") {
		t.Fatal("expected both empty to return false")
	}
}
