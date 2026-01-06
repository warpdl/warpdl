//go:build !windows

package warpcli

import (
	"fmt"
	"net"
)

// dialURI connects to a daemon using the parsed URI.
// This implementation handles unix and tcp schemes.
func dialURI(uri *DaemonURI) (net.Conn, error) {
	switch uri.Scheme {
	case SchemeUnix:
		debugLog("Connecting via Unix socket to %s", uri.Address)
		conn, err := dialFunc("unix", uri.Address)
		if err != nil {
			return nil, fmt.Errorf("unix socket connection failed: %w", err)
		}
		debugLog("Successfully connected via Unix socket")
		return conn, nil

	case SchemeTCP:
		debugLog("Connecting via TCP to %s", uri.Address)
		conn, err := dialFunc("tcp", uri.Address)
		if err != nil {
			return nil, fmt.Errorf("tcp connection failed: %w", err)
		}
		debugLog("Successfully connected via TCP")
		return conn, nil

	case SchemePipe:
		// This should never happen due to ParseDaemonURI validation on Unix
		return nil, ErrPipeNotSupported

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedScheme, uri.Scheme)
	}
}
