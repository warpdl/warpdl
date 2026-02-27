# Stack Research

**Domain:** Go download manager — protocol expansion (FTP/FTPS, SFTP) + JSON-RPC 2.0 API
**Researched:** 2026-02-26
**Confidence:** HIGH (all core libraries verified via pkg.go.dev official docs, GitHub releases)

## Recommended Stack

### Core Technologies

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| `github.com/jlaffaye/ftp` | v0.2.0 | FTP/FTPS client | Only serious Go FTP client with 1.4k stars, active maintenance (last commit Oct 2025), supports TLS (implicit + explicit), EPSV passive mode, REST resume via `RetrFrom(path, offset)`, and standard auth. Used by rclone. Issue #138 already identifies this library. |
| `golang.org/x/crypto/ssh` | v0.48.0 | SSH transport layer for SFTP | Official Go extended standard library. Required underpinning for SFTP. v0.48.0 (Feb 9, 2026) patches all known CVEs (GO-2025-4134, GO-2025-4135). Supports password auth, public key auth (RSA/ECDSA/Ed25519), known_hosts verification. |
| `github.com/pkg/sftp` | v1.13.10 | SFTP client over SSH | De-facto standard Go SFTP client. v1.13.10 (Oct 22, 2025). Implements `io.Seeker` via `File.Seek(offset, whence)` enabling resume. `WriteTo(w io.Writer)` provides optimized streaming for high-latency connections. Multiple goroutines can share one SSH connection. |
| `github.com/creachadair/jrpc2` | v1.3.4 | JSON-RPC 2.0 server and client | v1.3.4 (Nov 30, 2025). Stable v1 API with no-breaking-changes guarantee. Clean handler pattern (`func(ctx, *Request) (any, error)`). HTTP transport via built-in `jhttp` subpackage. IBM maintains a fork showing enterprise trust. Most actively maintained JSON-RPC 2.0 Go library. |
| `github.com/coder/websocket` | v1.8.14 | WebSocket transport for JSON-RPC | Formerly nhooyr/websocket, now maintained by Coder. v1.8.14 (Sep 2025). Zero external dependencies, context-aware, concurrent writes safe, `wsjson` subpackage for direct JSON read/write. Gorilla/websocket is archived (Dec 2022) — do not use it. |

