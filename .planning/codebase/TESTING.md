# Testing Patterns

**Analysis Date:** 2026-02-26

## Test Framework

**Runner:**
- Go standard testing (`testing` package)
- Test execution via `go test ./...`
- Test file suffix: `*_test.go`

**Run Commands:**
```bash
go test ./...                              # Run all tests
go test -run TestName ./pkg/warplib/      # Run specific test by pattern
go test -race ./...                        # Run with race detection enabled
go test -race -short ./...                 # Run with race detection and short timeout
go test ./... -coverprofile=cover.out     # Run with coverage report
go tool cover -func=cover.out              # View function-level coverage
go test -cover ./pkg/warplib/...          # Check specific package coverage
scripts/check_coverage.sh                  # Run coverage validation script
```

**Coverage Requirements:**
- Minimum total: 80% (enforced by CI)
- Minimum per-package: 80%
- Target locally: ~85% to ensure CI passes (platform-specific code paths may differ)
- Script: `scripts/check_coverage.sh` verifies coverage thresholds

Coverage script (`scripts/check_coverage.sh`) enforces:
- Total coverage >= 80% (configurable via `COVERAGE_MIN` env var)
- Per-package coverage >= 80% (configurable via `COVERAGE_MIN_PER_PKG` env var)
- Fails if any package has `[no test files]`
- Uses `go test -coverprofile` and `go tool cover` for reporting

## Test File Organization

**Location:**
- Co-located with source files in same package
- Convention: `source_file_test.go` in same directory as `source_file.go`
- Examples:
  - `cmd/cmd.go` paired with `cmd/cmd_test.go`
  - `cmd/counter_test.go` tests speed counter implementation
  - `cmd/daemon_pidfile_test.go` tests PID file handling

**Naming:**
- Test functions: `TestFunctionName()` - matches function being tested
- Variant tests: `TestFunctionName_Variant()` - describes test scenario
- Examples from codebase:
  - `TestDownloadCommand()` - tests download command basic functionality
  - `TestDownloadCommand_WithUserAgent()` - tests with specific UA variant
  - `TestListEmpty()` - tests list when no downloads exist
  - `TestConfirm_Force()`, `TestConfirm_YesInput()` - test input variations
  - `TestGetUserAgent_Chrome()`, `TestGetUserAgent_Firefox()` - browser variants
  - `TestGetUserAgent_CaseInsensitive()` - edge case testing

**Platform-specific Tests:**
- `*_unix_test.go` for Unix-only tests
- `*_windows_test.go` for Windows-only tests
- Example: `cmd/cmd_unix_test.go`, `cmd/cmd_windows_test.go`

**Structure by Package:**
```
cmd/
  cmd.go                      # CLI application setup
  cmd_test.go                 # Main command tests
  cmd_unix_test.go           # Unix-specific tests
  cmd_windows_test.go        # Windows-specific tests
  download.go                # Download command implementation
  daemon.go                  # Daemon command
  daemon_test.go             # Daemon tests
  daemon_windows.go          # Windows daemon implementation
  daemon_windows_test.go     # Windows daemon tests
  test_helpers_test.go       # Shared test utilities
  test_main_test.go          # Package-level test setup
```

## Test Structure

**TestMain Setup:**
From `cmd/test_main_test.go`:
```go
func TestMain(m *testing.M) {
	_ = os.Setenv("WARPDL_TEST_SKIP_DAEMON", "1")
	os.Exit(m.Run())
}
```
- Sets test environment variables before all tests
- `WARPDL_TEST_SKIP_DAEMON` prevents daemon startup during testing

**Suite Organization:**
Tests are organized by functionality, not in formal test suites. Each test function is independent:
- Setup in test function using `t.TempDir()` for isolation
- Cleanup via `defer` or `t.Cleanup()` calls
- No shared state between tests

Example from `cmd/cmd_test.go` - basic structure:
```go
func TestDownloadCommand(t *testing.T) {
    socketPath := filepath.Join(t.TempDir(), "warpdl.sock")
    t.Setenv("WARPDL_SOCKET_PATH", socketPath)
    srv := startFakeServer(t, socketPath)
    defer srv.close()

    app := cli.NewApp()
    ctx := newContext(app, []string{"http://example.com"}, "download")
    // Test execution
    if err := download(ctx); err != nil {
        t.Fatalf("download: %v", err)
    }
}
```

**Patterns:**

1. **Temporary directory isolation:**
```go
tmpDir := t.TempDir()  // Auto-cleanup after test
```

2. **Environment variable setup:**
```go
t.Setenv("VAR_NAME", "value")  // Auto-cleanup after test
```

3. **Helper deferred cleanup:**
```go
defer func() {
    oldValue := storeValue
    storeValue = testValue
    defer func() { storeValue = oldValue }()
}()
```

