# Coding Conventions

**Analysis Date:** 2026-02-26

## Naming Patterns

**Files:**
- Source files: `lowercase_with_underscores.go` (e.g., `cookie_parser.go`, `daemon_pidfile.go`)
- Test files: `*_test.go` suffix (e.g., `cmd_test.go`)
- Platform-specific files: `*_unix.go`, `*_windows.go`, `*_darwin.go` (e.g., `cmd_unix_test.go`, `stop_daemon_windows.go`)
- Test variants: `*_unix_test.go`, `*_windows_test.go` for platform-specific tests

**Functions:**
- Unexported functions: camelCase starting with lowercase (e.g., `download()`, `resolveDownloadPath()`, `parsePriority()`)
- Exported functions: PascalCase starting with uppercase (e.g., `Execute()`, `GetApp()`, `ParseCookieFlags()`)
- Handler functions: verb-noun pattern with Handler suffix (e.g., `getDaemonAction()`, `startFakeServer()`)
- Helper functions: simple names describing action (e.g., `writeMessage()`, `readMessage()`, `captureOutput()`)

**Variables:**
- Local variables: camelCase (e.g., `socketPath`, `absPath`, `dlPath`, `proxyURL`, `selectedPath`)
- Package-level variables: camelCase (e.g., `dlFlags`, `dlPath`, `fileName`, `proxyURL`, `globalFlags`, `listOverride`)
- Constants: UPPER_SNAKE_CASE (e.g., `DEF_MAX_PARTS`, `DEF_MAX_CONNS`, `UPDATE_VERSION`)
- Error variables: err (standard Go convention)

**Types:**
- Structs: PascalCase (e.g., `BuildArgs`, `DownloadParams`, `DownloadResponse`, `fakeServer`)
- Interfaces: PascalCase ending with "Handler" or descriptive name
- Exported types: Fully documented with comments above definition (e.g., `BuildArgs` at `cmd/cmd.go` lines 21-30)

## Code Style

**Formatting:**
- Go standard formatter: `go fmt` applied to all files
- Indentation: Tabs (Go standard)
- Line length: No strict limit, but keep reasonable
- Imports: Organized in groups: stdlib, then external packages, then internal packages

**Linting:**
- Not explicitly enforced in build, but code should follow Go conventions
- Platform-specific code isolated in separate files with build tags

**Import Organization:**
Order follows Go conventions:
1. Standard library imports (`"fmt"`, `"os"`, `"testing"`)
2. External packages (`"github.com/urfave/cli"`, `"github.com/vbauerster/mpb/v8"`)
3. Internal packages (`"github.com/warpdl/warpdl/..."`)

Example from `cmd/cmd_test.go`:
```go
import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urfave/cli"
	"github.com/vbauerster/mpb/v8"
	cmdcommon "github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warpcli"
	"github.com/warpdl/warpdl/pkg/warplib"
)
```

**Path Aliases:**
- `cmdcommon` for `github.com/warpdl/warpdl/cmd/common` - avoids conflict with `common` package
- `sharedcommon` for `github.com/warpdl/warpdl/common` - distinguishes from local `cmd/common`

## Error Handling

**Patterns:**

1. **Wrapped errors with context** - Use `fmt.Errorf()` with `%w` for error wrapping:
```go
// From cmd/download.go
return "", fmt.Errorf("failed to get current directory: %w", err)
return "", fmt.Errorf("invalid download directory: %w", err)
```

2. **Simple error returns** - Use `errors.New()` for constant error messages:
```go
// From cmd/download.go
errors.New("no url provided (use URL argument or -i/--input-file)")
```

3. **Error printing with context** - Use `PrintRuntimeErr()` for CLI errors:
```go
// From cmd/cmd.go, cmd/list.go, cmd/resume.go, etc.
cmdcommon.PrintRuntimeErr(ctx, "download", "new_client", err)
```
Format: `warpdl: command[action]: error message`

4. **Nil error checking** - Check before processing:
```go
// From cmd/test_helpers_test.go
if err != nil {
	t.Fatalf("unexpected error: %v", err)
}
```

