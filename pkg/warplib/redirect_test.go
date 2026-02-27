package warplib

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRedirectPolicy_MaxHops(t *testing.T) {
	t.Run("allows redirects within limit", func(t *testing.T) {
		policy := RedirectPolicy(10)
		// Simulate 9 prior hops (under limit)
		via := make([]*http.Request, 9)
		for i := range via {
			via[i] = &http.Request{URL: &url.URL{Scheme: "http", Host: "example.com", Path: fmt.Sprintf("/%d", i)}}
		}
		req := &http.Request{
			URL:    &url.URL{Scheme: "http", Host: "example.com", Path: "/final"},
			Header: make(http.Header),
		}
		err := policy(req, via)
		if err != nil {
			t.Errorf("expected no error for 9 hops, got: %v", err)
		}
	})

	t.Run("rejects redirects exceeding limit", func(t *testing.T) {
		policy := RedirectPolicy(10)
		// Simulate 10 prior hops (at limit)
		via := make([]*http.Request, 10)
		for i := range via {
			via[i] = &http.Request{URL: &url.URL{Scheme: "http", Host: "example.com", Path: fmt.Sprintf("/%d", i)}}
		}
		req := &http.Request{
			URL:    &url.URL{Scheme: "http", Host: "example.com", Path: "/overflow"},
			Header: make(http.Header),
		}
		err := policy(req, via)
		if err == nil {
			t.Fatal("expected error for 10 hops, got nil")
		}
		if !errors.Is(err, ErrTooManyRedirects) {
			t.Errorf("expected ErrTooManyRedirects, got: %v", err)
		}
		// Error should include the last URL
		if !strings.Contains(err.Error(), "/9") {
			t.Errorf("error should include last URL, got: %v", err)
		}
	})

	t.Run("boundary: exactly max hops succeeds", func(t *testing.T) {
		policy := RedirectPolicy(3)
		via := make([]*http.Request, 2)
		for i := range via {
			via[i] = &http.Request{URL: &url.URL{Scheme: "http", Host: "example.com", Path: fmt.Sprintf("/%d", i)}}
		}
		req := &http.Request{
			URL:    &url.URL{Scheme: "http", Host: "example.com", Path: "/final"},
			Header: make(http.Header),
		}
		err := policy(req, via)
		if err != nil {
			t.Errorf("expected no error for exactly limit-1 hops, got: %v", err)
		}
	})

	t.Run("boundary: max+1 hops fails", func(t *testing.T) {
		policy := RedirectPolicy(3)
		via := make([]*http.Request, 3)
		for i := range via {
			via[i] = &http.Request{URL: &url.URL{Scheme: "http", Host: "example.com", Path: fmt.Sprintf("/%d", i)}}
		}
		req := &http.Request{
			URL:    &url.URL{Scheme: "http", Host: "example.com", Path: "/overflow"},
			Header: make(http.Header),
		}
		err := policy(req, via)
		if err == nil {
			t.Fatal("expected error for max hops, got nil")
		}
		if !errors.Is(err, ErrTooManyRedirects) {
			t.Errorf("expected ErrTooManyRedirects, got: %v", err)
		}
	})
}

