# Codebase Structure

**Analysis Date:** 2026-02-26

## Directory Layout

```
warpdl/
├── cmd/                          # CLI command handlers (61 files)
│   ├── main entry point delegation
│   ├── download.go              # Download command handler
│   ├── resume.go                # Resume command handler
│   ├── list.go                  # List command handler
│   ├── attach.go                # Attach console to download
│   ├── stop.go                  # Stop download command
│   ├── daemon.go                # Daemon startup command
│   ├── daemon_*.go              # Platform-specific daemon logic (Windows/Unix)
│   ├── queue.go                 # Queue management command
│   ├── config.go                # Configuration management
│   ├── flush.go                 # Clear download history
│   ├── cookie_parser.go         # Parse HTTP cookies
│   ├── uagent_*.go              # Platform user-agent strings
│   ├── common/                  # Shared CLI utilities
│   │   ├── help.go
│   │   ├── usage_error.go
│   │   └── version.go
│   ├── ext/                     # Extension management subcommands
│   │   ├── add.go
│   │   ├── list.go
│   │   ├── get.go
│   │   ├── delete.go
│   │   ├── activate.go
│   │   └── deactivate.go
│   ├── nativehost/              # Browser native messaging subcommands
│   │   ├── install.go
│   │   ├── uninstall.go
│   │   ├── status.go
│   │   ├── run.go
│   │   └── cmd.go
│   └── *_test.go                # 20+ test files
│
├── pkg/                          # Public packages
│   ├── warplib/                 # Core download engine (50+ files)
│   │   ├── manager.go           # Download state persistence (GOB-encoded)
│   │   ├── dloader.go           # Parallel segment downloader (38KB)
│   │   ├── item.go              # Download item and part structures
│   │   ├── part.go              # Part metadata and operations
│   │   ├── queue.go             # Download queue with priority support
│   │   ├── handlers.go          # Event callback function types
│   │   ├── checksum.go          # Checksum validation (MD5/SHA1/SHA256/SHA512)
│   │   ├── speed.go             # Speed limiting and work stealing
│   │   ├── errors.go            # Sentinel error definitions
│   │   ├── clength.go           # Content length parsing/validation
│   │   ├── dir_validation.go    # Directory path validation
│   │   ├── diskspace_*.go       # Platform-specific disk space checks
│   │   ├── file.go              # File operations (platform-agnostic)
│   │   ├── file_*.go            # Platform-specific file ops (Unix/Windows)
│   │   ├── sorter.go            # Item sorting for display
│   │   ├── *_test.go            # 25+ test files covering all operations
│   │   └── *_integration_test.go # Integration tests
│   │
│   ├── warpcli/                 # CLI-to-daemon client (10 files)
│   │   ├── client.go            # Client connection management
│   │   ├── dispatcher.go        # Request/response dispatch
│   │   ├── request.go           # Request building
│   │   ├── listener.go          # Response listening
│   │   ├── uri.go               # Daemon URI parsing
│   │   ├── dial_*.go            # Platform-specific dial logic
│   │   ├── daemon.go            # Ensure daemon is running
│   │   └── *_test.go            # Client tests
│   │
│   ├── credman/                 # Credential management (15 files)
│   │   ├── credman.go           # Main credential interface
│   │   ├── keyring/             # OS keyring integration
│   │   │   ├── keyring_darwin.go
│   │   │   ├── keyring_linux.go
│   │   │   ├── keyring_windows.go
│   │   │   └── *_test.go
│   │   ├── encryption/          # File encryption for stored credentials
│   │   │   ├── encryption.go
│   │   │   └── *_test.go
│   │   ├── types/               # Type definitions
│   │   └── *_test.go
│   │
│   └── logger/                  # Logging utilities (minimal)
│       └── logger.go
│
├── internal/                     # Private packages
│   ├── server/                  # IPC server (31 files)
│   │   ├── server.go            # Main server (connection listener)
│   │   ├── connection.go        # Individual connection handling
│   │   ├── pool.go              # Connection pooling
│   │   ├── webserver.go         # Web UI server (alternate port)
│   │   ├── listener_*.go        # Platform-specific listeners (Unix/Windows)
│   │   ├── sync_conn.go         # Synchronous connection wrapper
│   │   ├── *_test.go            # Server tests
│   │   └── assets/              # Web UI assets (if any)
│   │
│   ├── api/                     # Request handlers (17 files)
│   │   ├── api.go               # Handler registration
│   │   ├── download.go          # Download handler
│   │   ├── resume.go            # Resume handler
│   │   ├── attach.go            # Attach console handler
│   │   ├── stop.go              # Stop handler
│   │   ├── list.go              # List downloads handler
│   │   ├── flush.go             # Flush history handler
│   │   ├── add_ext.go           # Install extension handler
│   │   ├── get_ext.go           # Get extension handler
│   │   ├── list_ext.go          # List extensions handler
│   │   ├── delete_ext.go        # Delete extension handler
│   │   ├── activate_ext.go      # Activate extension handler
│   │   ├── deactivate_ext.go    # Deactivate extension handler
│   │   ├── version.go           # Version info handler
│   │   ├── queue.go             # Queue status/management handlers
│   │   ├── api_test.go
│   │   └── queue_test.go
│   │
│   ├── daemon/                  # Daemon lifecycle (2 files)
│   │   ├── runner.go            # Daemon runner with lifecycle management
│   │   └── runner_test.go
│   │
│   ├── extl/                    # JavaScript extension engine (14 files)
│   │   ├── engine.go            # Goja runtime manager
│   │   ├── extension.go         # Extension struct
│   │   ├── extract.go           # URL extraction logic
│   │   ├── install.go           # Extension installation
│   │   ├── list.go              # List installed extensions
│   │   ├── load.go              # Load extension from disk
│   │   ├── delete.go            # Delete extension
│   │   ├── activate.go          # Activate/deactivate
│   │   ├── README.md            # Extension engine documentation
│   │   ├── *_test.go            # Extension tests
│   │   └── testdata/            # Test fixtures
│   │
│   ├── nativehost/              # Browser extension native messaging (12 files)
│   │   ├── handler.go           # Message handling
│   │   ├── install_*.go         # Platform-specific install logic
│   │   ├── message.go           # Message types
│   │   ├── protocol.go          # Protocol implementation
│   │   └── *_test.go
│   │
│   └── service/                 # Windows service integration (8 files)
│       ├── service.go           # Windows service wrapper
│       ├── *_windows.go         # Windows-only implementations
│       └── *_test.go
│
├── common/                       # Shared types and constants (8 files)
│   ├── types.go                 # UpdateType enum, DownloadParams, etc.
│   ├── constants.go             # Global constants (timeouts, env vars)
│   ├── errors.go                # Common error types
│   └── *_test.go
│
├── main.go                       # Binary entry point (39 lines)
├── main_test.go
│
├── go.mod                        # Module definition
├── go.sum                        # Dependency checksums
│
├── Makefile                      # Build targets
├── .goreleaser.yml              # Release configuration
│
├── docs/                         # User documentation
│   ├── src/content/docs/
│   │   ├── api/                 # API documentation
│   │   ├── configuration/       # Configuration guides
│   │   ├── development/         # Development guides
│   │   ├── getting-started/     # User guides
│   │   ├── troubleshooting/     # Troubleshooting docs
│   │   └── usage/               # Usage examples
│   └── scripts/                 # Doc build scripts
│
├── specs/                        # Feature specifications
│   ├── issue-129/               # Feature spec files
│   ├── issue-135/
│   └── issue-136/
│
├── debug/                        # Debug utilities
│   └── extl/                     # Extension debugging tools
│
├── tests/                        # Integration tests
│   └── e2e/                      # End-to-end tests
│
├── scripts/                      # Build and utility scripts
│   ├── check_coverage.sh        # Code coverage verification
│   ├── release_*.sh             # Release scripts
│   └── *.sh                     # Various utilities
│
├── .github/                      # GitHub Actions CI/CD
│   └── workflows/               # GitHub Actions workflows
│
├── docker/                       # Docker-related files
│   └── Dockerfile
│
├── CLAUDE.md                     # Agent instructions (this repo)
├── AGENTS.md                     # Symlink to CLAUDE.md
├── CONTRIBUTING.md              # Contribution guidelines
├── CODE_OF_CONDUCT.md
├── LICENSE
└── README.md
```

