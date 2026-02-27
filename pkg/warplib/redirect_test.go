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