func TestRedirectPolicy_CrossProtocol(t *testing.T) {
	t.Run("rejects HTTP to FTP redirect", func(t *testing.T) {
		policy := RedirectPolicy(10)
		via := []*http.Request{
			{URL: &url.URL{Scheme: "http", Host: "example.com", Path: "/file"}},
		}
		req := &http.Request{
			URL:    &url.URL{Scheme: "ftp", Host: "ftp.example.com", Path: "/file"},
			Header: make(http.Header),
		}
		err := policy(req, via)
		if err == nil {
			t.Fatal("expected error for cross-protocol redirect, got nil")
		}
		if !errors.Is(err, ErrCrossProtocolRedirect) {
			t.Errorf("expected ErrCrossProtocolRedirect, got: %v", err)
		}
		if !strings.Contains(err.Error(), "http -> ftp") {
			t.Errorf("error should include protocol info, got: %v", err)
		}
	})

	t.Run("rejects HTTPS to FTP redirect", func(t *testing.T) {
		policy := RedirectPolicy(10)
		via := []*http.Request{
			{URL: &url.URL{Scheme: "https", Host: "example.com", Path: "/file"}},
		}
		req := &http.Request{
			URL:    &url.URL{Scheme: "ftp", Host: "ftp.example.com", Path: "/file"},
			Header: make(http.Header),
		}
		err := policy(req, via)
		if !errors.Is(err, ErrCrossProtocolRedirect) {
			t.Errorf("expected ErrCrossProtocolRedirect, got: %v", err)
		}
	})

	t.Run("allows HTTP to HTTPS redirect", func(t *testing.T) {
		policy := RedirectPolicy(10)
		via := []*http.Request{
			{URL: &url.URL{Scheme: "http", Host: "example.com", Path: "/file"}},
		}
		req := &http.Request{
			URL:    &url.URL{Scheme: "https", Host: "example.com", Path: "/file"},
			Header: make(http.Header),
		}
		err := policy(req, via)
		if err != nil {
			t.Errorf("HTTP to HTTPS should be allowed, got: %v", err)
		}
	})

	t.Run("allows HTTPS to HTTP redirect", func(t *testing.T) {
		policy := RedirectPolicy(10)
		via := []*http.Request{
			{URL: &url.URL{Scheme: "https", Host: "example.com", Path: "/file"}},
		}
		req := &http.Request{
			URL:    &url.URL{Scheme: "http", Host: "cdn.example.com", Path: "/file"},
			Header: make(http.Header),
		}
		err := policy(req, via)
		if err != nil {
			t.Errorf("HTTPS to HTTP should be allowed, got: %v", err)
		}
	})
}

func TestRedirectPolicy_NoVia(t *testing.T) {
	policy := RedirectPolicy(10)
	req := &http.Request{
		URL:    &url.URL{Scheme: "http", Host: "example.com", Path: "/file"},
		Header: make(http.Header),
	}
	err := policy(req, nil)
	if err != nil {
		t.Errorf("expected no error for first request (no via), got: %v", err)
	}
}

func TestIsCrossOrigin(t *testing.T) {
	tests := []struct {
		name     string
		hostA    string
		hostB    string
		expected bool
	}{
		{"same host", "example.com", "example.com", false},
		{"different host", "example.com", "cdn.example.com", true},
		{"same host different port", "example.com:80", "example.com:8080", true},
		{"same host same port", "example.com:443", "example.com:443", false},
		{"subdomain change", "api.example.com", "cdn.example.com", true},
		{"totally different domain", "example.com", "other.com", true},
		{"same host port vs no port", "example.com", "example.com:80", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &url.URL{Host: tt.hostA}
			b := &url.URL{Host: tt.hostB}
			result := isCrossOrigin(a, b)
			if result != tt.expected {
				t.Errorf("isCrossOrigin(%s, %s) = %v, want %v", tt.hostA, tt.hostB, result, tt.expected)
			}
		})
	}
}

func TestIsHTTPScheme(t *testing.T) {
	tests := []struct {
		scheme   string
		expected bool
	}{
		{"http", true},
		{"https", true},
		{"ftp", false},
		{"sftp", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.scheme, func(t *testing.T) {
			if got := isHTTPScheme(tt.scheme); got != tt.expected {
				t.Errorf("isHTTPScheme(%q) = %v, want %v", tt.scheme, got, tt.expected)
			}
		})
	}
}