## Directory Purposes

**`cmd/`:**
- Purpose: CLI command implementations using urfave/cli framework
- Contains: 61 .go files including command handlers, platform-specific code, and tests
- Key files: cmd.go (CLI app definition), download.go, resume.go, daemon.go
- Test files: 20+ test files covering command logic

**`pkg/warplib/`:**
- Purpose: Core download engine and state management
- Contains: 50+ files implementing download, resumption, queuing, checksums, work stealing
- Key files: manager.go (state persistence), dloader.go (parallel downloads), queue.go
- Critical: This is the heart of the application

**`pkg/warpcli/`:**
- Purpose: Client library for communicating with daemon
- Contains: Connection management, request marshaling, platform-specific IPC dial
- Entry point for CLI commands to reach daemon

**`pkg/credman/`:**
- Purpose: Secure credential storage with OS keyring fallback
- Contains: Keyring integration (macOS/Linux/Windows), encryption, file-based storage
- Dependencies: github.com/zalando/go-keyring

**`internal/server/`:**
- Purpose: IPC server accepting daemon client connections
- Contains: Listener creation, connection handling, connection pooling, web server
- Platform-specific: Unix socket, named pipe, TCP fallback

**`internal/api/`:**
- Purpose: Request handlers for all daemon operations
- Contains: 17 handler files (download, resume, attach, queue management, extensions)
- Pattern: Each handler validates input, delegates to Manager/Engine, returns response

