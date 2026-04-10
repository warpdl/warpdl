# Repository Guidelines

## Project Structure & Module Organization

WarpDL is a cross-platform download manager written in Go. Key directories:

- `cmd/` - CLI commands, client code, and extension handling
- `internal/` - Private packages: `api`, `cookies`, `daemon`, `extl`, `nativehost`, `scheduler`, `server`, `service`
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
