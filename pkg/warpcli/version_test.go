package warpcli

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"sync"
	"testing"

	"github.com/warpdl/warpdl/common"
)

// mockVersionServer creates a mock server that responds to version requests
func mockVersionServer(_ *testing.T, response *common.VersionResponse, shouldError bool) (net.Conn, net.Conn) {
	c1, c2 := net.Pipe()
	go func() {
		reqBytes, err := read(c2)
		if err != nil {
			return
		}
		var req Request
		_ = json.Unmarshal(reqBytes, &req)

		var respBytes []byte
		if shouldError {
			respBytes, _ = json.Marshal(Response{Ok: false, Error: "daemon error"})
		} else {
			respMsg, _ := json.Marshal(response)
			respBytes, _ = json.Marshal(Response{Ok: true, Update: &Update{Type: req.Method, Message: respMsg}})
		}
		_ = write(c2, respBytes)
	}()
	return c1, c2
}

func TestCheckVersionMismatch_EmptyVersion(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	client := &Client{
		conn: c1,
		mu:   &sync.RWMutex{},
		d:    &Dispatcher{Handlers: make(map[common.UpdateType][]Handler)},
	}

	// Should return immediately without making any calls
	// Capture stderr to verify no output
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	client.CheckVersionMismatch("")

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	io.Copy(&buf, r)
	if buf.Len() > 0 {
		t.Fatalf("expected no stderr output, got: %s", buf.String())
	}
}

func TestCheckVersionMismatch_SuppressedByEnvVar(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	client := &Client{
		conn: c1,
		mu:   &sync.RWMutex{},
		d:    &Dispatcher{Handlers: make(map[common.UpdateType][]Handler)},
	}

	// Set suppression env var
	t.Setenv(VersionCheckEnv, "1")

	// Capture stderr to verify no output
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	client.CheckVersionMismatch("1.0.0")

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	io.Copy(&buf, r)
	if buf.Len() > 0 {
		t.Fatalf("expected no stderr output when suppressed, got: %s", buf.String())
	}
}

func TestCheckVersionMismatch_DaemonError(t *testing.T) {
	c1, c2 := mockVersionServer(t, nil, true)
	defer c1.Close()
	defer c2.Close()

	client := &Client{
		conn: c1,
		mu:   &sync.RWMutex{},
		d:    &Dispatcher{Handlers: make(map[common.UpdateType][]Handler)},
	}

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	client.CheckVersionMismatch("1.0.0")

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("Warning: could not verify daemon version")) {
		t.Fatalf("expected warning about version check error, got: %s", output)
	}
}

func TestCheckVersionMismatch_VersionMismatch(t *testing.T) {
	versionResp := &common.VersionResponse{Version: "2.0.0", Commit: "abc123"}
	c1, c2 := mockVersionServer(t, versionResp, false)
	defer c1.Close()
	defer c2.Close()

	client := &Client{
		conn: c1,
		mu:   &sync.RWMutex{},
		d:    &Dispatcher{Handlers: make(map[common.UpdateType][]Handler)},
	}

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	client.CheckVersionMismatch("1.0.0")

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()
	if !bytes.Contains([]byte(output), []byte("Warning: CLI version (1.0.0) differs from daemon version (2.0.0)")) {
		t.Fatalf("expected mismatch warning, got: %s", output)
	}
	if !bytes.Contains([]byte(output), []byte("stop-daemon")) {
		t.Fatalf("expected restart instruction, got: %s", output)
	}
}

func TestCheckVersionMismatch_VersionMatch(t *testing.T) {
	versionResp := &common.VersionResponse{Version: "1.0.0", Commit: "abc123"}
	c1, c2 := mockVersionServer(t, versionResp, false)
	defer c1.Close()
	defer c2.Close()

	client := &Client{
		conn: c1,
		mu:   &sync.RWMutex{},
		d:    &Dispatcher{Handlers: make(map[common.UpdateType][]Handler)},
	}

	// Capture stderr - should be silent when versions match
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	client.CheckVersionMismatch("1.0.0")

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	io.Copy(&buf, r)
	if buf.Len() > 0 {
		t.Fatalf("expected no output when versions match, got: %s", buf.String())
	}
}

func TestGetDaemonVersion(t *testing.T) {
	versionResp := &common.VersionResponse{
		Version:   "1.2.3",
		Commit:    "abc123",
		BuildType: "release",
	}
	c1, c2 := mockVersionServer(t, versionResp, false)
	defer c1.Close()
	defer c2.Close()

	client := &Client{
		conn: c1,
		mu:   &sync.RWMutex{},
		d:    &Dispatcher{Handlers: make(map[common.UpdateType][]Handler)},
	}

	resp, err := client.GetDaemonVersion()
	if err != nil {
		t.Fatalf("GetDaemonVersion failed: %v", err)
	}
	if resp.Version != "1.2.3" {
		t.Errorf("expected version 1.2.3, got %s", resp.Version)
	}
	if resp.Commit != "abc123" {
		t.Errorf("expected commit abc123, got %s", resp.Commit)
	}
	if resp.BuildType != "release" {
		t.Errorf("expected build_type release, got %s", resp.BuildType)
	}
}

func TestGetDaemonVersion_Error(t *testing.T) {
	c1, c2 := mockVersionServer(t, nil, true)
	defer c1.Close()
	defer c2.Close()

	client := &Client{
		conn: c1,
		mu:   &sync.RWMutex{},
		d:    &Dispatcher{Handlers: make(map[common.UpdateType][]Handler)},
	}

	_, err := client.GetDaemonVersion()
	if err == nil {
		t.Fatal("expected error from GetDaemonVersion")
	}
}
