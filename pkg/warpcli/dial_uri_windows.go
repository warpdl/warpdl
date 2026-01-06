//go:build windows

package warpcli

import (
	"fmt"
	"net"

	"github.com/warpdl/warpdl/common"
)

// dialURI connects to a daemon using the parsed URI.
// This implementation handles pipe and tcp schemes.
func dialURI(uri *DaemonURI) (net.Conn, error) {
	switch uri.Scheme {
	case SchemePipe:
		debugLog("Connecting via named pipe to %s", uri.Address)
		timeout := common.DefaultDialTimeout
		conn, err := dialPipeFunc(uri.Address, &timeout)
		if err != nil {
			return nil, fmt.Errorf("named pipe connection failed: %w", err)
		}
		debugLog("Successfully connected via named pipe")
		return conn, nil

	case SchemeTCP:
		debugLog("Connecting via TCP to %s", uri.Address)
		conn, err := dialFunc("tcp", uri.Address)
		if err != nil {
			return nil, fmt.Errorf("tcp connection failed: %w", err)
		}
		debugLog("Successfully connected via TCP")
		return conn, nil

	case SchemeUnix:
		// This should never happen due to ParseDaemonURI validation on Windows
		return nil, ErrUnixNotSupported

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedScheme, uri.Scheme)
	}
}
