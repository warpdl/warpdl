package warplib

import (
	"bytes"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// newProxyServer creates a mock HTTP proxy server that records all requests
// passing through it. Returns the server and a channel that receives each
// proxied request's URL.
func newProxyServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var proxyHits atomic.Int32

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits.Add(1)

		// For CONNECT method (HTTPS proxy), just establish tunnel
		if r.Method == http.MethodConnect {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Forward the request to the actual destination
		client := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
		}

		// Build the request to forward
		targetURL := r.URL.String()
		if r.URL.Host == "" {
			targetURL = "http://" + r.Host + r.URL.Path
			if r.URL.RawQuery != "" {
				targetURL += "?" + r.URL.RawQuery
			}
		}

		proxyReq, err := http.NewRequest(r.Method, targetURL, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}

		// Copy headers
		for key, values := range r.Header {
			for _, value := range values {
				proxyReq.Header.Add(key, value)
			}
		}

		resp, err := client.Do(proxyReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		// Copy response headers
		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}))

	return proxy, &proxyHits
}

// newProxyServerWithAuth creates a proxy server that requires authentication.
func newProxyServerWithAuth(t *testing.T, username, password string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var proxyHits atomic.Int32

	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check Proxy-Authorization header
		auth := r.Header.Get("Proxy-Authorization")
		if auth == "" {
			w.Header().Set("Proxy-Authenticate", "Basic realm=\"proxy\"")
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}

		// Validate credentials (simplified - real impl would decode base64)
		expectedAuth := "Basic " + basicAuth(username, password)
		if auth != expectedAuth {
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}

		proxyHits.Add(1)

		// Forward request (simplified)
		client := &http.Client{}
		targetURL := r.URL.String()
		if r.URL.Host == "" {
			targetURL = "http://" + r.Host + r.URL.Path
		}

		proxyReq, _ := http.NewRequest(r.Method, targetURL, r.Body)
		for key, values := range r.Header {
			for _, value := range values {
				proxyReq.Header.Add(key, value)
			}
		}

		resp, err := client.Do(proxyReq)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}))

	return proxy, &proxyHits
}

// basicAuth returns the base64 encoding of username:password for Basic auth.
func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64Encode([]byte(auth))
}

// base64Encode is a simple base64 encoder.
func base64Encode(data []byte) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var result strings.Builder
	for i := 0; i < len(data); i += 3 {
		var n uint32
		remaining := len(data) - i
		if remaining >= 3 {
			n = uint32(data[i])<<16 | uint32(data[i+1])<<8 | uint32(data[i+2])
			result.WriteByte(alphabet[n>>18&63])
			result.WriteByte(alphabet[n>>12&63])
			result.WriteByte(alphabet[n>>6&63])
			result.WriteByte(alphabet[n&63])
		} else if remaining == 2 {
			n = uint32(data[i])<<16 | uint32(data[i+1])<<8
			result.WriteByte(alphabet[n>>18&63])
			result.WriteByte(alphabet[n>>12&63])
			result.WriteByte(alphabet[n>>6&63])
			result.WriteByte('=')
		} else {
			n = uint32(data[i]) << 16
			result.WriteByte(alphabet[n>>18&63])
			result.WriteByte(alphabet[n>>12&63])
			result.WriteString("==")
		}
	}
	return result.String()
}

