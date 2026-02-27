# Project Research Summary

**Project:** WarpDL Protocol Expansion (FTP/SFTP/HTTP Redirects/JSON-RPC 2.0)
**Domain:** Daemon-based Go download manager — protocol and API surface expansion
**Researched:** 2026-02-26
**Confidence:** HIGH

## Executive Summary

WarpDL is a mature daemon-based download manager with a clean layered architecture. This milestone adds four additive features: HTTP redirect following (#144), FTP/FTPS support (#138), SFTP support (#139), and a JSON-RPC 2.0 API (#137). The existing architecture is well-suited for this expansion — the core download engine (`pkg/warplib`), daemon server (`internal/server`), and API handler layer (`internal/api`) each have clear boundaries. The recommended approach is to introduce a `DownloaderI` interface as the central abstraction, route URL schemes through a new protocol router (`protocol.go`), and implement FTP and SFTP as single-stream downloaders that satisfy that interface. HTTP redirect fixing is a 2-line change. JSON-RPC sits atop the existing secondary HTTP server.

The implementation order is dictated by dependency: redirect fix first (no dependencies, unblocks all other features), protocol interface abstraction second (prerequisite for FTP/SFTP), FTP/FTPS third, SFTP fourth, JSON-RPC fifth (independent of FTP/SFTP but highest UX impact). All five work items are well-understood with mature, verified libraries — no speculative architecture decisions are needed.

The primary risks are security failures that ship silently: FTP credentials stored in plaintext URLs, SFTP using `InsecureIgnoreHostKey()`, JSON-RPC WebSocket endpoints lacking auth (CSRF-exploitable from any browser tab), and FTPS disabling TLS verification. Every one of these has real-world CVEs as precedents. These are not theoretical — they must be caught in code review gates before the implementation phase ships.

---

## Key Findings

### Recommended Stack

All library decisions are HIGH confidence — verified against official pkg.go.dev documentation and GitHub release history. The stack is minimal: three new direct dependencies for protocols, two for JSON-RPC, one stdlib-only fix for redirects.

**Core technologies:**
- `github.com/jlaffaye/ftp` v0.2.0 — FTP/FTPS client. Only serious Go FTP client (1.4k stars, active through Oct 2025), supports TLS, EPSV passive mode, and `RetrFrom(path, offset)` for resume.
- `golang.org/x/crypto/ssh` v0.48.0 — SSH transport for SFTP. Official Go extended stdlib. v0.48.0 patches all known CVEs (GO-2025-4134, GO-2025-4135 — unbounded memory and DoS).
- `github.com/pkg/sftp` v1.13.10 — SFTP protocol over SSH. De-facto standard. `File.Seek(offset, io.SeekStart)` for resume; `WriteTo(w)` for optimized streaming.
- `github.com/creachadair/jrpc2` v1.3.4 — JSON-RPC 2.0 server. v1-stable API, `jhttp` subpackage for HTTP transport. Most actively maintained Go JSON-RPC 2.0 library.
- `github.com/coder/websocket` v1.8.14 — WebSocket transport for JSON-RPC. Zero dependencies, context-aware, `wsjson` subpackage. Gorilla/websocket is archived (Dec 2022) — do not use.
- `net/http` stdlib — HTTP redirect following. Zero new dependencies. Go's `http.Client` already follows up to 10 redirects; the fix is capturing the resolved URL post-redirect.

**Do not use:** `gorilla/websocket` (archived), `net/rpc/jsonrpc` (JSON-RPC 1.0 only), `golang.org/x/crypto` < v0.45.0 (CVE), `InsecureIgnoreHostKey()` in production, `pkg/sftp/v2` (alpha, unstable API).

### Expected Features

**Must have (table stakes — this milestone):**
- HTTP redirect following (301/302/303/307/308) with final URL capture — users expect this for any CDN-hosted or URL-shortened download
- FTP single-stream download with resume via `REST`/`RetrFrom` — core issue #138
- FTPS explicit TLS (STARTTLS) — required by most institutional FTP servers
- SFTP with password auth and private key file auth — core issue #139; key auth required since many server admins disable passwords
- SFTP TOFU (Trust-On-First-Use) host key policy — security non-negotiable; must be user-friendly
- JSON-RPC 2.0 HTTP + WebSocket endpoint with auth token — core issue #137
- JSON-RPC method subset: `warpdl.addUri`, `warpdl.resume`, `warpdl.stop`, `warpdl.tellStatus`, `warpdl.tellActive`, `warpdl.tellStopped`, `warpdl.getVersion`
- JSON-RPC WebSocket push notifications: `onDownloadStart`, `onDownloadProgress`, `onDownloadComplete`, `onDownloadError`

**Should have (differentiators — v1.x):**
- SFTP SSH agent integration — common request, blocked by platform complexity
- FTP keep-alive NoOp pings — edge case for servers with short idle timeouts
- JSON-RPC `system.listMethods` and `getGlobalStat` — discoverability and monitoring
- FTPS implicit TLS (port 990) — rare legacy requirement

**Defer (v2+):**
- FTP and SFTP recursive/directory download — major scope expansion
- JSON-RPC batch requests / `system.multicall` — rarely used in practice, high complexity
- AriaNG web UI bundling — downstream concern, not WarpDL's responsibility

**Hard anti-features (never):**
- Parallel segments over FTP/SFTP — not a protocol limitation to work around; accept it
- FTP active mode — breaks behind NAT for nearly all users
- JSON-RPC remote access by default (`0.0.0.0`) — exposes attack surface; explicit opt-in only
- Cross-protocol redirect following (HTTP→FTP) — security risk (open redirect), RFC-prohibited

### Architecture Approach

The existing architecture is layered cleanly: `cmd/` → `pkg/warpcli` → IPC socket → `internal/server` → `internal/api` → `pkg/warplib`. New features integrate at two layers only: the download engine (`pkg/warplib`) for FTP/SFTP/redirect, and the server layer (`internal/server`) for JSON-RPC. The CLI layer and IPC protocol require zero changes. The Manager, handlers, and connection pool remain untouched.

**Major components (new/modified):**
1. `pkg/warplib/protocol.go` — `DownloaderI` interface + URL scheme router (`ftp://`, `ftps://`, `sftp://`, `http://`, `https://` → correct downloader). Prerequisite for FTP and SFTP.
2. `pkg/warplib/ftp_dloader.go` — Single-stream FTP/FTPS downloader implementing `DownloaderI`. No parallel segments.
3. `pkg/warplib/sftp_dloader.go` — Single-stream SFTP downloader implementing `DownloaderI`. TOFU host key policy.
4. `pkg/warplib/dloader.go` (modify) — Capture `resp.Request.URL.String()` after `fetchInfo()` GET for redirect URL resolution. 2-line change.
5. `internal/server/jsonrpc.go` — JSON-RPC 2.0 request handler (HTTP POST + WebSocket upgrade) on existing `port+1` server.
6. `internal/api/jsonrpc_handler.go` — Thin adapter mapping JSON-RPC method names to existing `Api` handler methods. Zero business logic duplication.

**Unchanged:** `pkg/warpcli/`, `cmd/`, `internal/server/server.go`, `pkg/warplib/manager.go`, `pkg/warplib/handlers.go`.

### Critical Pitfalls

1. **FTP `ServerConn` is not goroutine-safe** — do not attempt parallel segments; `jlaffaye/ftp` explicitly documents concurrent calls cause panics. Enforce `maxConnections = 1` for FTP at the type level, not as a config option.

2. **CVE-2024-45336: Authorization header re-sent across cross-domain redirects** — Go < 1.22.11/1.24 had this bug. WarpDL is on 1.24.9 (safe), but any custom `CheckRedirect` that manually copies headers will reintroduce it. Use default `CheckRedirect` (nil) and capture the resolved URL only.

3. **JSON-RPC WebSocket CSRF from browser** — localhost binding does NOT prevent browser tab attacks. Any webpage can open `ws://localhost:<port>`. Auth token required on every request including WebSocket upgrades. This is not theoretical — there's a real CVE (CVE-2025-52882) for this exact pattern.

4. **SFTP `InsecureIgnoreHostKey()` in production** — `golang.org/x/crypto/ssh` requires `HostKeyCallback` to be set; developers set `InsecureIgnoreHostKey()` to silence the error. This disables all MITM protection. Use `knownhosts.New()` or TOFU. Add a CI grep gate: any occurrence of `InsecureIgnoreHostKey` outside `_test.go` fails the build.

5. **GOB deserialization breaks if `Item` struct changes incorrectly** — new fields on `Item` must have zero values that mean "HTTP/existing behavior." If a `Protocol` enum is added, `HTTP = iota` (value 0), not `FTP = iota`. Write a migration fixture test before merging any `item.go` changes.

6. **FTP/SFTP credentials in stored URL** — `Item.Url` persisted to `~/.config/warpdl/userdata.warp`. Strip credentials from URL before storage; use `credman` for persistence if needed.

---

## Implications for Roadmap

Based on research, the dependency graph dictates 5 phases with a clear build order. Phases 1 and 2 are prerequisites; phases 3, 4, and 5 can be parallelized after phase 2 completes.

### Phase 1: HTTP Redirect Following
**Rationale:** Zero new dependencies, zero regression risk, 2-line code change. Unblocks every other feature (FTP URLs from HTTP pages, JSON-RPC URLs via addUri may redirect). Ships first with near-zero implementation cost.
**Delivers:** HTTP/HTTPS downloads now follow 301/302/303/307/308 redirect chains; final URL is captured post-redirect for correct resume and display.
**Addresses:** Issue #144; table-stakes redirect feature for CDN/URL-shortener downloads.
**Avoids:** CVE-2024-45336 (use default `CheckRedirect`, do not manually copy headers).
**No research-phase needed** — all patterns are stdlib, documented, and well-understood.

### Phase 2: Protocol Interface Abstraction
**Rationale:** FTP and SFTP cannot be added without this — `Manager.AddDownload` currently takes a concrete `*Downloader`. This is a mandatory structural prerequisite, not a feature. Must complete before phase 3 or 4.
**Delivers:** `DownloaderI` interface in `protocol.go`; existing HTTP `Downloader` satisfies it; `Manager.AddDownload` updated to accept interface; `NewDownloaderFromURL` factory added.
**Risk:** If the existing `Downloader` struct has unexported fields used by `Manager`, the interface surface needs careful scoping. LOW risk — interface methods are all public.
**No research-phase needed** — standard Go interface extraction pattern.

### Phase 3: FTP/FTPS Downloader
**Rationale:** Depends on phase 2. FTP is the more requested protocol (issue #138 predates SFTP). Library is well-documented with clear patterns.
**Delivers:** `ftp://` and `ftps://` URL downloads; single-stream with resume via `RetrFrom`; FTPS explicit TLS; credential extraction from URL userinfo.
**Uses:** `github.com/jlaffaye/ftp` v0.2.0.
**Implements:** `FTPDownloader` struct in `ftp_dloader.go` satisfying `DownloaderI`.
**Must avoid:** Goroutine-unsafe FTP connection sharing (pitfall 1), FTPS `InsecureSkipVerify` (pitfall 6), credentials in stored URL (security).
**No research-phase needed** — library docs are comprehensive, patterns are prescribed.

### Phase 4: SFTP Downloader
**Rationale:** Depends on phase 2. More complex than FTP due to SSH auth and host key verification design decision. TOFU policy must be designed before writing any code.
**Delivers:** `sftp://` URL downloads; single-stream with resume via `File.Seek`; password auth; private key file auth; TOFU host key policy with `~/.config/warpdl/known_hosts`.
**Uses:** `golang.org/x/crypto/ssh` v0.48.0, `github.com/pkg/sftp` v1.13.10.
**Implements:** `SFTPDownloader` struct in `sftp_dloader.go` satisfying `DownloaderI`.
**Must avoid:** `InsecureIgnoreHostKey()` (pitfall 4 — CI gate required), missing `known_hosts` file on first use (graceful TOFU prompt required).
**Design decision required before coding:** TOFU policy details — what to store, how to display fingerprint mismatch errors, how `--sftp-insecure` opt-in works.
**Needs research-phase consideration** — SFTP host key UX is a design-gate, not just implementation.

### Phase 5: JSON-RPC 2.0 API
**Rationale:** Independent of FTP/SFTP. Can be parallelized with phases 3/4. Highest external impact (enables AriaNG, third-party integrations, scripting). Placed last in serial ordering because JSON-RPC `addUri` becomes more useful after FTP/SFTP are available.
**Delivers:** JSON-RPC 2.0 endpoint on existing `port+1` HTTP server; HTTP POST and WebSocket transports; auth token (generated at daemon start, written to `~/.config/warpdl/rpc.token`); method suite: `warpdl.addUri`, `.resume`, `.stop`, `.tellStatus`, `.tellActive`, `.tellStopped`, `.getVersion`; WebSocket push notifications.
**Uses:** `github.com/creachadair/jrpc2` v1.3.4, `github.com/coder/websocket` v1.8.14.
**Implements:** `internal/server/jsonrpc.go` (handler + auth), `internal/api/jsonrpc_handler.go` (thin adapter).
**Must avoid:** CSRF via WebSocket without auth (pitfall 3), binding to `0.0.0.0` by default, duplicating `Api` business logic in JSON-RPC adapter (anti-pattern 2).
**No research-phase needed** — existing HTTP server, auth patterns, and adapter pattern are all well-documented.

### Phase Ordering Rationale

- Phase 1 is a prerequisite by user impact (every other protocol benefits from redirects) and zero-risk (no new dependencies).
- Phase 2 is a prerequisite by code structure — there is no way to add FTP/SFTP without it. It should be kept minimal (pure interface extraction, no behavior change).
- Phases 3, 4, and 5 are independent of each other after phase 2. If parallel development is available, they can proceed concurrently. Serial ordering puts FTP before SFTP (simpler protocol, lower security surface) and JSON-RPC last (highest integration testing surface).
- GOB compatibility (pitfall 5) must be verified in whichever phase first touches `item.go` or `manager.go` — most likely phase 2 or 3.

### Research Flags

Phases requiring deeper design work or research during planning:
- **Phase 4 (SFTP):** TOFU policy UX is a design-gate. Must decide: Where is the known_hosts file? What does the interactive prompt look like? How is `--sftp-insecure` exposed? Does it need a `warpdl sftp trust-host` subcommand? This needs to be specified before implementation begins, not discovered during it.

Phases with standard, well-documented patterns (skip research-phase):
- **Phase 1 (HTTP Redirects):** Pure stdlib, 2-line fix, verified behavior.
- **Phase 2 (Interface Abstraction):** Standard Go interface extraction, no external dependencies.
- **Phase 3 (FTP):** Library patterns prescribed in STACK.md, pitfalls fully enumerated.
- **Phase 5 (JSON-RPC):** Adapter pattern, auth pattern, and library docs all verified.

---

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | All 5 libraries verified via official pkg.go.dev docs and GitHub releases. Version compatibility confirmed. CVE status checked. |
| Features | HIGH | FTP/SFTP feature sets based on protocol specs and official library docs. JSON-RPC based on spec + aria2 reference. HTTP redirect based on stdlib docs. |
| Architecture | HIGH | Based on direct codebase inspection + verified library integration points. Interface patterns are standard Go. |
| Pitfalls | HIGH | All 6 critical pitfalls backed by official docs, CVE records, or reproducible open-source bugs. Not speculative. |

**Overall confidence:** HIGH

### Gaps to Address

- **SFTP TOFU UX design:** The technical approach is known (cache keys in `~/.config/warpdl/known_hosts`, use `knownhosts.New()`), but the interactive CLI UX for accepting a new key on first connection is unspecified. This needs a concrete design before SFTP implementation begins. Specifically: does the daemon interactively prompt (it's headless), or does the CLI handle it?
- **JSON-RPC method naming conflict:** ARCHITECTURE.md uses `warpdl.*` prefixes (per PROJECT.md), while FEATURES.md suggests `aria2.*` names for AriaNG compatibility. The project spec takes precedence (`warpdl.*`), but this means AriaNG requires a plugin/proxy adapter — document this clearly so it's not revisited during implementation.
- **FTP `REST` server support detection:** `RetrFrom(path, offset)` assumes the FTP server supports the `REST` command. Some legacy servers do not. The architecture specifies checking `ServerConn` features before assuming resume works, but the exact fallback path (graceful error or full restart) needs to be decided.
- **`golang.org/x/crypto/ssh` indirect vs direct dependency:** It is already an indirect dependency via `go-keyring`. Elevating it to a direct dependency and pinning to v0.48.0 must be done explicitly to ensure the patched version is used.

---

## Sources

### Primary (HIGH confidence)
- `pkg.go.dev/github.com/jlaffaye/ftp` — FTP client capabilities, version history, TLS support, RetrFrom for resume
- `pkg.go.dev/github.com/pkg/sftp` — SFTP v1.13.10 stable, Seek support, WriteTo recommendation, concurrent reads
- `pkg.go.dev/golang.org/x/crypto` — v0.48.0 release date, CVE fix version (v0.45.0), knownhosts package
- `pkg.go.dev/github.com/creachadair/jrpc2` — v1.3.4 stable, jhttp subpackage, handler pattern
- `pkg.go.dev/github.com/coder/websocket` — v1.8.14, wsjson subpackage, gorilla archive confirmed
- `pkg.go.dev/net/http` — Default redirect behavior (10 hops), CheckRedirect semantics
- `jsonrpc.org/specification` — JSON-RPC 2.0 spec (error codes, envelope format)
- `aria2.github.io/manual/en/html/aria2c.html` — aria2 RPC method reference, token auth pattern
- `pkg.go.dev/vuln/GO-2025-4134` and `GO-2025-4135` — x/crypto/ssh CVE records
- `github.com/golang/go/issues/70530` — CVE-2024-45336 (Authorization header re-sent across redirect)
- `cvedetails.com/cve/CVE-2024-45336` — CVE record
- `pkg.go.dev/encoding/gob` — GOB field compatibility rules
- Codebase direct inspection — `pkg/warplib/dloader.go`, `internal/api/download.go`, `internal/server/web.go`, `common/types.go`

### Secondary (MEDIUM confidence)
- `securitylabs.datadoghq.com/articles/claude-mcp-cve-2025-52882` — WebSocket auth bypass on localhost JSON-RPC (CVE-2025-52882), informs pitfall 3
- `github.com/mopidy/mopidy/issues/1659` — Localhost RPC not protected against CSRF, real-world precedent
- `forum.golangbridge.org/t/websocket-in-2025/38671` — Community confirmation of gorilla archived, coder/websocket as replacement
- `skarlso.github.io/2019/02/17/go-ssh-with-host-key-verification` — Survey finding near-zero Go projects verify SSH host keys
- `winscp.net/eng/docs/ftp_modes` — FTP active vs passive mode behavior
- `github.com/sonnyp/aria2.js/blob/main/README.md` — aria2 notification event names (onDownloadStart, etc.)

### Tertiary (LOW confidence)
- None — all key decisions backed by primary or secondary sources.

---
*Research completed: 2026-02-26*
*Ready for roadmap: yes*