**`internal/daemon/`:**
- Purpose: Daemon process lifecycle management
- Contains: Runner with start/stop/shutdown logic, graceful shutdown coordination
- Used by: cmd/daemon command

**`internal/extl/`:**
- Purpose: JavaScript extension engine using Goja runtime
- Contains: Extension loading, URL extraction, activation/deactivation
- Dependencies: github.com/dop251/goja

**`internal/nativehost/`:**
- Purpose: Browser extension native messaging protocol
- Contains: Message handling, installation, protocol implementation
- Used by: Browser extensions via stdin/stdout

**`common/`:**
- Purpose: Shared types and constants across all packages
- Contains: UpdateType enum (method names), DownloadParams, error types, constants
- Used by: CLI, client, server, and API layers

## Key File Locations

**Entry Points:**
- `main.go`: Binary entry point - delegates to cmd.Execute
- `cmd/cmd.go - Execute()`: CLI app initialization with all commands
- `cmd/daemon.go - getDaemonAction()`: Daemon startup
- `internal/server/server.go - Start()`: Server listener startup
- `internal/api/api.go - RegisterHandlers()`: API handler registration

**Configuration & Persistence:**
- `~/.config/warpdl/userdata.warp`: GOB-encoded Manager state (created by manager.go)
- `~/.config/warpdl/extstore/`: Extension storage directory
- `~/.config/warpdl/cookies.json`: HTTP cookie jar (if configured)
- `/tmp/warpdl.sock` or `/run/warpdl.sock`: Unix socket (Unix/Linux/macOS)
- `\\.\pipe\warpdl`: Named pipe (Windows)
- `localhost:3849`: TCP fallback (if primary IPC fails)

**Core Logic:**
- `pkg/warplib/manager.go`: Download state management and persistence
- `pkg/warplib/dloader.go`: Parallel segment downloader (38KB - complex)
- `pkg/warplib/queue.go`: Concurrent download queue
- `pkg/warplib/handlers.go`: Event callback types for download lifecycle
- `pkg/warplib/checksum.go`: Checksum validation for downloaded files
- `pkg/warplib/speed.go`: Speed limiting and work stealing implementation

**Testing:**
- `pkg/warplib/*_test.go`: Unit tests for download engine (25+ files)
- `pkg/warplib/*_integration_test.go`: Integration tests
- `cmd/*_test.go`: Command handler tests
- `internal/api/*_test.go`: API handler tests
- `scripts/check_coverage.sh`: Coverage verification script

**API Handlers:**
- `internal/api/download.go`: Process download requests
- `internal/api/resume.go`: Resume incomplete downloads
- `internal/api/attach.go`: Stream progress to client
- `internal/api/list.go`: Query download history
- `internal/api/queue.go`: Queue status and manipulation
- `internal/api/add_ext.go`, `get_ext.go`, `list_ext.go`, `delete_ext.go`: Extension management

## Naming Conventions

**Files:**
- `*.go`: Go source files
- `*_test.go`: Unit tests
- `*_integration_test.go`: Integration tests
- `*_unix.go`, `*_windows.go`: Platform-specific implementations
- `*_unix_test.go`, `*_windows_test.go`: Platform-specific tests
- `*.pb.go`: Protocol buffer generated files (if used)