4. **State mutation and restoration:**
```go
// From cmd/cmd_test.go
oldDlPath, oldFileName := dlPath, fileName
dlPath = t.TempDir()
fileName = "custom.bin"
defer func() {
    dlPath = oldDlPath
    fileName = oldFileName
}()
```

## Mocking

**Approach:**
- Manual mock servers created for integration testing
- No dedicated mocking framework (no `testify/mock`, `gomock`, etc.)
- Mock servers are test-local helper functions

**Fake Server Implementation:**

From `cmd/cmd_test.go` - `startFakeServer()` pattern:
```go
type fakeServer struct {
    listener net.Listener
    wg       sync.WaitGroup
}

func startFakeServer(t *testing.T, socketPath string, fail ...map[common.UpdateType]string) *fakeServer {
    t.Helper()
    suppressVersionCheck(t)

    listener, err := createTestListener(t, socketPath)
    if err != nil {
        t.Fatalf("listen: %v", err)
    }

    srv := &fakeServer{listener: listener}
    var failMap map[common.UpdateType]string
    if len(fail) > 0 {
        failMap = fail[0]
    }

    srv.wg.Add(1)
    go func() {
        defer srv.wg.Done()
        for {
            conn, err := listener.Accept()
            if err != nil {
                return
            }
            // Handle each connection
            srv.wg.Add(1)
            go func(c net.Conn) {
                defer srv.wg.Done()
                defer c.Close()
                // Parse JSON-RPC requests and send responses
            }(conn)
        }
    }()

    return srv
}

func (s *fakeServer) close() {
    _ = s.listener.Close()
    s.wg.Wait()
}
```

**Mock Protocol:**
Fake server responds to JSON-RPC style requests with predefined responses:
```go
// From cmd/cmd_test.go
switch req.Method {
case common.UPDATE_DOWNLOAD:
    resp := common.DownloadResponse{
        DownloadId:        "id",
        FileName:          "file.bin",
        SavePath:          "file.bin",
        DownloadDirectory: ".",
        ContentLength:     warplib.ContentLength(10),
        MaxConnections:    1,
        MaxSegments:       1,
    }
    writeResponse(c, req.Method, resp)
case common.UPDATE_LIST:
    items := listOverride  // Allow test to inject custom data
    if items == nil {
        items = []*warplib.Item{{...}}  // Default response
    }
    resp := common.ListResponse{Items: items}
    writeResponse(c, req.Method, resp)
}
```

**Failure Injection:**
```go
// From cmd/cmd_test.go
srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
    common.UPDATE_DOWNLOAD: "download failed",
})
```
Maps method types to error messages for testing error handling.

**What to Mock:**
- Network communication (fake servers for daemon communication)
- File system operations (use `t.TempDir()` instead of actual filesystem)
- HTTP servers (use `httptest.NewServer` from standard library)

**What NOT to Mock:**
- Standard library functions
- Environment variables (use `t.Setenv()` instead)
- Go concurrency primitives
- Error types

## Fixtures and Factories

**Test Data:**
From `cmd/cmd_test.go`:
```go
// Global test data override
var listOverride []*warplib.Item

// In test:
listOverride = []*warplib.Item{{
    Hash:       "id",
    Name:       "file.bin",
    TotalSize:  10,
    Downloaded: 10,
    Hidden:     false,
    Children:   false,
    DateAdded:  time.Now(),
    Resumable:  true,
    Parts:      make(map[int64]*warplib.ItemPart),
}}
defer func() { listOverride = nil }()
```

**Factory Functions:**
From `cmd/test_helpers_test.go`:
```go
// Context factory
func newContext(app *cli.App, args []string, name string) *cli.Context {
    set := flag.NewFlagSet(name, flag.ContinueOnError)
    _ = set.Parse(args)
    ctx := cli.NewContext(app, set, nil)
    ctx.Command = cli.Command{Name: name}
    return ctx
}
```

**Output Capture Utilities:**
From `cmd/test_helpers_test.go`:
```go
// Captures stdout/stderr during execution
func captureOutput(f func()) (stdout, stderr string) {
    oldStdout := os.Stdout
    oldStderr := os.Stderr

    rOut, wOut, _ := os.Pipe()
    rErr, wErr, _ := os.Pipe()
    os.Stdout = wOut
    os.Stderr = wErr

    f()

    wOut.Close()
    wErr.Close()
    os.Stdout = oldStdout
    os.Stderr = oldStderr

    var bufOut, bufErr bytes.Buffer
    io.Copy(&bufOut, rOut)
    io.Copy(&bufErr, rErr)

    return bufOut.String(), bufErr.String()
}
```

