//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	// testFileSize is the default size for locally-served test files (1MB).
	testFileSize = 1 * 1024 * 1024

	// daemonStartupWait gives the daemon time to bind its socket.
	daemonStartupWait = 2 * time.Second

	// commandTimeout is the default timeout for CLI commands.
	commandTimeout = 30 * time.Second
)

// ---------------------------------------------------------------------------
// testEnv — per-test isolated environment
// ---------------------------------------------------------------------------

// testEnv encapsulates an isolated WarpDL test environment with its own
// configDir, downloadDir, socket path, and environment variables.
type testEnv struct {
	ConfigDir   string
	DownloadDir string
	SocketPath  string
	Env         []string
	BinaryPath  string

	daemonCmd    *exec.Cmd
	daemonCancel context.CancelFunc
	mu           sync.Mutex
}

// newTestEnv creates isolated temp directories and environment variables for
// a single test. It uses binaryPath from TestMain (cli_download_test.go).
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	configDir := t.TempDir()
	downloadDir := t.TempDir()
	socketPath := filepath.Join(configDir, "warpdl.sock")

	env := append(os.Environ(),
		"WARPDL_CONFIG_DIR="+configDir,
		"WARPDL_SOCKET_PATH="+socketPath,
	)

	return &testEnv{
		ConfigDir:   configDir,
		DownloadDir: downloadDir,
		SocketPath:  socketPath,
		Env:         env,
		BinaryPath:  binaryPath,
	}
}

// startDaemon launches the daemon process in the background with optional
// extra flags (e.g., "--max-concurrent", "1"). It registers cleanup to
// stop the daemon when the test finishes.
func (e *testEnv) startDaemon(t *testing.T, extraFlags ...string) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	e.daemonCancel = cancel

	args := append([]string{"daemon"}, extraFlags...)
	cmd := exec.CommandContext(ctx, e.BinaryPath, args...)
	cmd.Env = e.Env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("failed to start daemon: %v", err)
	}

	e.mu.Lock()
	e.daemonCmd = cmd
	e.mu.Unlock()

	t.Cleanup(func() { e.stopDaemon(t) })

	// Give daemon time to bind its socket.
	time.Sleep(daemonStartupWait)
}

// stopDaemon gracefully stops the daemon, then force-kills on timeout.
func (e *testEnv) stopDaemon(t *testing.T) {
	t.Helper()

	e.mu.Lock()
	cmd := e.daemonCmd
	cancel := e.daemonCancel
	e.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return
	}

	// Try graceful stop via stop-daemon command.
	stopCmd := exec.Command(e.BinaryPath, "stop-daemon")
	stopCmd.Env = e.Env
	_ = stopCmd.Run()

	if cancel != nil {
		cancel()
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
	}
}

// run executes a warpdl CLI command and returns combined stdout+stderr.
// Fails the test on non-zero exit.
func (e *testEnv) run(t *testing.T, args ...string) string {
	t.Helper()
	output, err := e.runRaw(args...)
	if err != nil {
		t.Fatalf("command %v failed: %v\nOutput: %s", args, err, output)
	}
	return output
}

// runWithStdin executes a warpdl CLI command with the given stdin content and
// returns combined stdout+stderr. Fails the test on non-zero exit.
func (e *testEnv) runWithStdin(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(e.BinaryPath, args...)
	cmd.Env = e.Env
	cmd.Stdin = strings.NewReader(stdin)

	output, err := runCmdWithTimeout(cmd, commandTimeout)
	if err != nil {
		t.Fatalf("command %v failed: %v\nOutput: %s", args, err, output)
	}
	return output
}

// runExpectError executes a warpdl CLI command and expects it to fail.
// Returns the combined output. Fails the test if the command succeeds.
func (e *testEnv) runExpectError(t *testing.T, args ...string) string {
	t.Helper()
	output, err := e.runRaw(args...)
	if err == nil {
		t.Fatalf("expected command %v to fail, but it succeeded.\nOutput: %s", args, output)
	}
	return output
}

// runMayFail executes a warpdl CLI command and returns combined output and
// error. Does not fail the test on non-zero exit.
func (e *testEnv) runMayFail(args ...string) (string, error) {
	return e.runRaw(args...)
}

// runRaw is the internal helper that runs a command with timeout.
func (e *testEnv) runRaw(args ...string) (string, error) {
	cmd := exec.Command(e.BinaryPath, args...)
	cmd.Env = e.Env
	return runCmdWithTimeout(cmd, commandTimeout)
}

// runCmdWithTimeout runs cmd.CombinedOutput() with a timeout.
func runCmdWithTimeout(cmd *exec.Cmd, timeout time.Duration) (string, error) {
	done := make(chan struct{}, 1)
	var output []byte
	var err error

	go func() {
		output, err = cmd.CombinedOutput()
		close(done)
	}()

	select {
	case <-done:
		return string(output), err
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		return "", fmt.Errorf("timeout after %v", timeout)
	}
}

// ---------------------------------------------------------------------------
// Local HTTP test server with range request support
// ---------------------------------------------------------------------------

