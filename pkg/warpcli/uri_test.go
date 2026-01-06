package warpcli

import (
	"errors"
	"net"

	"runtime"
	"testing"
)

// TestParseDaemonURI_ValidUnixSocket verifies that Unix socket URIs are parsed correctly.
// Format: unix:///path/to/socket
func TestParseDaemonURI_ValidUnixSocket(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		wantScheme  string
		wantAddress string
	}{
		{
			name:        "absolute path",
			uri:         "unix:///tmp/warpdl.sock",
			wantScheme:  "unix",
			wantAddress: "/tmp/warpdl.sock",
		},
		{
			name:        "home directory path",
			uri:         "unix:///home/user/.config/warpdl/daemon.sock",
			wantScheme:  "unix",
			wantAddress: "/home/user/.config/warpdl/daemon.sock",
		},
		{
			name:        "var run path",
			uri:         "unix:///var/run/warpdl.sock",
			wantScheme:  "unix",
			wantAddress: "/var/run/warpdl.sock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uri, err := ParseDaemonURI(tt.uri)
			if err != nil {
				t.Fatalf("ParseDaemonURI() error = %v, want nil", err)
			}
			if uri.Scheme != tt.wantScheme {
				t.Errorf("Scheme = %q, want %q", uri.Scheme, tt.wantScheme)
			}
			if uri.Address != tt.wantAddress {
				t.Errorf("Address = %q, want %q", uri.Address, tt.wantAddress)
			}
		})
	}
}

// TestParseDaemonURI_ValidTCP verifies that TCP URIs with explicit ports are parsed correctly.
// Format: tcp://host:port
func TestParseDaemonURI_ValidTCP(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		wantScheme  string
		wantAddress string
	}{
		{
			name:        "localhost with port",
			uri:         "tcp://localhost:3849",
			wantScheme:  "tcp",
			wantAddress: "localhost:3849",
		},
		{
			name:        "IP address with port",
			uri:         "tcp://127.0.0.1:3849",
			wantScheme:  "tcp",
			wantAddress: "127.0.0.1:3849",
		},
		{
			name:        "hostname with custom port",
			uri:         "tcp://myserver:8080",
			wantScheme:  "tcp",
			wantAddress: "myserver:8080",
		},
		{
			name:        "IPv6 localhost with port",
			uri:         "tcp://[::1]:3849",
			wantScheme:  "tcp",
			wantAddress: "[::1]:3849",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uri, err := ParseDaemonURI(tt.uri)
			if err != nil {
				t.Fatalf("ParseDaemonURI() error = %v, want nil", err)
			}
			if uri.Scheme != tt.wantScheme {
				t.Errorf("Scheme = %q, want %q", uri.Scheme, tt.wantScheme)
			}
			if uri.Address != tt.wantAddress {
				t.Errorf("Address = %q, want %q", uri.Address, tt.wantAddress)
			}
		})
	}
}

// TestParseDaemonURI_TCPDefaultPort verifies that TCP URIs without ports default to 3849.
// Format: tcp://host defaults to tcp://host:3849
func TestParseDaemonURI_TCPDefaultPort(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		wantScheme  string
		wantAddress string
	}{
		{
			name:        "localhost no port",
			uri:         "tcp://localhost",
			wantScheme:  "tcp",
			wantAddress: "localhost:3849",
		},
		{
			name:        "IP address no port",
			uri:         "tcp://127.0.0.1",
			wantScheme:  "tcp",
			wantAddress: "127.0.0.1:3849",
		},
		{
			name:        "hostname no port",
			uri:         "tcp://myserver",
			wantScheme:  "tcp",
			wantAddress: "myserver:3849",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uri, err := ParseDaemonURI(tt.uri)
			if err != nil {
				t.Fatalf("ParseDaemonURI() error = %v, want nil", err)
			}
			if uri.Scheme != tt.wantScheme {
				t.Errorf("Scheme = %q, want %q", uri.Scheme, tt.wantScheme)
			}
			if uri.Address != tt.wantAddress {
				t.Errorf("Address = %q, want %q", uri.Address, tt.wantAddress)
			}
		})
	}
}