// TestParseProxyURL tests proxy URL parsing and validation.
func TestParseProxyURL(t *testing.T) {
	tests := []struct {
		name       string
		proxyURL   string
		wantErr    bool
		wantHost   string
		wantUser   string
		wantScheme string
	}{
		{
			name:       "valid http proxy",
			proxyURL:   "http://proxy.example.com:8080",
			wantErr:    false,
			wantHost:   "proxy.example.com:8080",
			wantScheme: "http",
		},
		{
			name:       "valid https proxy",
			proxyURL:   "https://proxy.example.com:8080",
			wantErr:    false,
			wantHost:   "proxy.example.com:8080",
			wantScheme: "https",
		},
		{
			name:       "valid socks5 proxy",
			proxyURL:   "socks5://proxy.example.com:1080",
			wantErr:    false,
			wantHost:   "proxy.example.com:1080",
			wantScheme: "socks5",
		},
		{
			name:       "proxy with auth",
			proxyURL:   "http://user:pass@proxy.example.com:8080",
			wantErr:    false,
			wantHost:   "proxy.example.com:8080",
			wantUser:   "user",
			wantScheme: "http",
		},
		{
			name:       "proxy without port (should use default)",
			proxyURL:   "http://proxy.example.com",
			wantErr:    false,
			wantHost:   "proxy.example.com",
			wantScheme: "http",
		},
		{
			name:     "empty proxy URL",
			proxyURL: "",
			wantErr:  true,
		},
		{
			name:     "invalid URL",
			proxyURL: "://invalid",
			wantErr:  true,
		},
		{
			name:     "unsupported scheme",
			proxyURL: "ftp://proxy.example.com:8080",
			wantErr:  true,
		},
		{
			name:       "localhost proxy",
			proxyURL:   "http://localhost:8080",
			wantErr:    false,
			wantHost:   "localhost:8080",
			wantScheme: "http",
		},
		{
			name:       "IP address proxy",
			proxyURL:   "http://192.168.1.1:8080",
			wantErr:    false,
			wantHost:   "192.168.1.1:8080",
			wantScheme: "http",
		},
		{
			name:       "IPv6 proxy",
			proxyURL:   "http://[::1]:8080",
			wantErr:    false,
			wantHost:   "[::1]:8080",
			wantScheme: "http",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxyConfig, err := ParseProxyURL(tt.proxyURL)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseProxyURL(%q) expected error, got nil", tt.proxyURL)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseProxyURL(%q) unexpected error: %v", tt.proxyURL, err)
				return
			}

			if proxyConfig == nil {
				t.Errorf("ParseProxyURL(%q) returned nil config", tt.proxyURL)
				return
			}

			if proxyConfig.Host != tt.wantHost {
				t.Errorf("ParseProxyURL(%q) host = %q, want %q", tt.proxyURL, proxyConfig.Host, tt.wantHost)
			}

			if proxyConfig.Scheme != tt.wantScheme {
				t.Errorf("ParseProxyURL(%q) scheme = %q, want %q", tt.proxyURL, proxyConfig.Scheme, tt.wantScheme)
			}

			if tt.wantUser != "" && proxyConfig.Username != tt.wantUser {
				t.Errorf("ParseProxyURL(%q) username = %q, want %q", tt.proxyURL, proxyConfig.Username, tt.wantUser)
			}
		})
	}
}

// TestNewHTTPClientWithProxy tests creating an HTTP client configured with a proxy.
func TestNewHTTPClientWithProxy(t *testing.T) {
	tests := []struct {
		name     string
		proxyURL string
		wantErr  bool
	}{
		{
			name:     "valid http proxy",
			proxyURL: "http://proxy.example.com:8080",
			wantErr:  false,
		},
		{
			name:     "valid https proxy",
			proxyURL: "https://proxy.example.com:8080",
			wantErr:  false,
		},
		{
			name:     "valid socks5 proxy",
			proxyURL: "socks5://proxy.example.com:1080",
			wantErr:  false,
		},
		{
			name:     "empty proxy URL returns default client",
			proxyURL: "",
			wantErr:  false,
		},
		{
			name:     "invalid proxy URL",
			proxyURL: "://invalid",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewHTTPClientWithProxy(tt.proxyURL)

			if tt.wantErr {
				if err == nil {
					t.Errorf("NewHTTPClientWithProxy(%q) expected error, got nil", tt.proxyURL)
				}
				return
			}

			if err != nil {
				t.Errorf("NewHTTPClientWithProxy(%q) unexpected error: %v", tt.proxyURL, err)
				return
			}

			if client == nil {
				t.Errorf("NewHTTPClientWithProxy(%q) returned nil client", tt.proxyURL)
			}
		})
	}
}