### Supporting Libraries

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `golang.org/x/crypto/ssh/knownhosts` | bundled with x/crypto | Parse OpenSSH known_hosts for host verification | Always — use instead of `InsecureIgnoreHostKey()`. Required for production SFTP. |
| `net/http` stdlib | Go 1.24.9 built-in | HTTP redirect following (#144) and JSON-RPC HTTP transport | HTTP redirect is a zero-dependency fix via `CheckRedirect` policy on `http.Client`. The existing HTTP downloader in `pkg/warplib/dloader.go` needs `CheckRedirect` configured to allow redirects (current behavior is the Go default of 10 hops, which may already work — verify before adding any dependency). |
| `encoding/json` stdlib | Go 1.24.9 built-in | JSON serialization for JSON-RPC 2.0 | Already available. jrpc2 and coder/websocket both use it internally. |

### Development Tools

| Tool | Purpose | Notes |
|------|---------|-------|
| `go test -race -short ./...` | Race detection for concurrent SFTP/FTP downloads | FTP/SFTP are single-stream but connection pooling code must be race-safe |
| `scripts/check_coverage.sh` | Enforce 80% coverage per package | New packages (`pkg/warplib/ftp.go`, `pkg/warplib/sftp.go`, `internal/api/jsonrpc/`) each need mock-based unit tests |
| `go test -tags=e2e` | E2E protocol tests | Add FTP/SFTP E2E tests using public test servers (ftp.dlptest.com for FTP, test.rebex.net for SFTP) |

## Installation

```bash
# FTP client
go get github.com/jlaffaye/ftp@v0.2.0

# SFTP client (SSH transport + SFTP protocol)
go get golang.org/x/crypto@v0.48.0
go get github.com/pkg/sftp@v1.13.10

# JSON-RPC 2.0 server
go get github.com/creachadair/jrpc2@v1.3.4

# WebSocket transport for JSON-RPC
go get github.com/coder/websocket@v1.8.14
```

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| `github.com/jlaffaye/ftp` | `github.com/goftp/server` | Never for a client — goftp/server is a server implementation, not a client |
| `github.com/jlaffaye/ftp` | `github.com/secsy/goftp` | If you need connection pooling and retries built-in. secsy/goftp has a cleaner API but lower adoption and last release 2021. jlaffaye is more actively maintained. |
| `github.com/pkg/sftp` | `github.com/pkg/sftp/v2` | Only when v2 leaves alpha. v2.0.0-alpha exists (Jan 2025) but API is unstable. Stick with v1.13.10 for now. |
| `github.com/creachadair/jrpc2` | `github.com/sourcegraph/jsonrpc2` | If you need WebSocket baked into the core lib. sourcegraph/jsonrpc2 has a websocket subpackage but is pre-v1 (v0.2.1), lacks batch support, and moves slowly. jrpc2 is v1-stable and more featureful. |
| `github.com/coder/websocket` | `github.com/gorilla/websocket` | Never for new projects. Gorilla was archived Dec 2022. No security patches. |
| `net/http` CheckRedirect | External redirect library | Never. Redirect following is native to `net/http`. Set `CheckRedirect` to `nil` (use Go default of 10 hops) or write a custom policy. Zero dependencies needed. |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| `github.com/gorilla/websocket` | Archived Dec 2022. No security patches. Multiple downstream projects (k6, argo-workflows) are actively replacing it. | `github.com/coder/websocket` |
| `net/rpc/jsonrpc` (stdlib) | Implements JSON-RPC 1.0 only. No notification support, no batch, no id:null. Issue #10929 has been open since 2015 with no resolution. | `github.com/creachadair/jrpc2` |
| `github.com/pkg/sftp/v2` | Alpha as of Jan 2025. API unstable, no stable release. | `github.com/pkg/sftp` v1.13.10 |
| `golang.org/x/crypto` < v0.45.0 | CVE GO-2025-4134 (unbounded memory in ssh), GO-2025-4135 (DoS in ssh/agent). Fix landed in v0.45.0. | `golang.org/x/crypto` v0.48.0 |
| FTP active mode | NAT-breaking. Almost all modern servers require passive mode. jlaffaye defaults to EPSV (extended passive). Don't override this. | Leave EPSV default in jlaffaye |
| SFTP `InsecureIgnoreHostKey()` | Disables MITM protection entirely. Security regression. | `knownhosts.New(knownHostsFile)` or `ssh.FixedHostKey(key)` |

## Stack Patterns by Variant

**For FTP/FTPS downloader (`pkg/warplib/ftp.go`):**
- Use `ftp.DialWithTimeout(5*time.Second)` — always set a timeout, the library has no default
- Use `ftp.DialWithExplicitTLS(tlsConfig)` for `ftps://` URLs (STARTTLS, port 21)
- Use `ftp.DialWithTLS(tlsConfig)` for implicit TLS (port 990, rare)
- Use `RetrFrom(path, offset)` for resume — it issues the FTP `REST` command
- FTP is single-stream: no parallel segments. Model as a single-part `Item` in warplib.

**For SFTP downloader (`pkg/warplib/sftp.go`):**
- `golang.org/x/crypto/ssh` creates the TCP+SSH connection
- `github.com/pkg/sftp` wraps it for file operations
- For resume: `sftp.Client.Open(path)` then `file.Seek(offset, io.SeekStart)` then `file.WriteTo(localFile)`
- Use `WriteTo` not `Read` in a loop — the library docs explicitly recommend it for throughput
- SFTP is single-stream: no parallel segments. Same warplib model as FTP.
- Host key verification: load `~/.ssh/known_hosts` via `knownhosts.New()`, not `InsecureIgnoreHostKey()`

**For JSON-RPC 2.0 server (`internal/api/jsonrpc/`):**
- Serve over HTTP via `jrpc2/jhttp` on the existing secondary HTTP server at `port+1`
- Add WebSocket upgrade via `coder/websocket` for clients that need push notifications
- Bind to `127.0.0.1` only by default; add a `--rpc-host` flag for explicit opt-in to remote access
- Auth token: require `Authorization: Bearer <token>` header; generate token on daemon start, persist to `~/.config/warpdl/rpc_secret`
- The JSON-RPC method naming convention should mirror aria2: `warp.addUri`, `warp.remove`, `warp.tellStatus`, etc.

**For HTTP redirect following (`pkg/warplib/dloader.go`):**
- Verify first: Go's `net/http.Client` follows redirects by default (up to 10 hops). The existing downloader may already work.
- If redirect chains strip the `Range` header (some CDN redirect implementations do this), set a custom `CheckRedirect` that preserves it.
- No new dependency required. This is likely a 5-10 line fix.

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|-----------------|-------|
| `github.com/pkg/sftp` v1.13.10 | `golang.org/x/crypto` v0.48.0 | pkg/sftp internally requires x/crypto ≥ v0.35.0 (verified from search). v0.48.0 is safe. |
| `github.com/creachadair/jrpc2` v1.3.4 | Go 1.21+ | jrpc2 uses generics and modern Go patterns. Go 1.24.9 is fully compatible. |
| `github.com/coder/websocket` v1.8.14 | Go 1.21+ | Zero external dependencies, compatible with Go 1.24.9. |
| `github.com/jlaffaye/ftp` v0.2.0 | Go 1.18+ | No special requirements. Go 1.24.9 compatible. |

## Sources

- [pkg.go.dev/github.com/jlaffaye/ftp](https://pkg.go.dev/github.com/jlaffaye/ftp) — capabilities, version history (v0.2.0 latest, May 2023; master commits Oct 2025) — HIGH confidence
- [github.com/jlaffaye/ftp commits](https://github.com/jlaffaye/ftp/commits/master) — confirmed active maintenance Oct 2025 — HIGH confidence
- [pkg.go.dev/github.com/pkg/sftp](https://pkg.go.dev/github.com/pkg/sftp) — v1.13.10 stable, seek support confirmed, WriteTo recommendation — HIGH confidence
- [pkg.go.dev/golang.org/x/crypto](https://pkg.go.dev/golang.org/x/crypto?tab=versions) — v0.48.0 published Feb 9, 2026; CVE fix in v0.45.0 — HIGH confidence
- [pkg.go.dev/github.com/creachadair/jrpc2](https://pkg.go.dev/github.com/creachadair/jrpc2) — v1.3.4 stable Nov 30, 2025; HTTP via jhttp subpackage — HIGH confidence
- [pkg.go.dev/github.com/coder/websocket](https://pkg.go.dev/github.com/coder/websocket) — v1.8.14 Sep 2025; wsjson for JSON read/write — HIGH confidence
- [Go Forum: WebSocket in 2025](https://forum.golangbridge.org/t/websocket-in-2025/38671) — community confirms gorilla archived, coder/websocket as replacement — MEDIUM confidence
- [The New Stack: Gorilla Toolkit Becomes Abandonware](https://thenewstack.io/gorilla-toolkit-open-source-project-becomes-abandonware/) — gorilla archived Dec 9, 2022 confirmed — HIGH confidence
- [GO-2025-4134 vulnerability](https://pkg.go.dev/vuln/GO-2025-4134) — x/crypto/ssh CVE, fix version v0.45.0 — HIGH confidence
- [golang/go Issue #10929](https://github.com/golang/go/issues/10929) — stdlib net/rpc/jsonrpc is JSON-RPC 1.0 only, no 2.0 support — HIGH confidence

---
*Stack research for: WarpDL protocol expansion — FTP/SFTP/HTTP redirects/JSON-RPC 2.0*
*Researched: 2026-02-26*
