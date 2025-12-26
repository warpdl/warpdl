//go:build windows

package warpcli

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/warpdl/warpdl/common"
)

// dialPipeFunc is a variable that points to the actual dialPipe implementation.
// This allows tests to mock the pipe dialing behavior.
var dialPipeFunc = dialPipeImpl

// dialPipeImpl is the actual implementation of Windows named pipe dialing.
// If timeout is nil, the default timeout from common.DefaultDialTimeout is used.
func dialPipeImpl(path string, timeout *time.Duration) (net.Conn, error) {
	if timeout == nil {
		defaultTimeout := common.DefaultDialTimeout
		timeout = &defaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	return winio.DialPipeContext(ctx, path)
}

// dial establishes a connection to the daemon using Windows named pipe with TCP fallback.
// It first attempts to connect via named pipe. If that fails, it falls back to TCP.
// Transport priority: Named Pipe > TCP
func dial() (net.Conn, error) {
	pipePath := pipePath()
	debugLog("Attempting connection via named pipe at %s", pipePath)
	timeout := common.DefaultDialTimeout
	conn, pipeErr := dialPipeFunc(pipePath, &timeout)
	if pipeErr != nil {
		debugLog("Named pipe connection failed: %v, falling back to TCP", pipeErr)
		conn, err := dialFunc("tcp", tcpAddress())
		if err != nil {
			return nil, fmt.Errorf("failed to connect: named pipe error: %v; tcp error: %w", pipeErr, err)
		}
		debugLog("Successfully connected via TCP fallback to %s", tcpAddress())
		return conn, nil
	}
	debugLog("Successfully connected via named pipe")
	return conn, nil
}
