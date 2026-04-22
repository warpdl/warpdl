package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// NewPKCEVerifier returns a fresh RFC-7636 compliant code_verifier:
// 43 characters of base64url-encoded random bytes, within the allowed
// unreserved character set.
func NewPKCEVerifier() (string, error) {
	// 32 raw bytes → 43 base64url chars (no padding).
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// PKCEChallenge derives the code_challenge from a verifier.
// method: "S256" (default) or "plain".
func PKCEChallenge(verifier, method string) string {
	switch method {
	case "plain":
		return verifier
	default: // "S256" or anything unrecognised
		sum := sha256.Sum256([]byte(verifier))
		return base64.RawURLEncoding.EncodeToString(sum[:])
	}
}