5. **Error responses with help** - Print error and command help:
```go
// From cmd/download.go
return cmdcommon.PrintErrWithCmdHelp(ctx, errors.New("..."))
```

## Logging

**Framework:** No dedicated logging framework - uses `fmt` package for output
- `fmt.Println()` for general output
- `fmt.Printf()` for formatted output
- `fmt.Errorf()` for error construction with context

**Patterns:**
- CLI output via `fmt.Println()` or `fmt.Printf()`
- Progress via progress bar library (`mpb`) not logging
- Errors printed with context using `cmdcommon.PrintRuntimeErr()`
- Debug mode can be enabled via `--debug` or `-d` flag (sets `WARPDL_DEBUG=1` env var)

Example from `cmd/cmd.go` lines 195-202:
```go
Before: func(ctx *cli.Context) error {
    if ctx.GlobalBool("debug") {
        _ = os.Setenv(sharedcommon.DebugEnv, "1")
    }
    return nil
},
```

## Comments

**When to Comment:**
- Function documentation: Always use doc comments above exported functions
- Complex logic: Comment non-obvious algorithm steps or workarounds
- Gotchas: Comment platform-specific behavior or edge cases
- Test intent: Explain what the test validates, especially for complex scenarios

**Doc Comments/Comments:**
- Exported types: Full sentence starting with type name (e.g., `// BuildArgs contains build-time information...`)
- Exported functions: Sentence starting with function name (e.g., `// GetApp returns the configured CLI application...`)
- Struct fields: Brief description of field purpose and usage
- Test comments: Describe what the test validates

Example from `cmd/cmd.go` lines 18-30:
```go
// BuildArgs contains build-time information passed to the CLI application.
// These values are typically injected during the build process via ldflags
// and are used to display version and build information to users.
type BuildArgs struct {
	// Version is the semantic version of the application.
	Version string
	// BuildType indicates the build variant (e.g., "release", "debug", "snapshot").
	BuildType string
	// ...
}
```

Test comments from `cmd/counter_test.go`:
```go
// TestSpeedCounter_SetBar_Concurrent tests for race conditions when SetBar and IncrBy
// are called concurrently. Run with: go test -race -run TestSpeedCounter_SetBar_Concurrent
func TestSpeedCounter_SetBar_Concurrent(t *testing.T) {
```

## Function Design

**Size:**
- Small to medium functions (10-50 lines typical)
- Single responsibility principle
- Command handlers typically 30-100 lines with clear separation of concerns

**Parameters:**
- Keep parameter count low (usually 1-4 for most functions)
- Use structs for multiple related parameters (e.g., `DownloadParams`, `ResumeParams`)
- Context parameters passed to CLI command handlers via `*cli.Context`

Example from `cmd/download.go`:
```go
func resolveDownloadPath(cliPath string) (string, error) // Simple: 1 param, 2 returns
```

**Return Values:**
- Multiple returns common: `(result, error)` pattern
- Named returns rarely used
- Early returns for error conditions

Example from `cmd/cmd_test.go`:
```go
func startFakeServer(t *testing.T, socketPath string, fail ...map[common.UpdateType]string) *fakeServer
// Returns pointer to struct, used with cleanup via defer
```

## Module Design

**Exports:**
- Selective export of public APIs
- Unexported helper functions for implementation details
- Clear separation between command handlers (unexported) and utilities (exported as needed)

**Package Structure:**
- `cmd/` - CLI commands and handlers (mostly unexported functions)
- `cmd/common/` - Shared CLI utilities (exported: `Help()`, `PrintRuntimeErr()`, `InitBars()`)
- `pkg/warpcli/` - Client library for daemon communication (exported API)
- `pkg/warplib/` - Core download engine (exported types: `Item`, `Manager`)
- `internal/` - Implementation details not exposed to external consumers
- `common/` - Shared types and constants across all packages

**Barrel Files:**
- Not used; packages export functions and types directly
- No init() functions that perform global setup

---

*Convention analysis: 2026-02-26*
