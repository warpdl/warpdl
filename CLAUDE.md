# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository

- **GitHub**: https://github.com/warpdl/warpdl
- **Issues**: https://github.com/warpdl/warpdl/issues
- **Main branch**: `dev` (PRs target `dev`, not `main`)

## Build Commands

```bash
# Build the binary (stripped, optimized)
make build
# or directly:
go build -ldflags="-w -s" .

# Run tests
go test ./...

# Run a single test
go test -run TestName ./pkg/warplib/

# Run tests with race detection
go test -race -short ./...

# Run coverage check (same as CI)
scripts/check_coverage.sh

# Run E2E tests (requires network, uses build tag)
go test -v -tags=e2e -timeout=10m ./tests/e2e/

# Build with goreleaser (snapshot, no publish)
make goreleaser

# Format code
go fmt ./...

# Tidy dependencies
go mod tidy
```

**Go version**: 1.24.9+ (see `go.mod`). No golangci-lint configured; use `go fmt` and `go vet`.

## Architecture

WarpDL is a daemon-based download manager with parallel segment downloading. The architecture separates CLI commands from a background daemon that manages downloads.

### Entry Point

`main.go` → `cmd.Execute()` → `cmd.GetApp()` builds the `cli.App`. The default command (no subcommand) is `download`. Version, commit, date, and buildType are injected via ldflags at build time.

### Package Structure

- **`cmd/`** - CLI application using urfave/cli. Commands: download, resume, list, daemon, stop-daemon, attach, stop, flush, ext, native-host. Global flags: `--daemon-uri`, `--cookie`, `--debug/-d`.
- **`cmd/ext/`** - Extension management subcommands (install, uninstall, activate, deactivate)
- **`cmd/nativehost/`** - Browser native messaging host commands (install, uninstall, status)
- **`pkg/warplib/`** - Core download engine:
  - `manager.go` - Download state persistence (GOB encoded to `~/.config/warpdl/userdata.warp`)
  - `dloader.go` - Parallel segment downloader with HTTP range requests
  - `handlers.go` - 15+ event callback types for progress, errors, completion, retries
  - `item.go` - Download item (`Item`) and segment state (`ItemPart` with initial/final offsets)
  - `checksum.go` - Automatic checksum validation (MD5, SHA1, SHA256, SHA512 from HTTP headers)
  - `worksteal.go` - Fast parts steal remaining bytes from slow adjacent parts
  - `queue.go` - Priority queue manager (low/normal/high) for concurrent download limiting
  - `retry.go` - Automatic retry with exponential backoff
- **`pkg/warpcli/`** - CLI-to-daemon communication (custom binary protocol over sockets, bufio-based)
- **`pkg/credman/`** - Credential management with OS keyring integration and file-based fallback
- **`pkg/logger/`** - Centralized logging with debug mode support
- **`internal/server/`** - Daemon server with `SyncConn` (mutex-protected read/write), connection `Pool` for broadcasting progress to connected clients, and a secondary HTTP web server on `port+1`
- **`internal/api/`** - API handlers (`Api` struct holds logger, manager, extension engine, client). 15 update types mapped to handler functions.
- **`internal/extl/`** - JavaScript extension engine using Goja runtime for URL transformation hooks
- **`internal/daemon/`** - Daemon lifecycle management and runner
- **`internal/nativehost/`** - Browser extension native messaging implementation
- **`internal/service/`** - Windows service management
- **`common/`** - Shared types and constants (UpdateType, DownloadingAction enums)

### Data Flow

```
CLI Command → pkg/warpcli → Unix socket/named pipe → internal/server → internal/api → pkg/warplib → HTTP download
                                                          ↓
                                                    Pool broadcasts progress → connected CLI clients
```

### Key Design Patterns

