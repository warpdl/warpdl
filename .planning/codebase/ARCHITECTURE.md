# Architecture

**Analysis Date:** 2026-02-26

## Pattern Overview

**Overall:** Daemon-based service architecture with CLI client-server communication over IPC.

**Key Characteristics:**
- Single daemon process (`cmd/daemon.go` entry point) serves multiple CLI clients
- Platform-specific IPC transport: Unix socket/named pipe with TCP fallback
- Handler pattern for event-driven download lifecycle (progress, completion, errors)
- Manager pattern centralizes download state persistence and coordination
- Separation of concerns: CLI commands → warpcli (client) → server → API → warplib (core engine)

## Layers

**CLI Layer (Command Interface):**
- Purpose: Accept user commands and delegate to daemon or execute locally
- Location: `cmd/` directory (61 files)
- Contains: Command handlers (download, resume, list, attach, stop, daemon, ext, nativehost)
- Depends on: pkg/warpcli (client), pkg/warplib (for manager initialization in daemon)
- Used by: End users via terminal

**Client Communication Layer:**
- Purpose: Establish connections to daemon and manage RPC-style requests
- Location: `pkg/warpcli/` - client.go, dispatcher.go
- Contains: Client connection pool, request marshaling/unmarshaling
- Depends on: internal/server (connection protocol), common/ (message types)
- Used by: cmd/ commands to communicate with daemon

**Server Layer:**
- Purpose: Accept IPC connections and dispatch requests to API handlers
- Location: `internal/server/` - server.go, connection.go, pool.go, webserver.go
- Contains: Listener management (Unix socket/named pipe/TCP), connection pooling, WebServer on alternate port
- Depends on: internal/api (handlers), pkg/warplib (Manager)
- Used by: Daemon process to handle CLI requests

**API Handler Layer:**
- Purpose: Implement business logic for download/extension operations
- Location: `internal/api/` - 17 handler files (download.go, resume.go, attach.go, list.go, etc.)
- Contains: Handler functions that coordinate between Manager and request parameters
- Depends on: pkg/warplib (Manager, Downloader), internal/extl (Extension engine), pkg/credman (credentials)
- Used by: internal/server to process client requests

**Download Engine Layer (Core):**
- Purpose: Manage download process including segmentation, progress tracking, retry logic
- Location: `pkg/warplib/` - 50+ files covering download lifecycle
- Contains:
  - `manager.go`: Centralized download item persistence (GOB-encoded to `~/.config/warpdl/userdata.warp`)
  - `dloader.go`: Parallel segment downloader with HTTP range requests
  - `item.go`: Download item and part state structures
  - `queue.go`: Concurrent download queue with priority support
  - `handlers.go`: Callback function types for progress events
  - `checksum.go`: Automatic checksum validation (MD5, SHA1, SHA256, SHA512)
  - `speed.go`: Speed limiting and work stealing logic
  - `diskspace_*.go`: Platform-specific disk space validation
- Depends on: pkg/credman (credentials), pkg/logger (logging), common/ (types)
- Used by: internal/api, cmd (for direct daemon management)

**Extension Engine Layer:**
- Purpose: Execute JavaScript extensions for URL transformation and advanced features
- Location: `internal/extl/` - Goja runtime integration (14 directories)
- Contains: JavaScript engine, extension state management, extension installation/activation
- Depends on: pkg/warplib (for integration with downloads), github.com/dop251/goja (JS runtime)
- Used by: API handlers during URL extraction and download processing

**Daemon Lifecycle Layer:**
- Purpose: Manage daemon startup, shutdown, and platform-specific service integration
- Location: `internal/daemon/` - runner.go
- Contains: Lifecycle management, graceful shutdown, Windows service integration
- Depends on: internal/server (Server instance), internal/api (API handlers), pkg/warplib (Manager)
- Used by: cmd/daemon command