func TestRedirectFollowing_Integration(t *testing.T) {
	t.Run("single redirect updates URL", func(t *testing.T) {
		// Final server serving actual content
		finalSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "5")
			w.Header().Set("Content-Disposition", `attachment; filename="test.bin"`)
			w.Header().Set("Accept-Ranges", "bytes")
			w.Write([]byte("hello"))
		}))
		defer finalSrv.Close()

		// Redirect server
		redirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, finalSrv.URL+"/file.bin", http.StatusFound)
		}))
		defer redirectSrv.Close()

		client := &http.Client{}
		d, err := NewDownloader(client, redirectSrv.URL+"/download", &DownloaderOpts{
			SkipSetup: true,
		})
		if err != nil {
			t.Fatalf("NewDownloader failed: %v", err)
		}

		// d.url should be updated to final URL
		if d.url != finalSrv.URL+"/file.bin" {
			t.Errorf("d.url = %q, want %q", d.url, finalSrv.URL+"/file.bin")
		}
	})

	t.Run("multi-hop redirect chain updates URL", func(t *testing.T) {
		// Final server
		finalSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "5")
			w.Header().Set("Content-Disposition", `attachment; filename="test.bin"`)
			w.Header().Set("Accept-Ranges", "bytes")
			w.Write([]byte("hello"))
		}))
		defer finalSrv.Close()

		// Intermediate redirect server
		midSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, finalSrv.URL+"/file.bin", http.StatusMovedPermanently)
		}))
		defer midSrv.Close()

		// First redirect server
		firstSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, midSrv.URL+"/redirect", http.StatusTemporaryRedirect)
		}))
		defer firstSrv.Close()

		client := &http.Client{}
		d, err := NewDownloader(client, firstSrv.URL+"/download", &DownloaderOpts{
			SkipSetup: true,
		})
		if err != nil {
			t.Fatalf("NewDownloader failed: %v", err)
		}

		if d.url != finalSrv.URL+"/file.bin" {
			t.Errorf("d.url = %q, want %q", d.url, finalSrv.URL+"/file.bin")
		}
	})

	t.Run("no redirect keeps original URL", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", "5")
			w.Header().Set("Content-Disposition", `attachment; filename="test.bin"`)
			w.Header().Set("Accept-Ranges", "bytes")
			w.Write([]byte("hello"))
		}))
		defer srv.Close()

		client := &http.Client{}
		d, err := NewDownloader(client, srv.URL+"/file.bin", &DownloaderOpts{
			SkipSetup: true,
		})
		if err != nil {
			t.Fatalf("NewDownloader failed: %v", err)
		}

		if d.url != srv.URL+"/file.bin" {
			t.Errorf("d.url = %q, want %q", d.url, srv.URL+"/file.bin")
		}
	})

	t.Run("redirect loop exceeding max hops returns error", func(t *testing.T) {
		// Server that always redirects to itself
		var loopSrv *httptest.Server
		loopSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, loopSrv.URL+"/loop", http.StatusFound)
		}))
		defer loopSrv.Close()

		client := &http.Client{}
		_, err := NewDownloader(client, loopSrv.URL+"/start", &DownloaderOpts{
			SkipSetup: true,
		})
		if err == nil {
			t.Fatal("expected error for redirect loop, got nil")
		}
		if !strings.Contains(err.Error(), "redirect loop detected") {
			t.Errorf("error should mention redirect loop, got: %v", err)
		}
	})

	t.Run("all redirect status codes work", func(t *testing.T) {
		codes := []int{
			http.StatusMovedPermanently,  // 301
			http.StatusFound,             // 302
			http.StatusSeeOther,          // 303
			http.StatusTemporaryRedirect, // 307
			http.StatusPermanentRedirect, // 308
		}

		for _, code := range codes {
			t.Run(fmt.Sprintf("status_%d", code), func(t *testing.T) {
				finalSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Length", "5")
					w.Header().Set("Content-Disposition", `attachment; filename="test.bin"`)
					w.Header().Set("Accept-Ranges", "bytes")
					w.Write([]byte("hello"))
				}))
				defer finalSrv.Close()

				redirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Redirect(w, r, finalSrv.URL+"/file.bin", code)
				}))
				defer redirectSrv.Close()

				client := &http.Client{}
				d, err := NewDownloader(client, redirectSrv.URL+"/download", &DownloaderOpts{
					SkipSetup: true,
				})
				if err != nil {
					t.Fatalf("status %d: NewDownloader failed: %v", code, err)
				}

				if d.url != finalSrv.URL+"/file.bin" {
					t.Errorf("status %d: d.url = %q, want %q", code, d.url, finalSrv.URL+"/file.bin")
				}
			})
		}
	})
}

