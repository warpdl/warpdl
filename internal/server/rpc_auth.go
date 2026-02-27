package server

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"
)

// requireToken wraps an http.Handler with Bearer token authentication.
// Returns JSON-RPC 2.0 error response on auth failure (not plain HTTP error).
// Uses subtle.ConstantTimeCompare to prevent timing attacks on the secret.
//
// If secret is empty, all requests are rejected -- RPC requires explicit opt-in.
func requireToken(secret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !validToken(secret, r.Header.Get("Authorization")) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"error": map[string]any{
					"code":    -32600,
					"message": "Unauthorized",
				},
				"id": nil,
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// validToken checks whether the provided Authorization header value matches the secret.
// Requires "Bearer " prefix. Uses constant-time comparison to prevent timing attacks.
// Returns false if secret is empty (RPC requires a secret to be set).
func validToken(secret, authHeader string) bool {
	if secret == "" {
		return false
	}
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return false
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")
	return subtle.ConstantTimeCompare([]byte(token), []byte(secret)) == 1
}
