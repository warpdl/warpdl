# Phase 2: Protocol Interface - Context

**Gathered:** 2026-02-27
**Status:** Ready for planning

<domain>
## Phase Boundary

Extract a protocol-agnostic downloader interface from the existing HTTP-only download engine so that FTP and SFTP backends can plug in alongside HTTP without modifying the manager or API layers. This phase delivers the structural abstraction only — no new protocol implementations (those are Phases 3 and 4).

Requirements covered: PROTO-01, PROTO-02, PROTO-03.

</domain>

<decisions>
## Implementation Decisions

### Interface contract
- Separate `Probe` method for metadata fetching (file size, content type, checksum headers) before download starts — manager calls Probe first, then Download
- Single `Downloader` interface with a `Capabilities()` method reporting whether the protocol supports parallel segments, resume, etc. — no separate ParallelDownloader/StreamDownloader interfaces
- Each protocol downloader manages its own connection lifecycle internally (create, reuse, close) — manager just calls Download/Close
- Shared handler/callback contract: all protocols accept the same handler callbacks (progress, completion, error) that HTTP uses today — manager and UI code stays protocol-agnostic

### Error semantics
- Generic `DownloadError` wrapping protocol-specific errors — callers see uniform type but can `errors.Unwrap`/`errors.As` to get FTP/SFTP-specific details when needed
- Manager owns retry logic with exponential backoff, applies uniformly to all protocols — downloaders just report transient vs permanent errors
- Errors implement `IsTransient() bool` method — manager checks this to decide whether to retry; each protocol classifies its own errors (e.g., FTP 421 = transient, 530 = permanent)
- Authentication failures fail immediately with a clear "authentication failed" message — no interactive re-prompts mid-download (daemon architecture constraint)

### Registration & routing
- Static scheme-to-downloader map built at startup (`map[string]DownloaderFactory`) — no dynamic registry, no self-registration
- Unsupported schemes fail immediately with error listing supported schemes: `unsupported scheme "magnet" — supported: http, https, ftp, ftps, sftp`
- `http://` and `https://` share the same HTTPDownloader; `ftp://` and `ftps://` share the same FTPDownloader — TLS handled at transport level, not downloader level
- Scheme router lives inside `pkg/warplib` as part of the manager package — not a separate package

### GOB migration strategy
- New `Protocol` field added to `Item` struct — zero value (`0`) defaults to HTTP, no migration step needed
- Protocol field is a typed enum (`uint8` with iota constants): `ProtoHTTP = 0`, `ProtoFTP = 1`, `ProtoFTPS = 2`, `ProtoSFTP = 3` — zero value gives free backward compat
- Golden fixture test: capture a GOB-encoded `userdata.warp` from the current version as a test fixture, decode it, assert all items load with `Protocol == ProtoHTTP`
- Unknown protocol values (from newer versions) fail with clear error: `unknown protocol type 7 — upgrade warpdl` — no silent degradation

### Claude's Discretion
- Exact method signatures and parameter types on the Downloader interface
- Internal structure of the capability flags (bitfield vs struct vs method set)
- How the existing HTTP download code gets refactored to implement the new interface
- Test helper patterns for mock downloaders

</decisions>

<specifics>
## Specific Ideas

- The capability flags approach means the manager can query `downloader.Capabilities().SupportsParallel` before deciding whether to spawn multiple segments — single-stream protocols just return false
- The IsTransient() pattern follows Go standard library conventions (similar to `net.Error` interface)
- Golden fixture ensures real-world backward compat, not just synthetic test data

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>

---

*Phase: 02-protocol-interface*
*Context gathered: 2026-02-27*