- **Handler Pattern**: Event callbacks in warplib for progress tracking (SpawnPartHandlerFunc, DownloadProgressHandlerFunc, RetryHandlerFunc, ChecksumValidationHandlerFunc, etc.)
- **Work Stealing**: Fast downloader segments steal remaining bytes from slow adjacent segments to maximize throughput
- **Manager Pattern**: Centralized state in Manager (warplib) with GOB persistence, and Engine (extl)
- **Connection Pool**: Server broadcasts download progress updates to all connected CLI clients
- **Socket Fallback**: Unix socket → TCP on Linux/macOS; named pipe → TCP on Windows
- **Daemon Architecture**: Single daemon process serves multiple CLI clients concurrently

### Storage Locations

- Downloads metadata: `~/.config/warpdl/userdata.warp` (GOB-encoded `ManagerData{Items, QueueState}`)
- Extensions: `~/.config/warpdl/extstore/`
- Daemon socket: `/tmp/warpdl.sock` (Unix), named pipe (Windows)

## Commit Message Format

Follow this format from CONTRIBUTING.md:

```
<major_area>...: <type>: <commit-message>

<commit-description>
```

**Major areas**: `core`, `daemon`, `cli`, `api`, `extl`, `credman`, `docs`, `debug`

**Types**: `feat`, `fix`, `refactor`, `perf`, `test`, `chore`, `temp`

Example: `core,daemon: feat: implemented feature X`

## Platform-Specific Code

- Build tags: `//go:build` with `unix`, `windows`, `darwin`, `linux`
- `file_unix.go` / `file_windows.go` in warplib for file operations (Windows has 260-char path limit workaround via UNC paths)
- `cmd/uagent_darwin.go`, `cmd/uagent_linux.go` - Platform user-agents
- `cmd/cookieMan.go` - Cross-platform credential management (uses go-keyring)
- Windows: Named pipes via `Microsoft/go-winio`; falls back to TCP
- Disk space: Unix `Statfs`, Windows `GetDiskFreeSpaceEx`
- Retry errno: Windows `ERROR_PIPE_NOT_CONNECTED`, Unix domain socket timeouts
- Test files: `*_unix_test.go` / `*_windows_test.go`

## Testing

- **Minimum coverage**: 80% per package (enforced by CI via `scripts/check_coverage.sh`)
- **CI platforms**: Ubuntu + macOS (tests + coverage + race), Windows (build-only)
- **E2E tests**: `tests/e2e/` with `//go:build e2e` tag, downloads real 100MB files from Hetzner mirrors (4 locations, any-pass logic)
- **Test isolation**: `cmd/` tests set `WARPDL_TEST_SKIP_DAEMON=1` via TestMain to disable daemon interactions
- **Test helpers**: `cmd/test_helpers_test.go` provides `captureOutput()`, `newContext()`, assertion helpers
- Coverage check: `go test -cover ./pkg/warplib/...`
- Detailed report: `go test -coverprofile=cover.out ./... && go tool cover -func=cover.out`
- Race detection also runs: `scripts/go_test_retry.sh -race -short ./...` (retry wrapper for flaky race tests)

## Key Dependencies

- `github.com/dop251/goja` - JavaScript runtime for extensions
- `github.com/urfave/cli` - CLI framework (v1)
- `github.com/vbauerster/mpb/v8` - Progress bars
- `github.com/zalando/go-keyring` - OS credential storage
- `github.com/Microsoft/go-winio` - Windows named pipe support

## Active Technologies
- Go 1.24.9+ (CGO_ENABLED=0) + `modernc.org/sqlite` v1.46.1 (pure-Go SQLite, BSD-3-Clause), `adhocore/gronx` (cron parser, MIT) (001-scheduling-cookie-import)
- GOB-encoded files (`~/.config/warpdl/userdata.warp`) — existing pattern, extended with new Item fields (001-scheduling-cookie-import)

## Recent Changes
- 001-scheduling-cookie-import: Added Go 1.24.9+ (CGO_ENABLED=0) + `modernc.org/sqlite` v1.46.1 (pure-Go SQLite, BSD-3-Clause), `adhocore/gronx` (cron parser, MIT)
