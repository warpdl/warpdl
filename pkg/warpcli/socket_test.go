package warpcli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSocketPathEnv(t *testing.T) {
	path := "/tmp/warpdl-client.sock"
	t.Setenv("WARPDL_SOCKET_PATH", path)
	if got := socketPath(); got != path {
		t.Fatalf("expected %s, got %s", path, got)
	}
}

func TestSocketPathDefault(t *testing.T) {
	// Ensure env is NOT set
	os.Unsetenv("WARPDL_SOCKET_PATH")
	got := socketPath()
	expected := filepath.Join(os.TempDir(), "warpdl.sock")
	if got != expected {
		t.Fatalf("expected %s, got %s", expected, got)
	}
}
