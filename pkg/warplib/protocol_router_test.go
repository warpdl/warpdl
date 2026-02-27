package warplib

import (
	"net/http"
	"strings"
	"testing"
)

// Test 13: NewProtocolDownloader("http://...") returns non-nil httpProtocolDownloader
func TestSchemeRouter_HTTP(t *testing.T) {
	router := NewSchemeRouter(http.DefaultClient)
	pd, err := router.NewDownloader("http://example.com/file.zip", &DownloaderOpts{
		SkipSetup: true,
	})
	if err != nil {
		t.Fatalf("NewDownloader(http): unexpected error: %v", err)
	}
	if pd == nil {
		t.Fatalf("NewDownloader(http): returned nil")
	}
}

// Test 14: NewProtocolDownloader("https://...") returns non-nil httpProtocolDownloader
func TestSchemeRouter_HTTPS(t *testing.T) {
	router := NewSchemeRouter(http.DefaultClient)
	pd, err := router.NewDownloader("https://example.com/file.zip", &DownloaderOpts{
		SkipSetup: true,
	})
	if err != nil {
		t.Fatalf("NewDownloader(https): unexpected error: %v", err)
	}
	if pd == nil {
		t.Fatalf("NewDownloader(https): returned nil")
	}
}

// Test 15: Unsupported scheme returns descriptive error with supported schemes listed
func TestSchemeRouter_UnsupportedScheme(t *testing.T) {
	router := NewSchemeRouter(http.DefaultClient)
	_, err := router.NewDownloader("magnet:?xt=urn:btih:abc123", &DownloaderOpts{
		SkipSetup: true,
	})
	if err == nil {
		t.Fatalf("NewDownloader(magnet): expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported scheme") {
		t.Errorf("error should contain 'unsupported scheme', got: %q", err.Error())
	}
	// Should list supported schemes
	if !strings.Contains(err.Error(), "http") {
		t.Errorf("error should list supported scheme 'http', got: %q", err.Error())
	}
}

// Test 16: Case-insensitive scheme (HTTP://)
func TestSchemeRouter_CaseInsensitive(t *testing.T) {
	router := NewSchemeRouter(http.DefaultClient)
	// HTTP:// uppercase should resolve to http factory
	pd, err := router.NewDownloader("HTTP://EXAMPLE.COM/file.zip", &DownloaderOpts{
		SkipSetup: true,
	})
	if err != nil {
		t.Fatalf("NewDownloader(HTTP uppercase): unexpected error: %v", err)
	}
	if pd == nil {
		t.Fatalf("NewDownloader(HTTP uppercase): returned nil")
	}
}

// Test 17: Empty URL returns error
func TestSchemeRouter_EmptyURL(t *testing.T) {
	router := NewSchemeRouter(http.DefaultClient)
	_, err := router.NewDownloader("", &DownloaderOpts{
		SkipSetup: true,
	})
	if err == nil {
		t.Fatalf("NewDownloader(''): expected error, got nil")
	}
}

// Test 18: Invalid URL with no scheme returns error
func TestSchemeRouter_NoScheme(t *testing.T) {
	router := NewSchemeRouter(http.DefaultClient)
	// A URL like "://noscheme" — net/url may parse with empty scheme
	_, err := router.NewDownloader("noscheme.com/file", &DownloaderOpts{
		SkipSetup: true,
	})
	if err == nil {
		t.Fatalf("NewDownloader(noscheme): expected error, got nil")
	}
}

// Test: SupportedSchemes returns sorted slice
func TestSchemeRouter_SupportedSchemes(t *testing.T) {
	router := NewSchemeRouter(http.DefaultClient)
	schemes := SupportedSchemes(router)
	if len(schemes) == 0 {
		t.Fatalf("SupportedSchemes: expected non-empty list")
	}
	found := map[string]bool{}
	for _, s := range schemes {
		found[s] = true
	}
	if !found["http"] {
		t.Errorf("SupportedSchemes: 'http' should be in list, got %v", schemes)
	}
	if !found["https"] {
		t.Errorf("SupportedSchemes: 'https' should be in list, got %v", schemes)
	}
}

// Test: Register additional scheme works
func TestSchemeRouter_Register(t *testing.T) {
	router := NewSchemeRouter(http.DefaultClient)
	called := false
	router.Register("ftp", func(rawURL string, opts *DownloaderOpts) (ProtocolDownloader, error) {
		called = true
		_ = called
		return &mockProtocolDownloader{}, nil
	})

	pd, err := router.NewDownloader("ftp://files.example.com/file.tar", &DownloaderOpts{
		SkipSetup: true,
	})
	if err != nil {
		t.Fatalf("NewDownloader(ftp after Register): unexpected error: %v", err)
	}
	if pd == nil {
		t.Fatalf("NewDownloader(ftp after Register): returned nil")
	}

	schemes := SupportedSchemes(router)
	found := false
	for _, s := range schemes {
		if s == "ftp" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("SupportedSchemes: 'ftp' should appear after Register, got %v", schemes)
	}
}
