# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

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

# Build with goreleaser (snapshot, no publish)
make goreleaser

# Format code
go fmt ./...

# Tidy dependencies
go mod tidy
```

## Architecture

WarpDL is a daemon-based download manager with parallel segment downloading. The architecture separates CLI commands from a background daemon that manages downloads.

### Package Structure

- **`cmd/`** - CLI application using urfave/cli. Entry point delegates to these commands (download, resume, list, daemon, etc.)
- **`cmd/ext/`** - Extension management subcommands (install, uninstall, activate, deactivate)
- **`pkg/warplib/`** - Core download engine. Key files:
  - `manager.go` - Download state persistence (GOB encoded to `~/.config/warp/userdata.warp`)
  - `dloader.go` - Parallel segment downloader with HTTP range requests
  - `handlers.go` - Event-driven callbacks for progress, errors, completion
  - `item.go` - Download item and part state
- **`pkg/warpcli/`** - CLI-to-daemon communication layer (JSON-RPC style over Unix socket)
- **`pkg/credman/`** - Credential management with OS keyring integration
- **`internal/server/`** - Daemon server (Unix domain socket at `/tmp/warpdl.sock`, falls back to TCP)
- **`internal/api/`** - API handlers implementing download, resume, stop, extension management
- **`internal/extl/`** - JavaScript extension engine using Goja runtime
- **`common/`** - Shared types and constants (UpdateType, DownloadingAction enums)

### Data Flow

```
CLI Command → pkg/warpcli → Unix socket → internal/server → internal/api → pkg/warplib → HTTP download
```

### Key Design Patterns

- **Handler Pattern**: Event callbacks in warplib for progress tracking (SpawnPartHandlerFunc, DownloadProgressHandlerFunc, etc.)
- **Manager Pattern**: Centralized state in Manager (warplib) and Engine (extl)
- **Daemon Architecture**: Single daemon process serves multiple CLI clients

### Storage Locations

- Downloads metadata: `~/.config/warpdl/userdata.warp`
- Extensions: `~/.config/warpdl/extstore/`
- Daemon socket: `/tmp/warpdl.sock`

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

- `cmd/uagent_darwin.go`, `cmd/uagent_linux.go` - Platform user-agents
- `cmd/cookieMan_linux.go` - Linux-specific credential integration
- Keyring abstraction via `zalando/go-keyring`

## Test Coverage Requirements

- **Minimum coverage**: 80% per package (enforced by CI)
- Run coverage check: `scripts/check_coverage.sh`
- Check specific package: `go test -cover ./pkg/warplib/...`
- Detailed report: `go test -coverprofile=cover.out ./... && go tool cover -func=cover.out`

Note: Coverage may differ slightly between Linux (CI) and macOS (local) due to platform-specific code paths. Aim for ~85% locally to ensure CI passes.

## Key Dependencies

- `github.com/dop251/goja` - JavaScript runtime for extensions
- `github.com/urfave/cli` - CLI framework
- `github.com/vbauerster/mpb/v8` - Progress bars
- `github.com/zalando/go-keyring` - OS credential storage
