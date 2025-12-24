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

func TestTcpPort_Default(t *testing.T) {
	os.Unsetenv("WARPDL_TCP_PORT")
	got := tcpPort()
	expected := 3849
	if got != expected {
		t.Fatalf("expected %d, got %d", expected, got)
	}
}

func TestTcpPort_EnvOverride(t *testing.T) {
	t.Setenv("WARPDL_TCP_PORT", "4000")
	got := tcpPort()
	expected := 4000
	if got != expected {
		t.Fatalf("expected %d, got %d", expected, got)
	}
}

func TestTcpPort_InvalidEnv(t *testing.T) {
	t.Setenv("WARPDL_TCP_PORT", "not-a-number")
	got := tcpPort()
	expected := 3849 // Should fallback to default
	if got != expected {
		t.Fatalf("expected %d (default), got %d", expected, got)
	}
}

func TestForceTCP_Default(t *testing.T) {
	os.Unsetenv("WARPDL_FORCE_TCP")
	got := forceTCP()
	if got != false {
		t.Fatalf("expected false, got %v", got)
	}
}

func TestForceTCP_Enabled(t *testing.T) {
	t.Setenv("WARPDL_FORCE_TCP", "1")
	got := forceTCP()
	if got != true {
		t.Fatalf("expected true, got %v", got)
	}
}

func TestDebugMode_Default(t *testing.T) {
	os.Unsetenv("WARPDL_DEBUG")
	got := debugMode()
	if got != false {
		t.Fatalf("expected false, got %v", got)
	}
}

func TestDebugMode_Enabled(t *testing.T) {
	t.Setenv("WARPDL_DEBUG", "1")
	got := debugMode()
	if got != true {
		t.Fatalf("expected true, got %v", got)
	}
}

func TestTcpAddress(t *testing.T) {
	os.Unsetenv("WARPDL_TCP_PORT")
	got := tcpAddress()
	expected := "localhost:3849"
	if got != expected {
		t.Fatalf("expected %s, got %s", expected, got)
	}
}