**Directories:**
- `cmd/`: CLI commands (imperative)
- `pkg/`: Public packages (library code)
- `internal/`: Private packages (internal only)
- `common/`: Shared types and constants
- `docs/`: User-facing documentation
- `specs/`: Feature specifications
- `tests/`: Integration/E2E tests
- `scripts/`: Build and utility scripts

**Go Package Organization:**
- `package main`: Only in main.go
- `package cmd`: cmd/ and cmd/ext/, cmd/nativehost/
- `package warplib`: pkg/warplib/
- `package warpcli`: pkg/warpcli/
- `package credman`: pkg/credman/
- `package api`: internal/api/
- `package server`: internal/server/
- `package daemon`: internal/daemon/
- `package extl`: internal/extl/
- `package nativehost`: internal/nativehost/
- `package common`: common/

**Functions & Types:**
- `CamelCase`: Public exported names (e.g., `NewDownloader`, `Manager`, `Item`)
- `camelCase`: Private unexported names (e.g., `newClient`, `handleConnection`)
- `ALLCAPS`: Constants (e.g., `DefaultFileMode`, `ShutdownTimeout`)
- Handler suffixes: `Handler`, `Func` (e.g., `DownloadProgressHandlerFunc`, `downloadHandler`)
- Constructor prefix: `New` (e.g., `NewClient`, `NewDownloader`, `NewManager`)

**Error Variables:**
- `Err*` prefix: Sentinel errors (e.g., `ErrDownloadNotFound`, `ErrChecksumMismatch`)
- Exported from `pkg/warplib/errors.go`

## Where to Add New Code

**New Download Feature (e.g., speed limiting, retry logic):**
- Primary code: `pkg/warplib/` - core engine
- Tests: `pkg/warplib/*_test.go` - unit tests, place alongside feature
- Integration tests: `pkg/warplib/*_integration_test.go` if testing against real network
- API changes if needed: `internal/api/download.go` to expose new parameters

**New CLI Command (e.g., `warpdl config`):**
- Implementation: `cmd/config.go` (create new file following cmd/*.go pattern)
- Tests: `cmd/config_test.go`
- Registration: Add to `commands` slice in `cmd/cmd.go - GetApp()`
- Flags: Add to `configFlags` slice if command has flags
- Handler: Function named `config` following existing patterns

**New Extension API Feature:**
- Handler: `internal/api/<feature>.go` (create new file, pattern: `*Handler` and `*Func` types)
- Tests: `internal/api/<feature>_test.go`
- Registration: Call in `api.go - RegisterHandlers()` with `server.RegisterHandler()`
- Type definition: Add to `common/types.go` if new request/response types needed

**New Utility Package (shared code):**
- Location: `pkg/<name>/` if public, `internal/<name>/` if private
- Pattern: Follow existing package structure with constructor functions
- Tests: Collocated `*_test.go` files in same directory

**Platform-Specific Code:**
- Pattern: Base file + `_unix.go` and `_windows.go` variants
- Examples: `pkg/warplib/file.go` with `file_unix.go`, `file_windows.go`
- Tests: `*_unix_test.go`, `*_windows_test.go` in same package

## Special Directories

**`pkg/warplib/` (Core Engine):**
- Purpose: Download implementation, state management, concurrency control
- Generated: No generated files
- Committed: Yes, all files committed
- Size: 50+ files, ~3K LOC per file average
- Test coverage: 80%+ (enforced by CI)

**`~/.config/warpdl/` (Runtime State):**
- Purpose: User-specific configuration and download state
- Generated: Yes, created at runtime
- Committed: No, git-ignored
- Contents:
  - `userdata.warp`: GOB-encoded Manager state
  - `extstore/`: Installed extensions
  - `cookies.json`: HTTP cookie jar
  - `credentials.json`: Encrypted credentials (fallback if keyring unavailable)

**`internal/extl/` (Extension Engine):**
- Purpose: JavaScript runtime for URL transformations
- Generated: No
- Committed: Yes, all extension engine code
- Dependencies: github.com/dop251/goja (JavaScript interpreter)

**`.github/workflows/` (CI/CD):**
- Purpose: GitHub Actions automation
- Generated: No
- Committed: Yes

**`docs/` (User Documentation):**
- Purpose: Public-facing documentation (API, usage, configuration)
- Generated: Possibly (Astro/static site generation)
- Committed: Yes, source committed

---

*Structure analysis: 2026-02-26*
