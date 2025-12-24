// Package common provides shared types and constants used across the warpdl
// client-server communication layer.
package common

// Environment variable names for configuration.
const (
	// SocketPathEnv is the environment variable for custom socket path.
	SocketPathEnv = "WARPDL_SOCKET_PATH"

	// TCPPortEnv is the environment variable for custom TCP port.
	TCPPortEnv = "WARPDL_TCP_PORT"

	// ForceTCPEnv is the environment variable to force TCP connections.
	ForceTCPEnv = "WARPDL_FORCE_TCP"

	// DebugEnv is the environment variable to enable debug logging.
	DebugEnv = "WARPDL_DEBUG"
)
