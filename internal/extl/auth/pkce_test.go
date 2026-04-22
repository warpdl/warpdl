package auth

import (
	"regexp"
	"strings"
	"testing"
)

func TestNewPKCEVerifierIsCompliant(t *testing.T) {
	for i := 0; i < 100; i++ {
		v, err := NewPKCEVerifier()
		if err != nil {
			t.Fatalf("NewPKCEVerifier: %v", err)
		}
		// RFC 7636: 43-128 chars, unreserved set.
		if len(v) < 43 || len(v) > 128 {
			t.Fatalf("verifier length out of bounds: %d", len(v))
		}
		if matched, _ := regexp.MatchString(`^[A-Za-z0-9\-._~]+$`, v); !matched {
			t.Fatalf("verifier has invalid chars: %q", v)
		}
	}
}

func TestChallengeFromVerifierS256(t *testing.T) {
	// Test vector from RFC 7636 section 4.4
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	expected := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	got := PKCEChallenge(verifier, "S256")
	if got != expected {
		t.Fatalf("S256 challenge mismatch:\n got:  %s\n want: %s", got, expected)
	}
}

func TestChallengePlain(t *testing.T) {
	v := "abc123"
	if PKCEChallenge(v, "plain") != v {
		t.Fatal("plain challenge must equal verifier")
	}
}

func TestChallengeUnknownMethodFallsBackToS256(t *testing.T) {
	v, _ := NewPKCEVerifier()
	c := PKCEChallenge(v, "unknown")
	if c == "" || c == v || strings.ContainsAny(c, "+/=") {
		t.Fatalf("expected base64url S256 challenge, got %q", c)
	}
}
