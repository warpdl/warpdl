//go:build windows

// Package common provides shared types and constants used across the warpdl
// client-server communication layer.
package common

import (
	"os"
	"strings"
)

// DefaultPipeName is the default name for the Windows named pipe.
const DefaultPipeName = "warpdl"

// DefaultPipePath returns the full Windows named pipe path.
// Format: \\.\pipe\{name}
func DefaultPipePath() string {
	return `\\.\pipe\` + DefaultPipeName
}

// PipePath returns the Windows named pipe path for the daemon.
// It checks the WARPDL_PIPE_NAME environment variable first.
// If set and already contains the \\.\pipe\ prefix, it's used as-is.
// Otherwise, the prefix is prepended to the name.
// If not set, returns the default pipe path.
func PipePath() string {
	if name := os.Getenv(PipeNameEnv); name != "" {
		if strings.HasPrefix(name, `\\.\pipe\`) {
			return name
		}
		return `\\.\pipe\` + name
	}
	return DefaultPipePath()
}
