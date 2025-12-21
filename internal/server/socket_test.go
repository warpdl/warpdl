package server

import "testing"

func TestSocketPathEnv(t *testing.T) {
	path := "/tmp/warpdl-test.sock"
	t.Setenv("WARPDL_SOCKET_PATH", path)
	if got := socketPath(); got != path {
		t.Fatalf("expected %s, got %s", path, got)
	}
}

func TestSocketPathDefault(t *testing.T) {
	t.Setenv("WARPDL_SOCKET_PATH", "")
	got := socketPath()
	if got == "" {
		t.Fatalf("expected default socket path")
	}
}