func TestStripUnsafeHeaders(t *testing.T) {
	t.Run("strips custom headers", func(t *testing.T) {
		req := &http.Request{
			URL:    &url.URL{Scheme: "http", Host: "cdn.example.com"},
			Header: make(http.Header),
		}
		req.Header.Set("User-Agent", "WarpDL/1.0")
		req.Header.Set("Accept", "application/octet-stream")
		req.Header.Set("X-Custom-Token", "secret123")
		req.Header.Set("Authorization", "Bearer token")
		req.Header.Set("X-API-Key", "key456")

		stripUnsafeHeaders(req)

		// Safe headers preserved
		if req.Header.Get("User-Agent") != "WarpDL/1.0" {
			t.Error("User-Agent should be preserved")
		}
		if req.Header.Get("Accept") != "application/octet-stream" {
			t.Error("Accept should be preserved")
		}

		// Unsafe headers stripped
		if req.Header.Get("X-Custom-Token") != "" {
			t.Error("X-Custom-Token should be stripped")
		}
		if req.Header.Get("Authorization") != "" {
			t.Error("Authorization should be stripped")
		}
		if req.Header.Get("X-API-Key") != "" {
			t.Error("X-API-Key should be stripped")
		}
	})

	t.Run("preserves Range header", func(t *testing.T) {
		req := &http.Request{
			URL:    &url.URL{Scheme: "http", Host: "cdn.example.com"},
			Header: make(http.Header),
		}
		req.Header.Set("Range", "bytes=0-1023")
		req.Header.Set("X-Custom", "should-be-stripped")

		stripUnsafeHeaders(req)

		if req.Header.Get("Range") != "bytes=0-1023" {
			t.Error("Range header should be preserved for segment downloads")
		}
		if req.Header.Get("X-Custom") != "" {
			t.Error("X-Custom should be stripped")
		}
	})
}

func TestCVE2024_45336_AuthorizationHeaderLeak(t *testing.T) {
	// CVE-2024-45336 regression test: Authorization header must NOT be sent
	// to a different origin after redirect. Go 1.24+ handles this natively,
	// and our RedirectPolicy adds additional custom header stripping.
	t.Run("Authorization header not forwarded on cross-origin redirect", func(t *testing.T) {
		var capturedAuthHeader string

		// Final server (different origin) captures headers
		finalSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedAuthHeader = r.Header.Get("Authorization")
			w.Header().Set("Content-Length", "5")
			w.Header().Set("Content-Disposition", `attachment; filename="test.bin"`)
			w.Header().Set("Accept-Ranges", "bytes")
			w.Write([]byte("hello"))
		}))
		defer finalSrv.Close()

		// Redirect server (origin) redirects to different host
		redirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, finalSrv.URL+"/file.bin", http.StatusFound)
		}))
		defer redirectSrv.Close()

		client := &http.Client{}
		_, err := NewDownloader(client, redirectSrv.URL+"/download", &DownloaderOpts{
			SkipSetup: true,
			Headers: Headers{
				{Key: "Authorization", Value: "Bearer secret-token"},
			},
		})
		if err != nil {
			t.Fatalf("NewDownloader failed: %v", err)
		}

		// Authorization header should NOT have been forwarded to the different origin
		if capturedAuthHeader != "" {
			t.Errorf("Authorization header leaked to cross-origin server: got %q, want empty", capturedAuthHeader)
		}
	})

	t.Run("custom headers not forwarded on cross-origin redirect", func(t *testing.T) {
		var capturedCustomHeader string
		var capturedUserAgent string

		finalSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			capturedCustomHeader = r.Header.Get("X-Custom-Token")
			capturedUserAgent = r.Header.Get("User-Agent")
			w.Header().Set("Content-Length", "5")
			w.Header().Set("Content-Disposition", `attachment; filename="test.bin"`)
			w.Header().Set("Accept-Ranges", "bytes")
			w.Write([]byte("hello"))
		}))
		defer finalSrv.Close()

		redirectSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, finalSrv.URL+"/file.bin", http.StatusFound)
		}))
		defer redirectSrv.Close()

		client := &http.Client{}
		_, err := NewDownloader(client, redirectSrv.URL+"/download", &DownloaderOpts{
			SkipSetup: true,
			Headers: Headers{
				{Key: "X-Custom-Token", Value: "secret-api-key"},
			},
		})
		if err != nil {
			t.Fatalf("NewDownloader failed: %v", err)
		}

		if capturedCustomHeader != "" {
			t.Errorf("X-Custom-Token header leaked to cross-origin server: got %q, want empty", capturedCustomHeader)
		}
		// User-Agent should still be present (safe header)
		if capturedUserAgent == "" {
			t.Error("User-Agent should be preserved on cross-origin redirect")
		}
	})

	t.Run("headers preserved on same-origin redirect", func(t *testing.T) {
		var capturedCustomHeader string

		// Both handlers on the same server (same origin)
		mux := http.NewServeMux()
		mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/file.bin", http.StatusFound)
		})
		mux.HandleFunc("/file.bin", func(w http.ResponseWriter, r *http.Request) {
			capturedCustomHeader = r.Header.Get("X-Custom-Token")
			w.Header().Set("Content-Length", "5")
			w.Header().Set("Content-Disposition", `attachment; filename="test.bin"`)
			w.Header().Set("Accept-Ranges", "bytes")
			w.Write([]byte("hello"))
		})
		srv := httptest.NewServer(mux)
		defer srv.Close()

		client := &http.Client{}
		_, err := NewDownloader(client, srv.URL+"/redirect", &DownloaderOpts{
			SkipSetup: true,
			Headers: Headers{
				{Key: "X-Custom-Token", Value: "my-token"},
			},
		})
		if err != nil {
			t.Fatalf("NewDownloader failed: %v", err)
		}

		if capturedCustomHeader != "my-token" {
			t.Errorf("X-Custom-Token should be preserved on same-origin redirect, got %q", capturedCustomHeader)
		}
	})
}