**Assertion Helpers:**
From `cmd/test_helpers_test.go`:
```go
func assertContains(t *testing.T, output, expected string) {
    t.Helper()
    if !strings.Contains(output, expected) {
        t.Errorf("expected output to contain %q, got:\n%s", expected, output)
    }
}

func assertErrorFormat(t *testing.T, output, cmd, action string) {
    t.Helper()
    pattern := "warpdl: " + cmd + "[" + action + "]:"
    if !strings.Contains(output, pattern) {
        t.Errorf("expected error format %q, got:\n%s", pattern, output)
    }
}
```

**Location:**
- `cmd/test_helpers_test.go` - Shared CLI test utilities
- `cmd/test_main_test.go` - Package initialization for tests

## Coverage

**Coverage Check Command:**
```bash
# Run from project root
scripts/check_coverage.sh
```

**Coverage Report:**
```bash
# Generate profile
go test ./... -coverprofile=cover.out

# View detailed report
go tool cover -html=cover.out  # Opens in browser
go tool cover -func=cover.out  # Terminal output
```

**Per-Package Coverage:**
```bash
go test -cover ./pkg/warplib/...
go test -cover ./cmd/...
```

**Notes:**
- Coverage requirements may differ between platforms due to `*_unix.go` and `*_windows.go` files
- Windows CI may have different coverage paths than local macOS/Linux
- Scripts in `scripts/` include platform-specific adjustments

## Test Types

**Unit Tests:**
- Scope: Individual functions and methods in isolation
- Pattern: Test function name matches function name
- Examples:
  - `TestParseCookieFlags_SingleCookie()` - tests `ParseCookieFlags()` with one cookie
  - `TestGetUserAgent_Chrome()` - tests `getUserAgent()` with chrome input
  - `TestConfirm_Force()` - tests `confirm()` with force flag
  - `TestSpeedCounter_NilBar()` - tests speed counter handles nil bar safely

**Integration Tests:**
- Scope: Multiple components working together
- Fake servers for daemon communication
- Real file I/O with `t.TempDir()`
- Examples:
  - `TestDownloadCommand()` - tests download CLI with fake daemon server
  - `TestDownloadPathDefault()` - tests download path resolution with environment
  - `TestFlushCancelled()` - tests user confirmation flow with stdin/stdout

**E2E Tests:**
- Status: Not present in unit test suite
- Location: `tests/e2e/cli_download_test.go` exists but separate from main test suite
- Run via separate test target if needed

**Platform-Specific Tests:**
- Unix: `*_unix_test.go` files test Unix-only behavior
- Windows: `*_windows_test.go` files test Windows-only behavior
- Shared: `*_test.go` files run on all platforms
- Examples:
  - `cmd/cmd_unix_test.go` - daemon shutdown, socket handling on Unix
  - `cmd/cmd_windows_test.go` - service management on Windows

## Common Patterns

**Async Testing with WaitGroup:**
From `cmd/counter_test.go`:
```go
func TestSpeedCounter_SetBar_Concurrent(t *testing.T) {
    sc := NewSpeedCounter(time.Millisecond)
    p := mpb.New()
    bar1 := p.AddBar(100)

    sc.Start()
    defer sc.Stop()

    var wg sync.WaitGroup
    // Spawn goroutines that call SetBar concurrently
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            if i%2 == 0 {
                sc.SetBar(bar1)
            } else {
                sc.SetBar(bar2)
            }
        }(i)
    }

    wg.Wait()
    // Test passes if no race detected (run with -race flag)
}
```

**Error Testing:**
```go
func TestParseCookieFlags_InvalidFormat(t *testing.T) {
    tests := []struct {
        name   string
        flags  []string
        errMsg string
    }{
        {
            name:   "missing equals sign",
            flags:  []string{"invalid-cookie"},
            errMsg: "invalid cookie format",
        },
        // ... more cases
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            _, err := ParseCookieFlags(tt.flags)
            if err == nil {
                t.Fatalf("expected error, got nil")
            }
            if !strings.Contains(err.Error(), tt.errMsg) {
                t.Errorf("expected %q in error, got %v", tt.errMsg, err)
            }
        })
    }
}
```

**Table-Driven Tests:**
Common pattern in the codebase - use struct slices to test multiple cases:
```go
tests := []struct {
    name   string
    input  string
    expect string
}{
    {"case1", "input1", "expected1"},
    {"case2", "input2", "expected2"},
}

for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // Test logic
    })
}
```

**Stdin/Stdout Capture:**
```go
func TestFlushCancelled(t *testing.T) {
    oldStdin := os.Stdin
    r, w, err := os.Pipe()
    if err != nil {
        t.Fatalf("Pipe: %v", err)
    }
    _, _ = w.Write([]byte("no\n"))
    _ = w.Close()
    os.Stdin = r
    defer func() {
        os.Stdin = oldStdin
        _ = r.Close()
    }()

    // Test code that reads from stdin
}
```

**Race Detection:**
Run with `-race` flag to catch data races:
```bash
go test -race ./...
go test -race -short ./...  # Shorter timeout for CI
```

---

*Testing analysis: 2026-02-26*
