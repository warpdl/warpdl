package warplib

import (
	"runtime"
	"strings"
)

// Constants for Windows long path support
const (
	LongPathThreshold = 240        // Threshold before applying prefix
	LongPathPrefix    = `\\?\`     // Extended-length path prefix
	UNCPrefix         = `\\`       // UNC path prefix
	UNCLongPathPrefix = `\\?\UNC\` // Extended UNC prefix
)

// IsLongPath returns true if path length exceeds threshold
func IsLongPath(path string) bool {
	return len(path) > LongPathThreshold
}

// NormalizePath applies Windows long path prefix if needed.
// On non-Windows platforms, it still normalizes slashes for Windows-style paths
// but does not add the \\?\ prefix.
func NormalizePath(path string) string {
	// Empty path: return unchanged
	if path == "" {
		return path
	}

	// Already prefixed: return unchanged
	if strings.HasPrefix(path, LongPathPrefix) {
		return path
	}

	// Unix-style paths: return unchanged
	if strings.HasPrefix(path, "/") {
		return path
	}

	// Normalize forward slashes to backslashes for Windows-style paths
	normalized := strings.ReplaceAll(path, "/", `\`)

	// Short paths: return normalized (slashes fixed but no prefix)
	if len(normalized) <= LongPathThreshold {
		return normalized
	}

	// Relative paths: return normalized (cannot prefix relative paths)
	if !isAbsolutePath(normalized) {
		return normalized
	}

	// Non-Windows platforms: return normalized but don't add prefix
	// This allows slash normalization for testing but respects platform limits
	if runtime.GOOS != "windows" {
		return normalized
	}

	// Windows platform with long absolute path: add appropriate prefix

	// UNC path (\\server\...): prefix with \\?\UNC\
	if strings.HasPrefix(normalized, UNCPrefix) && !strings.HasPrefix(normalized, LongPathPrefix) {
		// Convert \\server\share to \\?\UNC\server\share
		return UNCLongPathPrefix + normalized[2:]
	}

	// Regular absolute Windows path: prefix with \\?\
	return LongPathPrefix + normalized
}

// isAbsolutePath checks if a path is absolute (drive letter or UNC)
func isAbsolutePath(path string) bool {
	// Check for drive letter (e.g., C:\)
	if len(path) >= 3 && path[1] == ':' && path[2] == '\\' {
		return true
	}
	// Check for UNC path (\\server\share)
	if strings.HasPrefix(path, UNCPrefix) {
		return true
	}
	return false
}