func TestNewHTTPClientWithProxy_HasRedirectPolicy(t *testing.T) {
	client, err := NewHTTPClientWithProxy("")
	if err != nil {
		t.Fatalf("NewHTTPClientWithProxy failed: %v", err)
	}
	if client.CheckRedirect == nil {
		t.Error("NewHTTPClientWithProxy should set CheckRedirect policy")
	}
}

func TestNewHTTPClientFromEnvironment_HasRedirectPolicy(t *testing.T) {
	client, err := NewHTTPClientFromEnvironment()
	if err != nil {
		t.Fatalf("NewHTTPClientFromEnvironment failed: %v", err)
	}
	if client.CheckRedirect == nil {
		t.Error("NewHTTPClientFromEnvironment should set CheckRedirect policy")
	}
}

func TestNewHTTPClientWithProxyAndTimeout_HasRedirectPolicy(t *testing.T) {
	client, err := NewHTTPClientWithProxyAndTimeout("", 5000)
	if err != nil {
		t.Fatalf("NewHTTPClientWithProxyAndTimeout failed: %v", err)
	}
	if client.CheckRedirect == nil {
		t.Error("NewHTTPClientWithProxyAndTimeout should set CheckRedirect policy")
	}
}

func TestRedirectPolicy_CrossOriginHeaderStripping(t *testing.T) {
	t.Run("strips custom headers on cross-origin redirect", func(t *testing.T) {
		policy := RedirectPolicy(10)
		via := []*http.Request{
			{URL: &url.URL{Scheme: "http", Host: "origin.example.com", Path: "/file"}},
		}
		req := &http.Request{
			URL:    &url.URL{Scheme: "http", Host: "cdn.example.com", Path: "/file"},
			Header: make(http.Header),
		}
		req.Header.Set("User-Agent", "WarpDL/1.0")
		req.Header.Set("X-Custom-Token", "secret")

		err := policy(req, via)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if req.Header.Get("User-Agent") != "WarpDL/1.0" {
			t.Error("User-Agent should be preserved on cross-origin redirect")
		}
		if req.Header.Get("X-Custom-Token") != "" {
			t.Error("X-Custom-Token should be stripped on cross-origin redirect")
		}
	})

	t.Run("preserves all headers on same-origin redirect", func(t *testing.T) {
		policy := RedirectPolicy(10)
		via := []*http.Request{
			{URL: &url.URL{Scheme: "http", Host: "example.com", Path: "/old"}},
		}
		req := &http.Request{
			URL:    &url.URL{Scheme: "http", Host: "example.com", Path: "/new"},
			Header: make(http.Header),
		}
		req.Header.Set("User-Agent", "WarpDL/1.0")
		req.Header.Set("X-Custom-Token", "secret")
		req.Header.Set("Authorization", "Bearer token")

		err := policy(req, via)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if req.Header.Get("X-Custom-Token") != "secret" {
			t.Error("custom headers should be preserved on same-origin redirect")
		}
		if req.Header.Get("Authorization") != "Bearer token" {
			t.Error("Authorization should be preserved on same-origin redirect")
		}
	})
}