// TestDownloadThroughProxy tests that downloads actually go through the configured proxy.
func TestDownloadThroughProxy(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Create a content server
	content := bytes.Repeat([]byte("proxy-test-content"), 1000)
	contentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer contentServer.Close()

	// Create a proxy server
	proxyServer, proxyHits := newProxyServer(t)
	defer proxyServer.Close()

	// Create HTTP client with proxy
	proxyURL := proxyServer.URL
	client, err := NewHTTPClientWithProxy(proxyURL)
	if err != nil {
		t.Fatalf("NewHTTPClientWithProxy: %v", err)
	}

	// Create downloader with proxy-configured client
	d, err := NewDownloader(client, contentServer.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
		MaxConnections:    1,
		MaxSegments:       1,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}

	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Verify the request went through the proxy
	if proxyHits.Load() == 0 {
		t.Error("download did not go through proxy")
	}
}

// TestDownloadThroughProxyWithAuth tests proxy authentication.
func TestDownloadThroughProxyWithAuth(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Create a content server
	content := []byte("authenticated-proxy-content")
	contentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer contentServer.Close()

	// Create an authenticated proxy server
	username := "testuser"
	password := "testpass"
	proxyServer, proxyHits := newProxyServerWithAuth(t, username, password)
	defer proxyServer.Close()

	// Parse proxy URL and add credentials
	proxyURLParsed, _ := url.Parse(proxyServer.URL)
	proxyURLParsed.User = url.UserPassword(username, password)
	proxyURL := proxyURLParsed.String()

	// Create HTTP client with authenticated proxy
	client, err := NewHTTPClientWithProxy(proxyURL)
	if err != nil {
		t.Fatalf("NewHTTPClientWithProxy: %v", err)
	}

	// Create downloader
	d, err := NewDownloader(client, contentServer.URL+"/auth-file.bin", &DownloaderOpts{
		DownloadDirectory: base,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}

	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Verify request went through authenticated proxy
	if proxyHits.Load() == 0 {
		t.Error("download did not go through authenticated proxy")
	}
}

// TestDownloadProxyAuthFailure tests that authentication failure is properly handled.
func TestDownloadProxyAuthFailure(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Create a content server
	content := []byte("should-not-download")
	contentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer contentServer.Close()

	// Create an authenticated proxy server
	proxyServer, proxyHits := newProxyServerWithAuth(t, "admin", "secret")
	defer proxyServer.Close()

	// Try with wrong credentials
	proxyURLParsed, _ := url.Parse(proxyServer.URL)
	proxyURLParsed.User = url.UserPassword("wrong", "credentials")
	proxyURL := proxyURLParsed.String()

	client, err := NewHTTPClientWithProxy(proxyURL)
	if err != nil {
		t.Fatalf("NewHTTPClientWithProxy: %v", err)
	}

	// Attempt download - should fail due to auth
	_, err = NewDownloader(client, contentServer.URL+"/fail.bin", &DownloaderOpts{
		DownloadDirectory: base,
	})

	// Should either return error or proxyHits should be 0
	if err == nil && proxyHits.Load() > 0 {
		t.Error("download succeeded with wrong proxy credentials")
	}
}

// TestDownloadProxyUnreachable tests error handling when proxy is unreachable.
func TestDownloadProxyUnreachable(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Create a content server
	contentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("content"))
	}))
	defer contentServer.Close()

	// Use a proxy URL that points to an unreachable address
	proxyURL := "http://127.0.0.1:59999" // Unlikely to be listening

	client, err := NewHTTPClientWithProxy(proxyURL)
	if err != nil {
		t.Fatalf("NewHTTPClientWithProxy: %v", err)
	}

	// Attempt download - should fail because proxy is unreachable
	_, err = NewDownloader(client, contentServer.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
	})

	if err == nil {
		t.Error("expected error when proxy is unreachable, got nil")
	}
}

