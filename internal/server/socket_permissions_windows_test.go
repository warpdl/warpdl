//go:build windows

package server

import "testing"

func TestSetSocketPermissions_NoOp(t *testing.T) {
	// Should not panic or error
	setSocketPermissions("C:\\nonexistent\\path.sock")
}
