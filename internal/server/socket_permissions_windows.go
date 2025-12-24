//go:build windows

package server

// setSocketPermissions is a no-op on Windows.
// Windows uses TCP sockets instead of Unix sockets, and permissions
// are managed through Windows ACLs rather than Unix file permissions.
func setSocketPermissions(path string) error {
	return nil
}