// TestDownloadWithoutProxy tests that downloads work without a proxy configured.
func TestDownloadWithoutProxy(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	content := bytes.Repeat([]byte("no-proxy-content"), 100)
	contentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer contentServer.Close()

	// Create client without proxy (empty string)
	client, err := NewHTTPClientWithProxy("")
	if err != nil {
		t.Fatalf("NewHTTPClientWithProxy with empty string: %v", err)
	}

	d, err := NewDownloader(client, contentServer.URL+"/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}

	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

// TestProxyEnvironmentVariable tests that proxy can be configured via environment variable.
func TestProxyEnvironmentVariable(t *testing.T) {
	tests := []struct {
		name    string
		envVar  string
		envVal  string
		wantErr bool
	}{
		{
			name:    "HTTP_PROXY set",
			envVar:  "HTTP_PROXY",
			envVal:  "http://envproxy.example.com:8080",
			wantErr: false,
		},
		{
			name:    "http_proxy lowercase",
			envVar:  "http_proxy",
			envVal:  "http://envproxy.example.com:8080",
			wantErr: false,
		},
		{
			name:    "HTTPS_PROXY set",
			envVar:  "HTTPS_PROXY",
			envVal:  "http://envproxy.example.com:8080",
			wantErr: false,
		},
		{
			name:    "ALL_PROXY set",
			envVar:  "ALL_PROXY",
			envVal:  "socks5://envproxy.example.com:1080",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			t.Setenv(tt.envVar, tt.envVal)

			// NewHTTPClientFromEnvironment should pick up the env var
			client, err := NewHTTPClientFromEnvironment()

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if client == nil {
				t.Error("expected non-nil client")
			}
		})
	}
}

// TestProxyNoProxyBypass tests that NO_PROXY is respected.
func TestProxyNoProxyBypass(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Create a proxy server that records hits
	proxyServer, proxyHits := newProxyServer(t)
	defer proxyServer.Close()

	// Create a content server on localhost
	content := []byte("bypass-content")
	contentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusOK)
		w.Write(content)
	}))
	defer contentServer.Close()

	// Set proxy and NO_PROXY to bypass localhost
	t.Setenv("HTTP_PROXY", proxyServer.URL)
	t.Setenv("NO_PROXY", "localhost,127.0.0.1")

	client, err := NewHTTPClientFromEnvironment()
	if err != nil {
		t.Fatalf("NewHTTPClientFromEnvironment: %v", err)
	}

	d, err := NewDownloader(client, contentServer.URL+"/bypass.bin", &DownloaderOpts{
		DownloadDirectory: base,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}

	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Proxy should NOT have been hit due to NO_PROXY
	if proxyHits.Load() > 0 {
		t.Error("proxy was used despite NO_PROXY setting")
	}
}

// TestProxyConfig tests the ProxyConfig struct and its methods.
func TestProxyConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  ProxyConfig
		wantURL string
	}{
		{
			name: "simple http proxy",
			config: ProxyConfig{
				Scheme: "http",
				Host:   "proxy.example.com:8080",
			},
			wantURL: "http://proxy.example.com:8080",
		},
		{
			name: "https proxy with auth",
			config: ProxyConfig{
				Scheme:   "https",
				Host:     "proxy.example.com:8080",
				Username: "user",
				Password: "pass",
			},
			wantURL: "https://user:pass@proxy.example.com:8080",
		},
		{
			name: "socks5 proxy",
			config: ProxyConfig{
				Scheme: "socks5",
				Host:   "proxy.example.com:1080",
			},
			wantURL: "socks5://proxy.example.com:1080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.config.URL()
			if got != tt.wantURL {
				t.Errorf("ProxyConfig.URL() = %q, want %q", got, tt.wantURL)
			}
		})
	}
}

// TestDownloadMultiPartThroughProxy tests that multi-part downloads work through proxy.
func TestDownloadMultiPartThroughProxy(t *testing.T) {
	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Create a range-capable content server
	content := bytes.Repeat([]byte("multi-part-proxy-test"), 1000)
	contentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")

		if r.Header.Get("Range") == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			w.WriteHeader(http.StatusOK)
			w.Write(content)
			return
		}

		rangeHeader := strings.TrimPrefix(r.Header.Get("Range"), "bytes=")
		parts := strings.SplitN(rangeHeader, "-", 2)
		start, _ := strconv.Atoi(parts[0])
		end := len(content) - 1
		if parts[1] != "" {
			if e, err := strconv.Atoi(parts[1]); err == nil {
				end = e
			}
		}
		if start > end || start < 0 || end >= len(content) {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		chunk := content[start : end+1]
		w.Header().Set("Content-Length", strconv.Itoa(len(chunk)))
		w.Header().Set("Content-Range", "bytes "+strconv.Itoa(start)+"-"+strconv.Itoa(end)+"/"+strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusPartialContent)
		w.Write(chunk)
	}))
	defer contentServer.Close()

	// Create proxy server
	proxyServer, proxyHits := newProxyServer(t)
	defer proxyServer.Close()

	// Create HTTP client with proxy
	client, err := NewHTTPClientWithProxy(proxyServer.URL)
	if err != nil {
		t.Fatalf("NewHTTPClientWithProxy: %v", err)
	}

	// Create downloader with multiple parts
	d, err := NewDownloader(client, contentServer.URL+"/multipart.bin", &DownloaderOpts{
		DownloadDirectory: base,
		MaxConnections:    4,
		MaxSegments:       4,
	})
	if err != nil {
		t.Fatalf("NewDownloader: %v", err)
	}

	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Verify multiple requests went through proxy (one per part + initial request)
	if proxyHits.Load() < 2 {
		t.Errorf("expected multiple proxy hits for multi-part download, got %d", proxyHits.Load())
	}
}

