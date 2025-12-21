package warpcli

import "testing"

func TestSocketPathEnv(t *testing.T) {
	path := "/tmp/warpdl-client.sock"
	t.Setenv("WARPDL_SOCKET_PATH", path)
	if got := socketPath(); got != path {
		t.Fatalf("expected %s, got %s", path, got)
	}
}
