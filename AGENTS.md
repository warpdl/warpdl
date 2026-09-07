# Repository Guidelines

## Project Structure & Module Organization

WarpDL is a cross-platform download manager written in Go. Key directories:

- `cmd/` - CLI commands, client code, and extension handling
- `internal/` - Private packages: `api`, `daemon`, `extl`, `scheduler`, `server`, `service`
- `pkg/` - Public packages: `credman` (credential manager), `logger`, `warpcli` (client library), `warplib` (core download library)
- `common/` - Shared types and constants
- `tests/e2e/` - End-to-end test suite
- `scripts/` - Build and CI helper scripts

Entry point: `main.go` calls `cmd.Execute()`.

## Build, Test, and Development Commands

```bash
# Build the binary
go build -ldflags="-w -s" .
# or
make build

# Run all tests
go test ./...

# Run tests with coverage (80% minimum required)
scripts/check_coverage.sh

# Run race condition tests
go test -race -short ./...

# Run E2E tests
go test -v -tags=e2e ./tests/e2e/

# Run specific package tests
go test ./pkg/warplib/...
```

## Coding Style & Naming Conventions

- Go 1.25+ required
- Standard `gofmt` formatting
- Test files use `*_test.go` suffix with `_test` package for integration tests
- Platform-specific code uses `_unix.go`, `_windows.go`, `_darwin.go` suffixes

### Naming Rules

**Packages:**
- Lowercase, single word preferred (e.g., `api`, `cookies`, `scheduler`)
- No underscores or mixedCaps in package names
- Package name should match the directory name

**Types (structs, interfaces):**
- Exported types: `PascalCase` (e.g., `DownloadManager`, `ItemQueue`)
- Unexported types: `camelCase` (e.g., `itemStore`, `connPool`)
- Interfaces: end with `-er` suffix when describing behavior (e.g., `Reader`, `Writer`, `Downloader`, `Handler`)
- Interface implementations should NOT repeat the interface name (use `Downloader` not `DownloaderImpl`)

**Functions and Methods:**
- Exported: `PascalCase` (e.g., `GetItem`, `StartDownload`, `NewManager`)
- Unexported: `camelCase` (e.g., `parseURL`, `validatePath`, `initLogger`)
- Factory functions: prefix with `New` (e.g., `NewManager`, `NewClient`)
- Constructors returning error: `NewX` returns `(*X, error)` pattern
- Getters: no `Get` prefix for simple field access (e.g., `item.Status()` not `item.GetStatus()`)
- Setters: use `Set` prefix (e.g., `SetStatus`, `SetTimeout`)

**Variables and Constants:**
- Local variables: `camelCase` (e.g., `itemCount`, `downloadPath`)
- Constants: `PascalCase` when exported, `camelCase` when unexported
- Acronyms: preserve case (e.g., `HTTPClient`, `URLParser`, `maxID`, not `HttpClient`, `UrlParser`)
- Common initialisms: `ID`, `URL`, `HTTP`, `JSON`, `API`, `CPU`, `TCP`, `UDP`, `FTP`, `SSH`

**Error Variables:**
- Prefix with `Err` (e.g., `ErrNotFound`, `ErrInvalidInput`)
- Error messages: do not capitalize first letter, no trailing period
- Error types: end with `Error` (e.g., `DownloadError`, `ValidationError`)

**Test Files and Functions:**
- Test files: `*_test.go` suffix
- Test functions: `Test<Name>` (e.g., `TestDownload`, `TestQueueOperations`)
- Benchmark functions: `Benchmark<Name>` (e.g., `BenchmarkDownload`)
- Example functions: `Example<Name>` (e.g., `ExampleManager_Download`)
- Table-driven tests: use `tests := struct{...}` pattern

**File Naming:**
- Platform-specific: `filename_unix.go`, `filename_windows.go`, `filename_darwin.go`
- Architecture-specific: `filename_arm64.go`, `filename_amd64.go`
- Build-constrained files use `//go:build` directive at top

## Testing Guidelines

- Minimum 80% test coverage required (enforced in CI)
- Race condition testing enabled on Linux and macOS
- E2E tests require `-tags=e2e` build tag
- Test naming: `Test<Feature>` or `Test<Feature>_<Variant>`

## Commit & Pull Request Guidelines

Commit message format:

```
<area>: <type>: <message>

<optional description>
```

**Areas:** `core`, `docs`, `api`, `daemon`, `extl`, `debug`, `cli`, `credman`

**Types:** `feat`, `fix`, `refactor`, `perf`, `test`, `chore`, `temp`

Examples:
- `core,daemon: feat: implemented 'X'`
- `docs: fix a typo in README`
- `cli: refactor: simplified command parsing`

PR requirements:
- Branch from `dev` (development branch)
- Ensure all tests pass locally
- Follow commit message conventions
- Link related issues