// TestDownloaderOptsWithProxy tests that DownloaderOpts can include proxy configuration.
func TestDownloaderOptsWithProxy(t *testing.T) {
	opts := &DownloaderOpts{
		DownloadDirectory: "/tmp/test",
		MaxConnections:    4,
		ProxyURL:          "http://proxy.example.com:8080",
	}

	if opts.ProxyURL == "" {
		t.Error("ProxyURL field not accessible on DownloaderOpts")
	}

	if opts.ProxyURL != "http://proxy.example.com:8080" {
		t.Errorf("ProxyURL = %q, want %q", opts.ProxyURL, "http://proxy.example.com:8080")
	}
}

// TestInvalidProxyScheme tests that invalid proxy schemes are rejected.
func TestInvalidProxyScheme(t *testing.T) {
	invalidSchemes := []string{
		"ftp://proxy.example.com:8080",
		"ws://proxy.example.com:8080",
		"wss://proxy.example.com:8080",
		"file:///etc/passwd",
		"javascript:alert(1)",
	}

	for _, proxyURL := range invalidSchemes {
		t.Run(proxyURL, func(t *testing.T) {
			_, err := ParseProxyURL(proxyURL)
			if err == nil {
				t.Errorf("ParseProxyURL(%q) should have returned error for invalid scheme", proxyURL)
			}
		})
	}
}

// TestProxyWithSpecialCharactersInPassword tests proxy URLs with special chars in credentials.
func TestProxyWithSpecialCharactersInPassword(t *testing.T) {
	tests := []struct {
		name     string
		proxyURL string
		wantErr  bool
	}{
		{
			name:     "password with @",
			proxyURL: "http://user:p%40ssword@proxy.example.com:8080",
			wantErr:  false,
		},
		{
			name:     "password with :",
			proxyURL: "http://user:pass%3Aword@proxy.example.com:8080",
			wantErr:  false,
		},
		{
			name:     "password with spaces",
			proxyURL: "http://user:pass%20word@proxy.example.com:8080",
			wantErr:  false,
		},
		{
			name:     "unicode password",
			proxyURL: "http://user:%E2%9C%93@proxy.example.com:8080",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseProxyURL(tt.proxyURL)
			if tt.wantErr && err == nil {
				t.Errorf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestProxyConnectionTimeout tests that proxy connection respects timeout settings.
func TestProxyConnectionTimeout(t *testing.T) {
	// Create a listener that accepts connections but never responds
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	// Accept connections in background but don't respond
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// Hold connection open but never respond
			go func(c net.Conn) {
				buf := make([]byte, 1024)
				for {
					_, err := c.Read(buf)
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	proxyURL := "http://" + listener.Addr().String()

	// Create client with short timeout
	client, err := NewHTTPClientWithProxyAndTimeout(proxyURL, 100) // 100ms timeout
	if err != nil {
		t.Fatalf("NewHTTPClientWithProxyAndTimeout: %v", err)
	}

	base := t.TempDir()
	if err := SetConfigDir(base); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// This should timeout
	_, err = NewDownloader(client, "http://example.com/file.bin", &DownloaderOpts{
		DownloadDirectory: base,
	})

	if err == nil {
		t.Error("expected timeout error, got nil")
	}
}