// testServer wraps a local HTTP server that serves files with configurable
// sizes, supports HTTP range requests for segmented downloading, and can
// simulate error conditions (404, slow responses, mid-download failure).
type testServer struct {
	*http.Server
	URL string

	mu          sync.Mutex
	files       map[string]*testFile
	slowLatency time.Duration // per-chunk latency for /slow/* paths
}

// testFile represents a virtual file served by testServer.
type testFile struct {
	Size        int64
	ContentType string
}

// newTestServer starts a local HTTP test server on a random port.
// It serves files from a virtual filesystem and supports:
//   - Range requests (for segmented downloads)
//   - /slow/* paths with configurable latency
//   - /error/* paths that return HTTP errors
//   - /hang path that never responds (until context cancel)
func newTestServer(t *testing.T) *testServer {
	t.Helper()

	ts := &testServer{
		files: map[string]*testFile{
			"/testfile.bin": {Size: testFileSize, ContentType: "application/octet-stream"},
			"/small.bin":    {Size: 1024, ContentType: "application/octet-stream"},
			"/medium.bin":   {Size: 10 * 1024 * 1024, ContentType: "application/octet-stream"},
		},
		slowLatency: 100 * time.Millisecond,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", ts.handleRequest)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	ts.URL = fmt.Sprintf("http://127.0.0.1:%d", listener.Addr().(*net.TCPAddr).Port)
	ts.Server = &http.Server{Handler: mux}

	go func() {
		if err := ts.Server.Serve(listener); err != nil && err != http.ErrServerClosed {
			// Server shutting down is expected during cleanup.
		}
	}()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = ts.Server.Shutdown(ctx)
	})

	return ts
}

// addFile registers a virtual file at the given path with the given size.
func (ts *testServer) addFile(path string, size int64) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.files[path] = &testFile{Size: size, ContentType: "application/octet-stream"}
}

// setSlowLatency sets the per-chunk latency for /slow/* paths.
func (ts *testServer) setSlowLatency(d time.Duration) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.slowLatency = d
}

func (ts *testServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// /error/<code> paths return specific HTTP error codes.
	if strings.HasPrefix(path, "/error/") {
		code := 500
		if _, err := fmt.Sscanf(path, "/error/%d", &code); err != nil {
			code = 500
		}
		http.Error(w, fmt.Sprintf("simulated error %d", code), code)
		return
	}

	// /hang never responds (blocks until request context is cancelled).
	if path == "/hang" {
		<-r.Context().Done()
		return
	}

	// /slow/* paths serve files with artificial latency.
	slow := false
	lookupPath := path
	if strings.HasPrefix(path, "/slow") {
		slow = true
		lookupPath = strings.TrimPrefix(path, "/slow")
	}

	ts.mu.Lock()
	f, ok := ts.files[lookupPath]
	if !ok {
		ts.mu.Unlock()
		http.NotFound(w, r)
		return
	}
	fileSize := f.Size
	contentType := f.ContentType
	latency := ts.slowLatency
	ts.mu.Unlock()

	// Handle range requests.
	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" {
		var start, end int64
		n, err := fmt.Sscanf(rangeHeader, "bytes=%d-%d", &start, &end)
		if err != nil || n < 1 {
			http.Error(w, "invalid range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if n == 1 {
			end = fileSize - 1
		}
		if start >= fileSize || end >= fileSize || start > end {
			http.Error(w, "range not satisfiable", http.StatusRequestedRangeNotSatisfiable)
			return
		}

		length := end - start + 1
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, fileSize))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", length))
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusPartialContent)

		ts.writeBytes(w, r, length, slow, latency)
		return
	}

	// Full file response.
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileSize))
	w.Header().Set("Accept-Ranges", "bytes")

	if r.Method == http.MethodHead {
		return
	}

	ts.writeBytes(w, r, fileSize, slow, latency)
}

// writeBytes writes `length` zero bytes to w, optionally with per-chunk latency.
func (ts *testServer) writeBytes(w http.ResponseWriter, r *http.Request, length int64, slow bool, latency time.Duration) {
	const chunkSize = 32 * 1024 // 32KB chunks
	buf := make([]byte, chunkSize)

	remaining := length
	for remaining > 0 {
		n := int64(chunkSize)
		if n > remaining {
			n = remaining
		}

		if slow && latency > 0 {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(latency):
			}
		}

		written, err := w.Write(buf[:n])
		if err != nil {
			return
		}
		remaining -= int64(written)

		// Flush if possible to ensure data is sent.
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

// fileURL returns the full URL for a file path on this test server.
func (ts *testServer) fileURL(path string) string {
	return ts.URL + path
}

// slowFileURL returns the /slow/<path> URL for throttled serving.
func (ts *testServer) slowFileURL(path string) string {
	return ts.URL + "/slow" + path
}

// errorURL returns a URL that responds with the given HTTP status code.
func (ts *testServer) errorURL(code int) string {
	return fmt.Sprintf("%s/error/%d", ts.URL, code)
}

// ---------------------------------------------------------------------------
// Assertion helpers
// ---------------------------------------------------------------------------

// assertFileExists checks that a file exists at the given path.
func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("expected file to exist at %s", path)
	}
}