**Credential Management Layer:**
- Purpose: Secure credential storage with OS keyring integration
- Location: `pkg/credman/` - credman.go, keyring/*.go, encryption/*.go
- Contains: Keyring abstraction (macOS/Linux/Windows), file-based fallback, encryption
- Depends on: github.com/zalando/go-keyring (OS credential access)
- Used by: API handlers for HTTP authentication

**Browser Integration Layer:**
- Purpose: Native messaging host for browser extensions
- Location: `internal/nativehost/` and `cmd/nativehost/` - install.go, run.go, uninstall.go, status.go
- Contains: Browser native messaging protocol implementation
- Depends on: pkg/warpcli (daemon communication), pkg/warplib (Manager)
- Used by: Browser extensions via stdin/stdout

## Data Flow

**Download Request:**

1. User runs `warpdl download <url>`
2. `cmd/download.go` handler is invoked (CLI layer)
3. Handler calls `pkg/warpcli.NewClient()` to connect to daemon (client layer)
4. Client establishes IPC connection to daemon (Unix socket/named pipe/TCP)
5. `internal/server.handleConnection()` receives request (server layer)
6. Server calls registered `downloadHandler` in `internal/api/download.go` (API layer)
7. Handler creates `warplib.Downloader` instance with options (download engine)
8. Manager registers handlers from `pkg/warplib/handlers.go` for progress events
9. Downloader starts parallel segment downloads via `dloader.go`
10. Progress callbacks trigger RPC updates to CLI client
11. Client displays progress to user
12. On completion, Manager persists state to `~/.config/warpdl/userdata.warp` (GOB format)

**Resume Request:**

1. User runs `warpdl resume <hash>`
2. `cmd/resume.go` handler queries Manager for Item state
3. API handler validates item is resumable
4. Creates new Downloader with existing Item.Parts offsets
5. Resumes partial downloads using HTTP Range headers
6. Re-uses existing Item.Downloaded progress

**Queue Processing:**

1. If max concurrent downloads reached (default: 3), new downloads queued
2. `pkg/warplib.QueueManager` manages waiting items by priority
3. When active slot freed, next waiting item activated
4. Queue state persisted in `ManagerData.QueueState` for daemon restarts

**Extension Processing:**

1. API handler calls `internal/extl.Engine.Extract(url)`
2. Engine runs activated JavaScript extensions
3. Extensions can transform URL before download begins
4. Falls back to original URL if extraction fails

**State Persistence:**

```
Manager Instance
  ├── ItemsMap (map[hash]*Item)
  │   └── Item
  │       ├── Parts (map[offset]*ItemPart)
  │       └── dAlloc (*Downloader) - transient, not persisted
  └── QueueState
      ├── MaxConcurrent
      ├── Waiting ([]QueuedItemState)
      └── Paused (bool)
        ↓
    GOB Encoded to ~/.config/warpdl/userdata.warp
```

## Key Abstractions

**Downloader:**
- Purpose: Manages download of a single file with parallel segments
- Location: `pkg/warplib/dloader.go`
- Responsibilities: HTTP requests, segment coordination, progress tracking, retry logic, checksum validation, speed limiting, work stealing
- Pattern: Constructor with `DownloaderOpts`, Start/Resume/Stop lifecycle methods, Handler callbacks

**Manager:**
- Purpose: Centralized download state management
- Location: `pkg/warplib/manager.go`
- Responsibilities: Persist/restore Item state, coordinate with Downloaders, handle resume requests
- Pattern: Singleton-like, file-backed (GOB encoding), RWMutex protected access

**Item/ItemPart:**
- Purpose: Represent download metadata and progress
- Location: `pkg/warplib/item.go`
- Pattern: Serializable structures for persistence, organized by hash and byte offsets

**Handlers:**
- Purpose: Event callbacks for download lifecycle
- Location: `pkg/warplib/handlers.go`
- Pattern: Function types (ErrorHandlerFunc, SpawnPartHandlerFunc, etc.) registered on Downloader instance
- Examples: Progress updates, error notifications, completion, checksum validation, work stealing events

**QueueManager:**
- Purpose: Limit concurrent downloads
- Location: `pkg/warplib/queue.go`
- Responsibilities: Track active/waiting items, enforce max concurrency, priority-based queueing, persistence
- Pattern: Priority queue with onStart callback for activation

**Api:**
- Purpose: Coordinate request handling between server and manager
- Location: `internal/api/api.go`
- Responsibilities: Register handlers, delegate to download/extension operations
- Pattern: Dependency injection of Manager, Logger, HTTP Client, Extension Engine

**Server:**
- Purpose: IPC listener and connection management
- Location: `internal/server/server.go`
- Responsibilities: Accept connections, dispatch to handlers, manage connection pool
- Pattern: Platform-specific listener factory (Unix socket/named pipe/TCP), handler registry

**Client:**
- Purpose: Daemon communication from CLI
- Location: `pkg/warpcli/client.go`
- Responsibilities: Connect to daemon, send requests, receive updates
- Pattern: Auto-spawn daemon if not running, platform-specific dial (Unix socket/named pipe/TCP)

## Entry Points

**Binary Entry Point:**
- Location: `main.go`
- Triggers: `go run . <args>` or `./warpdl <command>`
- Responsibilities: Parse build args (version, commit, date), delegate to cmd.Execute

**CLI Application Entry:**
- Location: `cmd/cmd.go - Execute()` and `GetApp()`
- Triggers: main() → Execute() with command-line arguments
- Responsibilities: Build urfave/cli App with all commands, dispatch to command handlers

**Daemon Entry Point:**
- Location: `cmd/daemon.go - getDaemonAction()`
- Triggers: `warpdl daemon` or auto-spawned by client
- Responsibilities: Initialize Manager, Server, API, start listening for IPC connections

**API Handler Entry Points:**
- Location: `internal/api/*.go` - 17 handlers
- Examples:
  - `internal/api/download.go - downloadHandler()`: Process download requests
  - `internal/api/resume.go - resumeHandler()`: Resume incomplete downloads
  - `internal/api/list.go - listHandler()`: Query download history
  - `internal/api/attach.go - attachHandler()`: Stream download progress
  - `internal/api/queue.go - queueStatusHandler()`: Query queue state
  - `internal/api/add_ext.go - addExtHandler()`: Install extensions

**Server Connection Handler:**
- Location: `internal/server/server.go - handleConnection()`
- Triggers: New IPC client connection
- Responsibilities: Read JSON-RPC requests, invoke registered handlers, send responses

## Error Handling

**Strategy:** Explicit error returns with specific error types; context-aware error messages

**Patterns:**

1. **Sentinel Errors** (warplib):
   - Defined in `pkg/warplib/errors.go`
   - Examples: `ErrFileNameNotFound`, `ErrDownloadNotFound`, `ErrInsufficientDiskSpace`, `ErrChecksumMismatch`
   - Used for: Programmatic error checking, user-friendly messages

2. **Wrapped Errors** (API layer):
   - Use `fmt.Errorf("context: %w", err)` for error context
   - Location: `internal/api/*.go` handlers
   - Examples: `"invalid proxy URL: %w"`, `"failed to connect to daemon: %w"`

3. **Retry Logic** (Downloader):
   - Transient error detection in `pkg/warplib/dloader.go`
   - Retry configuration: `RetryConfig` struct with MaxRetries, BaseDelay, BackoffFactor
   - Callback: `RetryHandler` and `RetryExhaustedHandler` in Handlers
   - HTTP status codes 408, 429, 500, 502, 503, 504 are retried

4. **Validation Errors** (Download initialization):
   - Path validation: `dir_validation.go` (directory exists, writable)
   - Disk space check: `diskspace_*.go` (platform-specific)
   - Content length: `clength.go` (positive, supports ranges)
   - Checksum validation: `checksum.go` (hash computation post-download)

5. **Graceful Shutdown**:
   - Location: `internal/daemon/runner.go`, `internal/server/server.go`
   - Pattern: Context cancellation, timeout-based shutdown (common.ShutdownTimeout)

## Cross-Cutting Concerns

**Logging:**
- Framework: Go standard `log` package with custom wrapper
- Location: `pkg/logger/` (not heavily used; mostly direct `log.Logger` instances)
- Pattern: Passed to constructors (Server, Manager, API), controlled by `WARPDL_DEBUG=1` env var
- Debug mode: Enabled via `--debug/-d` flag, propagated via `common.DebugEnv`

**Validation:**
- Download path: `pkg/warplib/dir_validation.go`
- Disk space: `pkg/warplib/diskspace_*.go` (Unix: Statfs, Windows: GetDiskFreeSpaceEx)
- Item parts: `pkg/warplib/item.go - ValidateItemParts()`
- Directory writable: Checked during Downloader initialization

**Authentication & Cookies:**
- Cookie jar: Stored in HTTP Client, persisted to `~/.config/warpdl/cookies.json`
- Keyring credentials: Managed by `pkg/credman/` for stored authentication
- Request headers: Passed via `common.DownloadParams.Headers`
- Cookie CLI flag: `--cookie` (repeatable) in cmd/download.go, cmd/resume.go

**Platform-Specific Code:**
- User agent: `cmd/uagent_darwin.go`, `cmd/uagent_linux.go` - platform-specific user strings
- IPC transport: `internal/server/listener_*.go` (Unix socket, named pipe, TCP)
- Disk space: `pkg/warplib/diskspace_unix.go`, `diskspace_windows.go`
- Daemon lifecycle: `cmd/daemon_windows.go`, `cmd/service_windows.go`, `cmd/daemon_pidfile_unix.go`
- File operations: `pkg/warplib/file_unix.go`, `file_windows.go` (chmod, permissions)
- Tests: `*_unix_test.go`, `*_windows_test.go` naming convention

**Concurrency:**
- Download segments: Controlled by maxConnections, maxParts in Downloader
- Queue manager: Mutex-protected active/waiting maps
- Item state: RWMutex on Item struct
- Work stealing: Lock-free offset range updates between parts

---

*Architecture analysis: 2026-02-26*