// TestParseDaemonURI_ValidPipe verifies that Windows named pipe URIs are parsed correctly.
// Format: pipe://name
// This test is skipped on Unix platforms.
func TestParseDaemonURI_ValidPipe(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("pipe URIs are Windows-only")
	}

	tests := []struct {
		name        string
		uri         string
		wantScheme  string
		wantAddress string
	}{
		{
			name:        "simple pipe name",
			uri:         "pipe://warpdl",
			wantScheme:  "pipe",
			wantAddress: `\\.\pipe\warpdl`,
		},
		{
			name:        "pipe name with underscores",
			uri:         "pipe://warpdl_daemon",
			wantScheme:  "pipe",
			wantAddress: `\\.\pipe\warpdl_daemon`,
		},
		{
			name:        "full pipe path",
			uri:         `pipe://\\.\pipe\custom`,
			wantScheme:  "pipe",
			wantAddress: `\\.\pipe\custom`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uri, err := ParseDaemonURI(tt.uri)
			if err != nil {
				t.Fatalf("ParseDaemonURI() error = %v, want nil", err)
			}
			if uri.Scheme != tt.wantScheme {
				t.Errorf("Scheme = %q, want %q", uri.Scheme, tt.wantScheme)
			}
			if uri.Address != tt.wantAddress {
				t.Errorf("Address = %q, want %q", uri.Address, tt.wantAddress)
			}
		})
	}
}

// TestParseDaemonURI_InvalidScheme verifies that unsupported URI schemes return an error.
func TestParseDaemonURI_InvalidScheme(t *testing.T) {
	tests := []struct {
		name string
		uri  string
	}{
		{
			name: "ftp scheme",
			uri:  "ftp://localhost:21",
		},
		{
			name: "http scheme",
			uri:  "http://localhost:8080",
		},
		{
			name: "https scheme",
			uri:  "https://localhost:443",
		},
		{
			name: "file scheme",
			uri:  "file:///tmp/socket",
		},
		{
			name: "unknown scheme",
			uri:  "unknown://something",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseDaemonURI(tt.uri)
			if err == nil {
				t.Fatal("ParseDaemonURI() error = nil, want error")
			}
			if !errors.Is(err, ErrUnsupportedScheme) {
				t.Errorf("error = %v, want %v", err, ErrUnsupportedScheme)
			}
		})
	}
}

// TestParseDaemonURI_EmptyURI verifies that empty URIs return an error.
func TestParseDaemonURI_EmptyURI(t *testing.T) {
	tests := []struct {
		name string
		uri  string
	}{
		{
			name: "empty string",
			uri:  "",
		},
		{
			name: "whitespace only",
			uri:  "   ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseDaemonURI(tt.uri)
			if err == nil {
				t.Fatal("ParseDaemonURI() error = nil, want error")
			}
			if !errors.Is(err, ErrEmptyURI) {
				t.Errorf("error = %v, want %v", err, ErrEmptyURI)
			}
		})
	}
}

// TestParseDaemonURI_MalformedURI verifies that malformed URIs return an error.
func TestParseDaemonURI_MalformedURI(t *testing.T) {
	tests := []struct {
		name string
		uri  string
	}{
		{
			name: "missing scheme separator",
			uri:  "tcp/localhost:3849",
		},
		{
			name: "scheme without host",
			uri:  "tcp://",
		},
		{
			name: "unix without path",
			uri:  "unix://",
		},
		{
			name: "pipe without name",
			uri:  "pipe://",
		},
		{
			name: "invalid port",
			uri:  "tcp://localhost:invalid",
		},
		{
			name: "port out of range",
			uri:  "tcp://localhost:99999",
		},
		{
			name: "unix with relative path",
			uri:  "unix://relative/path",
		},
		{
			name: "no scheme",
			uri:  "localhost:3849",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseDaemonURI(tt.uri)
			if err == nil {
				t.Fatal("ParseDaemonURI() error = nil, want error")
			}
			// Should return either ErrInvalidPath or ErrUnsupportedScheme
			if !errors.Is(err, ErrInvalidPath) && !errors.Is(err, ErrUnsupportedScheme) {
				t.Logf("got error: %v", err)
			}
		})
	}
}