// assertFileSize checks that a file at the given path has the expected size.
func assertFileSize(t *testing.T, path string, expectedSize int64) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat file %s: %v", path, err)
	}
	if info.Size() != expectedSize {
		t.Fatalf("file size mismatch at %s: want %d, got %d", path, expectedSize, info.Size())
	}
}

// assertOutputContains checks that output contains the expected substring.
func assertOutputContains(t *testing.T, output, expected string) {
	t.Helper()
	if !strings.Contains(output, expected) {
		t.Errorf("expected output to contain %q, got:\n%s", expected, output)
	}
}

// assertOutputNotContains checks that output does NOT contain the substring.
func assertOutputNotContains(t *testing.T, output, unexpected string) {
	t.Helper()
	if strings.Contains(output, unexpected) {
		t.Errorf("expected output to NOT contain %q, got:\n%s", unexpected, output)
	}
}

// ---------------------------------------------------------------------------
// Download helper
// ---------------------------------------------------------------------------

// downloadAndVerify downloads a file from the given URL using the test
// environment's daemon, verifies the file exists with the expected size,
// and returns the output from the download command.
func (e *testEnv) downloadAndVerify(t *testing.T, rawURL string, expectedSize int64, extraFlags ...string) string {
	t.Helper()

	args := []string{"download", rawURL, "-d", "-l", e.DownloadDir, "-x", "4", "-s", "4"}
	args = append(args, extraFlags...)

	cmd := exec.Command(e.BinaryPath, args...)
	cmd.Env = e.Env

	output, err := runCmdWithTimeout(cmd, downloadTimeout)
	if err != nil {
		if isNetworkError(err, output) {
			t.Skipf("Network unavailable: %v\nOutput: %s", err, output)
		}
		t.Fatalf("download failed: %v\nOutput: %s", err, output)
	}

	// Determine filename from URL and verify the downloaded file.
	fileName := filenameFromURL(rawURL)
	if fileName != "" {
		filePath := filepath.Join(e.DownloadDir, fileName)
		assertFileExists(t, filePath)
		assertFileSize(t, filePath, expectedSize)
	}

	return output
}

// filenameFromURL extracts the filename from the last path segment of a URL.
func filenameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return path.Base(u.Path)
}

// ---------------------------------------------------------------------------
// Helpers for extracting hashes from list output
// ---------------------------------------------------------------------------

// extractHashFromListOutput parses the table-formatted list output and
// returns the first hash found. Returns empty string if none found.
func extractHashFromListOutput(output string) string {
	// The list format is: | N | Name | Hash | Status | Scheduled |
	// Hash appears between pipes after the name column.
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 5 {
			continue
		}
		hash := strings.TrimSpace(parts[3])
		// Hashes are short alphanumeric strings, skip headers.
		if hash != "" && hash != "Unique Hash" && !strings.Contains(hash, "-") {
			return hash
		}
	}
	return ""
}

// createInputFile creates a file with one URL per line, suitable for
// the --input-file/-i flag.
func createInputFile(t *testing.T, dir string, urls ...string) string {
	t.Helper()
	filePath := filepath.Join(dir, "urls.txt")
	content := strings.Join(urls, "\n") + "\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create input file: %v", err)
	}
	return filePath
}

// createInputFileWithComments creates an input file with URLs and comment lines.
func createInputFileWithComments(t *testing.T, dir string, lines ...string) string {
	t.Helper()
	filePath := filepath.Join(dir, "urls_comments.txt")
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create input file: %v", err)
	}
	return filePath
}

// createTempFile creates a temp file with the given content in the given dir.
func createTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create temp file %s: %v", path, err)
	}
	return path
}

// createDummyFile creates a file of the given size filled with zeros.
func createDummyFile(t *testing.T, path string, size int64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create dummy file %s: %v", path, err)
	}
	defer f.Close()
	if _, err := io.Copy(f, io.LimitReader(zeroReader{}, size)); err != nil {
		t.Fatalf("failed to write dummy file %s: %v", path, err)
	}
}

// zeroReader implements io.Reader that produces zero bytes.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// ---------------------------------------------------------------------------
// Extension test helpers
// ---------------------------------------------------------------------------

// createTestExtension creates a minimal JavaScript extension file for testing.
func createTestExtension(t *testing.T, dir, name string) string {
	t.Helper()
	extDir := filepath.Join(dir, "extensions")
	if err := os.MkdirAll(extDir, 0755); err != nil {
		t.Fatalf("failed to create extensions dir: %v", err)
	}

	extPath := filepath.Join(extDir, name+".js")
	content := `// Test extension
module.exports = {
    name: "` + name + `",
    version: "1.0.0",
    transform: function(url) {
        return url;
    }
};
`
	if err := os.WriteFile(extPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to create test extension: %v", err)
	}
	return extPath
}
