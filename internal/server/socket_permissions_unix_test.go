//go:build !windows

package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetSocketPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	// Create a test file
	f, err := os.Create(sockPath)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	f.Close()

	setSocketPermissions(sockPath)

	info, err := os.Stat(sockPath)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	if info.Mode().Perm() != 0700 {
		t.Fatalf("expected permissions 0700, got %o", info.Mode().Perm())
	}
}
