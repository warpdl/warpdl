package cmd

import (
	"testing"

	"github.com/warpdl/warpdl/pkg/warplib"
)

func TestParseCookieFlags_SingleCookie(t *testing.T) {
	flags := []string{"session=abc123"}

	header, err := ParseCookieFlags(flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if header.Key != "Cookie" {
		t.Errorf("expected key 'Cookie', got %q", header.Key)
	}
	if header.Value != "session=abc123" {
		t.Errorf("expected value 'session=abc123', got %q", header.Value)
	}
}

func TestParseCookieFlags_MultipleCookies(t *testing.T) {
	flags := []string{"session=abc", "user=xyz", "token=123"}

	header, err := ParseCookieFlags(flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if header.Key != "Cookie" {
		t.Errorf("expected key 'Cookie', got %q", header.Key)
	}

	// Multiple cookies should be joined with "; "
	expected := "session=abc; user=xyz; token=123"
	if header.Value != expected {
		t.Errorf("expected value %q, got %q", expected, header.Value)
	}
}

func TestParseCookieFlags_Empty(t *testing.T) {
	flags := []string{}

	header, err := ParseCookieFlags(flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Empty flags should return empty header (zero value)
	if header.Key != "" || header.Value != "" {
		t.Errorf("expected empty header for empty flags, got %+v", header)
	}
}

func TestParseCookieFlags_InvalidFormat(t *testing.T) {
	tests := []struct {
		name   string
		flags  []string
		errMsg string
	}{
		{
			name:   "missing equals sign",
			flags:  []string{"invalid-cookie"},
			errMsg: "invalid cookie format",
		},
		{
			name:   "one valid one invalid",
			flags:  []string{"valid=cookie", "invalid"},
			errMsg: "invalid cookie format",
		},
		{
			name:   "empty string in flags",
			flags:  []string{""},
			errMsg: "invalid cookie format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseCookieFlags(tt.flags)
			if err == nil {
				t.Error("expected error for invalid format")
				return
			}
			if !containsStr(err.Error(), tt.errMsg) {
				t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
			}
		})
	}
}

func TestParseCookieFlags_SpecialCharacters(t *testing.T) {
	// Cookies with URL-encoded characters should be preserved as-is
	flags := []string{"token=abc%3D123%3Btest"}

	header, err := ParseCookieFlags(flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if header.Value != "token=abc%3D123%3Btest" {
		t.Errorf("special characters not preserved: got %q", header.Value)
	}
}

func TestParseCookieFlags_ValueWithEquals(t *testing.T) {
	// Cookie values can contain = characters
	flags := []string{"token=abc=123=xyz"}

	header, err := ParseCookieFlags(flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if header.Value != "token=abc=123=xyz" {
		t.Errorf("value with equals not handled: got %q", header.Value)
	}
}

func TestParseCookieFlags_WhitespaceHandling(t *testing.T) {
	// Whitespace should be trimmed from cookie strings
	flags := []string{"  session=abc  ", " token=xyz "}

	header, err := ParseCookieFlags(flags)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "session=abc; token=xyz"
	if header.Value != expected {
		t.Errorf("whitespace not trimmed: got %q, want %q", header.Value, expected)
	}
}

func TestAppendCookieHeader(t *testing.T) {
	tests := []struct {
		name     string
		headers  warplib.Headers
		cookies  []string
		expected int // expected length after append
	}{
		{
			name:     "append to empty headers",
			headers:  warplib.Headers{},
			cookies:  []string{"session=abc"},
			expected: 1,
		},
		{
			name:     "append to existing headers",
			headers:  warplib.Headers{{Key: "User-Agent", Value: "WarpDL/1.0"}},
			cookies:  []string{"session=abc"},
			expected: 2,
		},
		{
			name:     "empty cookies no change",
			headers:  warplib.Headers{{Key: "User-Agent", Value: "WarpDL/1.0"}},
			cookies:  []string{},
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := AppendCookieHeader(tt.headers, tt.cookies)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result) != tt.expected {
				t.Errorf("expected %d headers, got %d", tt.expected, len(result))
			}
		})
	}
}

// Helper function
func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