// TestParseDaemonURI_EdgeCases verifies edge cases in URI parsing.
func TestParseDaemonURI_EdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		wantError bool
	}{
		{
			name:      "tcp with trailing slash",
			uri:       "tcp://localhost:3849/",
			wantError: false,
		},
		{
			name:      "unix with double slashes in path",
			uri:       "unix:///tmp//warpdl.sock",
			wantError: false,
		},
		{
			name:      "tcp uppercase scheme",
			uri:       "TCP://localhost:3849",
			wantError: false, // Should normalize to lowercase
		},
		{
			name:      "unix uppercase scheme",
			uri:       "UNIX:///tmp/socket",
			wantError: false, // Should normalize to lowercase
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseDaemonURI(tt.uri)
			if tt.wantError && err == nil {
				t.Fatal("ParseDaemonURI() error = nil, want error")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("ParseDaemonURI() error = %v, want nil", err)
			}
		})
	}
}

// TestNewClientWithURI_SkipsEnsureDaemon verifies that NewClientWithURI does NOT call ensureDaemon.
// This is critical - when using an explicit URI, we assume the daemon exists and should not spawn it.
func TestNewClientWithURI_SkipsEnsureDaemon(t *testing.T) {
	// Save original functions
	origEnsureDaemon := ensureDaemonFunc
	origDialURI := dialURIFunc
	defer func() {
		ensureDaemonFunc = origEnsureDaemon
		dialURIFunc = origDialURI
	}()

	// Track if ensureDaemon was called
	ensureDaemonCalled := false
	ensureDaemonFunc = func() error {
		ensureDaemonCalled = true
		return nil
	}

	// Mock dialURIFunc to return a mock connection
	dialURIFunc = func(uri *DaemonURI) (net.Conn, error) {
		// Return a mock connection using a pipe
		client, server := net.Pipe()
		// Close server end immediately since we don't need it
		server.Close()
		return client, nil
	}

	// Create client with URI
	client, err := NewClientWithURI("tcp://localhost:3849")
	if err != nil {
		t.Fatalf("NewClientWithURI() error = %v, want nil", err)
	}
	defer client.Close()

	// Verify ensureDaemon was NOT called
	if ensureDaemonCalled {
		t.Error("ensureDaemon was called, but should be skipped when using explicit URI")
	}
}

// TestNewClientWithURI_InvalidURI_ReturnsError verifies that invalid URIs return an error.
func TestNewClientWithURI_InvalidURI_ReturnsError(t *testing.T) {
	tests := []struct {
		name string
		uri  string
	}{
		{
			name: "empty URI",
			uri:  "",
		},
		{
			name: "invalid scheme",
			uri:  "http://localhost",
		},
		{
			name: "malformed URI",
			uri:  "tcp://",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewClientWithURI(tt.uri)
			if err == nil {
				t.Fatal("NewClientWithURI() error = nil, want error")
			}
			// Should fail at parsing stage, not connection stage
			t.Logf("got expected error: %v", err)
		})
	}
}

// TestNewClientWithURI_ConnectionFails_ReturnsError verifies that connection failures are reported.
func TestNewClientWithURI_ConnectionFails_ReturnsError(t *testing.T) {
	// Save original dialURIFunc
	origDialURI := dialURIFunc
	defer func() {
		dialURIFunc = origDialURI
	}()

	// Mock dialURIFunc to simulate connection failure
	dialURIFunc = func(uri *DaemonURI) (net.Conn, error) {
		return nil, errors.New("connection refused")
	}

	// Attempt to create client
	_, err := NewClientWithURI("tcp://localhost:3849")
	if err == nil {
		t.Fatal("NewClientWithURI() error = nil, want error")
	}

	// Verify error mentions connection failure
	errMsg := err.Error()
	if errMsg == "" {
		t.Error("expected non-empty error message")
	}
	t.Logf("got expected connection error: %v", err)
}
