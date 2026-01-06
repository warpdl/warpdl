package warpcli

import (
	"errors"
	"fmt"
	"net/url"
	"runtime"
	"strconv"
	"strings"

	"github.com/warpdl/warpdl/common"
)

// DaemonURI represents a parsed daemon connection URI.
type DaemonURI struct {
	Scheme  string // "unix", "tcp", or "pipe"
	Address string // Full address for dial
}

// Supported URI schemes
const (
	SchemeUnix = "unix"
	SchemeTCP  = "tcp"
	SchemePipe = "pipe"
)

// Errors
var (
	ErrEmptyURI          = errors.New("daemon URI cannot be empty")
	ErrUnsupportedScheme = errors.New("unsupported URI scheme")
	ErrInvalidPath       = errors.New("invalid path in URI")
	ErrPipeNotSupported  = errors.New("pipe:// scheme only supported on Windows")
	ErrUnixNotSupported  = errors.New("unix:// scheme not supported on Windows")
)

// ParseDaemonURI parses a daemon URI string into a DaemonURI struct.
func ParseDaemonURI(rawURI string) (*DaemonURI, error) {
	// Trim whitespace
	rawURI = strings.TrimSpace(rawURI)

	// Check for empty URI
	if rawURI == "" {
		return nil, ErrEmptyURI
	}

	// Parse the URI
	parsed, err := url.Parse(rawURI)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPath, err)
	}

	// Normalize scheme to lowercase
	scheme := strings.ToLower(parsed.Scheme)

	// Check for missing scheme
	if scheme == "" {
		return nil, ErrUnsupportedScheme
	}

	// Handle each scheme type
	switch scheme {
	case SchemeUnix:
		return parseUnixURI(parsed)
	case SchemeTCP:
		return parseTCPURI(parsed)
	case SchemePipe:
		return parsePipeURI(parsed)
	default:
		return nil, ErrUnsupportedScheme
	}
}

// parseUnixURI parses a Unix domain socket URI.
func parseUnixURI(parsed *url.URL) (*DaemonURI, error) {
	// Platform check
	if runtime.GOOS == "windows" {
		return nil, ErrUnixNotSupported
	}

	// Unix URIs should not have a host component
	// For unix:///path, Host is empty and Path is /path
	// For unix://relative/path, Host is "relative" and Path is /path (invalid)
	if parsed.Host != "" {
		return nil, ErrInvalidPath
	}

	// Extract path - for unix:///path, the path starts with /
	path := parsed.Path
	if path == "" {
		return nil, ErrInvalidPath
	}

	// Ensure absolute path
	if !strings.HasPrefix(path, "/") {
		return nil, ErrInvalidPath
	}

	return &DaemonURI{
		Scheme:  SchemeUnix,
		Address: path,
	}, nil
}

// parseTCPURI parses a TCP URI.
func parseTCPURI(parsed *url.URL) (*DaemonURI, error) {
	host := parsed.Host

	// Empty host is invalid
	if host == "" {
		return nil, ErrInvalidPath
	}

	// Check if port is present
	_, port, err := parseHostPort(host)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPath, err)
	}

	// If no port, append default
	var address string
	if port == "" {
		address = fmt.Sprintf("%s:%d", host, common.DefaultTCPPort)
	} else {
		// Validate port number
		portNum, err := strconv.Atoi(port)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid port", ErrInvalidPath)
		}
		if portNum < 1 || portNum > 65535 {
			return nil, fmt.Errorf("%w: port out of range", ErrInvalidPath)
		}
		address = host
	}

	return &DaemonURI{
		Scheme:  SchemeTCP,
		Address: address,
	}, nil
}

// parsePipeURI parses a Windows named pipe URI.
func parsePipeURI(parsed *url.URL) (*DaemonURI, error) {
	// Platform check
	if runtime.GOOS != "windows" {
		return nil, ErrPipeNotSupported
	}

	// Get the pipe name - could be in host or path
	pipeName := parsed.Host
	if pipeName == "" {
		return nil, ErrInvalidPath
	}

	// If the pipe name already has the Windows pipe prefix, use it as-is
	if strings.HasPrefix(pipeName, `\\.\pipe\`) {
		return &DaemonURI{
			Scheme:  SchemePipe,
			Address: pipeName,
		}, nil
	}

	// Otherwise, construct the full pipe path
	address := fmt.Sprintf(`\\.\pipe\%s`, pipeName)

	return &DaemonURI{
		Scheme:  SchemePipe,
		Address: address,
	}, nil
}

// parseHostPort splits a host:port string, handling IPv6 addresses with brackets.
// Returns host, port, error. Port may be empty if not present.
func parseHostPort(hostport string) (string, string, error) {
	// Check for IPv6 with brackets
	if strings.HasPrefix(hostport, "[") {
		// IPv6 address with brackets
		closeBracket := strings.Index(hostport, "]")
		if closeBracket == -1 {
			return "", "", errors.New("missing closing bracket in IPv6 address")
		}

		host := hostport[:closeBracket+1]
		remainder := hostport[closeBracket+1:]

		if remainder == "" {
			// No port
			return host, "", nil
		}

		if !strings.HasPrefix(remainder, ":") {
			return "", "", errors.New("invalid format after IPv6 address")
		}

		port := remainder[1:]
		return host, port, nil
	}

	// Regular host or IPv4
	colonCount := strings.Count(hostport, ":")

	if colonCount == 0 {
		// No port
		return hostport, "", nil
	}

	if colonCount == 1 {
		// Single colon - split normally
		parts := strings.Split(hostport, ":")
		return parts[0], parts[1], nil
	}

	// Multiple colons without brackets - bare IPv6 without port
	// This is technically valid for IPv6 addresses without a port
	return hostport, "", nil
}
