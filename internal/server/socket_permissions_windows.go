//go:build windows

package server

func setSocketPermissions(path string) {
	// No-op on Windows (uses TCP fallback)
}
