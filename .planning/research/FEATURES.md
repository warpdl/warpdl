# Feature Research

**Domain:** Download manager — protocol expansion (FTP/SFTP) and JSON-RPC API
**Researched:** 2026-02-26
**Confidence:** HIGH (FTP/SFTP features), HIGH (JSON-RPC features), MEDIUM (HTTP redirect nuances)

---

## Context

This milestone adds four discrete features to an existing Go download manager (WarpDL):
1. HTTP redirect following (#144)
2. FTP/FTPS protocol support (#138)
3. SFTP protocol support (#139)
4. JSON-RPC 2.0 API (#137)

The existing system does HTTP/HTTPS parallel segment downloading with a daemon architecture.
All four features are additive — they cannot break existing behavior.

---

## Feature Landscape

### HTTP Redirect Following (#144)

#### Table Stakes (Users Expect These)

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Follow 301/302/303/307/308 transparently | Any download URL behind a CDN or shortened URL will redirect; not following = broken downloads | LOW | Go's `net/http.Client.CheckRedirect` already handles this; current code likely disables it via custom client config. Removing the restriction or setting a sane policy is all that's needed. |
| Preserve HTTP method across redirect types | 307/308 preserve method; 301/302 convert to GET. Download managers issue GET, so this is transparent | LOW | Since downloads are GET-only, method preservation differences between redirect types are irrelevant in practice. |
| Max redirect hops limit (default 10) | Prevents infinite redirect loops from consuming resources indefinitely | LOW | Go stdlib defaults to 10 hops. Expose as configurable option. |
| Final URL tracking | Users and the manager need to know the actual file URL after redirects for resume and display | LOW | Capture `Response.Request.URL` after redirect chain resolves. |

#### Differentiators

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Configurable max redirect hops | Power users can increase/decrease limit per download | LOW | Simple option field in `DownloaderOpts`. Aria2 uses 20 as default. |
| Circular redirect detection with clear error | Error message "redirect loop detected" is more useful than generic timeout | LOW | Inspect `via` slice in `CheckRedirect` for repeated URLs. |

#### Anti-Features for HTTP Redirect

| Anti-Feature | Why Requested | Why Problematic | Alternative |
|--------------|---------------|-----------------|-------------|
| Cross-protocol redirect following (HTTP → FTP) | "Just follow wherever it goes" | HTTP RFC explicitly prohibits cross-protocol redirects; security risk (open redirect to arbitrary protocol) | Validate scheme in `CheckRedirect`, return error if scheme changes |
| Trusting redirect `Location` headers unconditionally | Simplicity | SSRF vector if redirected to localhost/internal IPs | Add configurable allowlist/denylist for redirect targets; default behavior: allow all, but document the risk |

---

### FTP/FTPS Protocol Support (#138)

#### Table Stakes (Users Expect These)

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Connect to `ftp://` URLs | Basic protocol support; the entire feature request | MEDIUM | Use `github.com/jlaffaye/ftp`. Parse URL for host, port, path, credentials. |
| Anonymous FTP login | Most public FTP servers use anonymous access (username `anonymous`, email as password) | LOW | Parse URL; if no credentials, use anonymous. Already a `jlaffaye/ftp` convention. |
| Authenticated FTP (username + password from URL) | Private FTP servers require credentials | LOW | Extract from `ftp://user:pass@host/path` URL format. |
| Passive mode (PASV/EPSV) by default | Firewalls and NAT block active mode (server-initiated connections); passive is the only mode that works reliably for end users | LOW | `jlaffaye/ftp` uses EPSV by default; falls back to PASV. Correct behavior by default. |
| Binary transfer mode | Text mode corrupts binary files (translates line endings) | LOW | Always use `TYPE I` (binary). `jlaffaye/ftp` defaults to binary. |
| Resume support (`REST` command) | Large FTP files interrupted mid-download need resume or they restart from zero | MEDIUM | Use `RetrFrom(path, offset)` method from `jlaffaye/ftp`. Must store offset in `Item.Parts` state. Because FTP has no native range requests, resume means single-connection seek — not multi-segment. |
| FTPS explicit TLS (`ftps://` or `ftp://` with `--use-tls`) | FTPS is increasingly required by enterprise/institutional FTP servers for compliance | MEDIUM | `jlaffaye/ftp` supports both `DialWithExplicitTLS` and `DialWithTLS` (implicit). Need to determine from URL scheme or user option. |
| Report file size before download | Users expect progress bars; size comes from `SIZE` command or directory listing | LOW | Call `c.FileSize(path)` before `RetrFrom`. Falls back to unknown if server doesn't support `SIZE`. |
| Store as single-stream download | FTP has no Accept-Ranges equivalent; cannot split segments | LOW | Unlike HTTP downloader, FTP downloader is single-stream only. One connection, one part. This is a hard protocol constraint. |

#### Differentiators for FTP

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| FTPS implicit TLS (`ftps://` scheme port 990) | Some legacy FTPS servers only support implicit TLS | LOW | `jlaffaye/ftp.DialWithTLS(tlsConfig)` on port 990 instead of 21. Map `ftps://` → implicit TLS. |
| Configurable connection timeout | Corporate firewalls often have slow FTP handshakes; default 30s may be too short | LOW | `ftp.DialWithTimeout(d)` option in `jlaffaye/ftp`. Expose via `DownloaderOpts`. |
| TLS certificate verification control | Self-signed FTPS servers are common in enterprise environments | LOW | Expose `InsecureSkipVerify` as option. Default to `false` (secure). |
| Keep-alive pings during long downloads | FTP servers disconnect idle control connections | LOW | `c.NoOp()` periodically. Needed for large files on servers with short idle timeouts. |

#### Anti-Features for FTP

| Anti-Feature | Why Requested | Why Problematic | Alternative |
|--------------|---------------|-----------------|-------------|
| Parallel segment downloading over FTP | Users assume FTP = same as HTTP parallel download | FTP doesn't support byte-range requests natively. Multiple connections downloading different chunks of the same file require server-side coordination that most FTP servers don't support. This is a hard protocol limitation. | Clear documentation + single-stream with keep-alive and resume. Progress bar without segments. |
| FTP active mode support | "Complete protocol support" | Active mode requires the server to connect back to the client. Fails through NAT, firewalls, and most home networks without port forwarding. More code for a mode that almost never works. | Passive mode only. Document clearly. |
| FTP directory listing / recursive download | "Download entire FTP directory" | Major scope expansion beyond issue #138 which is single-file download. Adds recursive traversal, file filtering, collision handling, huge test surface. | v2 feature if demand exists. Keep MVP as single-file download from `ftp://` URL. |
| FTP upload | "It's just the reverse" | Issue #138 is download-only. Upload is a completely different use case, different UX, different command surface. | Out of scope for this milestone. |
| FTP proxy support | Parity with HTTP proxy | FTP-specific proxies are rare, complex, and have multiple incompatible protocols (SOCKS, HTTP CONNECT, FTP proxy gateway). Not worth the complexity. | HTTP/SOCKS proxy tunneling if needed in the future. |

---

### SFTP Protocol Support (#139)

#### Table Stakes (Users Expect These)

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Connect to `sftp://` URLs | Basic protocol support; the entire feature request | MEDIUM | Use `golang.org/x/crypto/ssh` + `github.com/pkg/sftp`. Parse URL for host, port (default 22), path, credentials. |
| Password authentication | Most users connecting to their own servers use password auth | LOW | `ssh.Password("pass")` in `ssh.ClientConfig.Auth`. Extract from `sftp://user:pass@host/path` URL. |
| SSH private key file authentication | More secure, common for automated use cases; server admins often disable password auth | MEDIUM | `ssh.PublicKeys(signer)`. Key path configurable via CLI flag or option. Must handle passphrase-protected keys too. |
| Single-stream download only | SFTP is an SSH subsystem; multiplexing is limited and not analogous to HTTP range requests | LOW | `sftp.Client.Open(path)` → `file.WriteTo(localFile)`. Single stream is the correct and expected behavior. |
| Resume via `Seek()` | SFTP files support `ReadAt` and seeking; resume means seeking to offset before reading | MEDIUM | `file.Seek(offset, io.SeekStart)`. Must store offset in `Item.Parts` state like FTP resume. |
| Report file size before download | Progress bar requires size; `sftp.Client.Stat(path).Size()` gives size | LOW | Call before opening file for download. |
| Host key verification (with known_hosts or explicit fingerprint) | Blind host key acceptance is a MITM vulnerability | HIGH | This is the hard part of SFTP. Options: (a) auto-accept first-use (TOFU), (b) use system known_hosts, (c) explicit fingerprint. **Default must not be InsecureIgnoreHostKey in production.** |

#### Differentiators for SFTP

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| TOFU (Trust-On-First-Use) host key policy | Friendlier than strict known_hosts for single-user desktop use; warns on key change | MEDIUM | Cache host keys in `~/.config/warpdl/known_hosts`. Accept on first connection, reject on mismatch with clear error. |
| Configurable port (default 22) | Non-standard SSH ports are common in enterprise | LOW | Parse from URL: `sftp://user@host:2222/path`. |
| Passphrase-protected key support | Most security-conscious users encrypt their private keys | MEDIUM | Prompt for passphrase interactively via CLI. Pass via flag for non-interactive use. |
| Connection keep-alive | SSH connections drop on idle; long SFTP downloads on flaky networks stall silently | LOW | SSH KeepAlive config via `ssh.ClientConfig.Config`. |

#### Anti-Features for SFTP

| Anti-Feature | Why Requested | Why Problematic | Alternative |
|--------------|---------------|-----------------|-------------|
| SSH agent integration | "Use my existing SSH credentials" | Adds dependency on SSH agent socket, platform-specific code, complex key negotiation. Issue #139 explicitly lists this as P1 enhancement (out of scope for MVP). | Explicit key file path flag. Document SSH agent as future enhancement. |
| Parallel segment downloading over SFTP | Same as FTP — users assume it | SSH connection multiplexing is different from HTTP range requests. Concurrent `ReadAt` on the same file over SFTP has race conditions and most servers limit concurrent operations. `github.com/pkg/sftp` does support `MaxConcurrentRequestsPerFile` but this is for pipeline buffering, not true parallel segments. | Use `UseConcurrentReads` from `pkg/sftp` for throughput optimization without the complexity of segment splitting. |
| SFTP directory sync / recursive download | "Download entire remote dir" | Same scope creep issue as FTP. Issue #139 is single-file download. | Defer to v2. |
| SCP protocol support | "It's similar to SFTP" | SCP is a different protocol with no resume support and different tooling. Separate feature entirely. | Out of scope. |
| InsecureIgnoreHostKey as default | "Easier for users" | Active MITM vulnerability. Any tool that defaults to ignoring host keys is categorically insecure. | TOFU policy with warning. Never silently ignore. |

---

### JSON-RPC 2.0 API (#137)

#### Table Stakes (Users Expect These)

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| JSON-RPC 2.0 spec compliance | Third-party tools (AriaNG, scripts) assume spec compliance: `jsonrpc: "2.0"`, `id`, `method`, `params`, `result`/`error` fields | LOW | Follow https://www.jsonrpc.org/specification exactly. |
| `aria2.addUri` equivalent (add download by URL) | AriaNG and most JSON-RPC download manager clients call this method name | LOW | Map to existing `downloadHandler`. Accept URL + optional options dict. |
| `aria2.pause` / `aria2.unpause` | Expected download control methods | LOW | Map to stop/resume handlers. |
| `aria2.remove` | Remove a download from the queue | LOW | Map to flush handler. |
| `aria2.tellStatus` (get download status) | Polling-based clients need this; required for AriaNG | LOW | Return download state: status, totalLength, completedLength, downloadSpeed, gid. |
| `aria2.tellActive` / `aria2.tellWaiting` / `aria2.tellStopped` | List downloads by state | LOW | Map to existing list handler with state filter. |
| `aria2.getVersion` | Client compatibility check | LOW | Return version info. Already exists in `versionHandler`. |
| WebSocket notifications (push events) | Real-time progress without polling; `onDownloadStart`, `onDownloadProgress`, `onDownloadComplete`, `onDownloadError`, `onDownloadStop` | MEDIUM | WarpDL already has a WebSocket server (`web.go`). Wire pool broadcast events to JSON-RPC notification format. |
| HTTP transport support | Simple script integration without WebSocket | LOW | GET/POST over HTTP to `/jsonrpc` endpoint. Already have HTTP server on `port+1`. |
| Secret token authentication | Without auth, any local process can control downloads | LOW | aria2 uses `token:` prefix in params. Add `Authorization: Bearer <token>` header support too. Bind to `127.0.0.1` only by default. |
| Standard error codes | JSON-RPC spec defines error codes: -32700 (parse error), -32600 (invalid request), -32601 (method not found), -32602 (invalid params), -32603 (internal error) | LOW | Wrap all errors in `{"code": N, "message": "..."}` format. |

#### Differentiators for JSON-RPC

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| AriaNG compatibility (aria2 method names) | AriaNG is the most popular download manager UI; if WarpDL speaks aria2 RPC, it gets AriaNG for free | MEDIUM | Not all aria2 methods are relevant (BitTorrent, Metalink). Implement download management subset: addUri, pause, unpause, remove, tellStatus, tellActive, tellWaiting, tellStopped, getGlobalStat, getVersion. |
| `system.listMethods` | Self-documenting API; API clients can discover capabilities | LOW | Return array of supported method names. |
| `aria2.getGlobalStat` | Total download speed, number of active downloads | LOW | Aggregate from active downloaders. |
| Configurable listen address | Allow remote access when explicitly opted into | LOW | Default: `127.0.0.1:port`. Configurable via flag: `--rpc-listen-all` binds to `0.0.0.0`. |
| Configurable token via flag or config file | Automation-friendly setup | LOW | `--rpc-secret` flag. Also support reading from env var `WARPDL_RPC_SECRET`. |

#### Anti-Features for JSON-RPC

| Anti-Feature | Why Requested | Why Problematic | Alternative |
|--------------|---------------|-----------------|-------------|
| JSON-RPC batch requests (`system.multicall`) | aria2 compatibility; "do multiple things atomically" | Significant implementation complexity for a feature that's rarely used in practice. The PROJECT.md explicitly lists this as out of scope. | Expose single-method calls; clients can pipeline requests serially. |
| Rate limiting | "Production API hardening" | Overkill for a local daemon API. WarpDL is a single-user tool bound to localhost. Rate limiting adds complexity with near-zero security benefit against local attackers. | Explicit out of scope. Auth token is sufficient. |
| OAuth2 / JWT authentication | "Enterprise-grade security" | WarpDL is a local desktop download manager, not a SaaS API. OAuth2 flow for a local tool is absurd UX. | Static secret token (`--rpc-secret`) is the standard pattern (aria2, transmission, qBittorrent all use this). |
| XML-RPC support | aria2 supports XML-RPC too | JSON-RPC 2.0 is the modern standard. XML-RPC is legacy. No modern tooling uses it. AriaNG uses JSON-RPC only. | JSON-RPC 2.0 only. |
| Remote access by default (bind `0.0.0.0`) | "Useful for remote management" | Binds download manager API to all network interfaces; combined with weak or no token, exposes attack surface. The vulhub/aria2 RCE CVE is literally an aria2 RPC endpoint exposed without auth. | Localhost-only by default. Explicit opt-in `--rpc-listen-all` flag with warning in docs. |
| WebSocket-only transport | "Simpler implementation" | Curl-based scripts and simple integrations need HTTP POST. Removing HTTP support excludes non-browser tooling. | Both HTTP POST and WebSocket on the same port/path. |

---

## Feature Dependencies

```
HTTP Redirect Following
    └──required by──> FTP download (FTP servers can redirect; HTTP redirects from ftp:// links on web)
    └──required by──> JSON-RPC addUri (URLs submitted via RPC may redirect)

FTP Protocol Support
    └──requires──> New downloader interface (protocol-agnostic downloader abstraction in warplib)
    └──enhances──> JSON-RPC addUri (adds ftp:// URL support)

SFTP Protocol Support
    └──requires──> New downloader interface (same abstraction as FTP)
    └──enhances──> JSON-RPC addUri (adds sftp:// URL support)

JSON-RPC API
    └──requires──> Existing daemon HTTP server (already exists on port+1)
    └──enhances──> All download protocols (adds programmatic control)
    └──requires──> Auth token mechanism (secret token validation middleware)

New Downloader Interface (abstraction)
    └──required by──> FTP downloader implementation
    └──required by──> SFTP downloader implementation
    └──enables──> Future protocol additions without touching existing HTTP code
```

### Dependency Notes

- **FTP and SFTP both require a protocol-agnostic downloader interface:** The current `Downloader` struct is HTTP-specific (`*http.Client`). Adding FTP/SFTP requires either: (a) a new parallel struct implementing a common interface, or (b) a thin abstraction over the start/stop/progress surface. Option (a) is simpler and avoids regression risk on the HTTP downloader.
- **HTTP redirect is independent and can ship first:** It's a one-line change to `http.Client` config. No new downloader abstraction needed.
- **JSON-RPC does not depend on FTP/SFTP:** The API layer dispatches to whatever downloader handles the URL scheme. FTP/SFTP extend the URL dispatch table; JSON-RPC just passes URLs through.
- **SFTP host key verification blocks progress:** The security decision (TOFU vs known_hosts vs InsecureIgnoreHostKey) must be made before SFTP can ship. This is a design gate, not just implementation.

---

## MVP Definition

### Launch With (v1 — this milestone)

- [ ] HTTP redirect following — Minimal code change, unblocks all other features, ships first
- [ ] FTP single-stream download with resume — Core protocol support per issue #138
- [ ] FTPS (explicit TLS) support — Security baseline; most institutional FTP requires it
- [ ] SFTP with password auth — Core protocol support per issue #139; key auth adds complexity
- [ ] SFTP with private key file auth — Required for automated use cases; most server admins disable password
- [ ] SFTP TOFU host key policy — Security is non-negotiable; but must be user-friendly
- [ ] JSON-RPC 2.0 HTTP + WebSocket endpoint — Core API for issue #137
- [ ] JSON-RPC auth token (secret) — Security prerequisite before any API ships
- [ ] JSON-RPC aria2 method subset (addUri, pause, unpause, remove, tellStatus, tellActive, tellStopped, getVersion) — Minimum for AriaNG compatibility
- [ ] JSON-RPC push notifications via WebSocket (onDownloadStart, onDownloadProgress, onDownloadComplete, onDownloadError) — Required for real-time UIs

### Add After Validation (v1.x)

- [ ] SFTP SSH agent support — Blocked by platform complexity; common request
- [ ] FTP keep-alive pings — Edge case for long downloads on servers with short idle timeouts
- [ ] JSON-RPC batch requests — Only if AriaNG or other clients require it
- [ ] FTPS implicit TLS — Rare legacy requirement

### Future Consideration (v2+)

- [ ] FTP directory/recursive download — Major scope expansion; new feature category
- [ ] SFTP recursive download — Same
- [ ] JSON-RPC rate limiting — Only relevant if WarpDL becomes a shared/multi-user daemon
- [ ] AriaNG web UI bundling — Downstream usage, not WarpDL's responsibility

---

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| HTTP redirect following | HIGH | LOW | P1 |
| FTP download (single-stream, resume) | HIGH | MEDIUM | P1 |
| FTPS explicit TLS | HIGH | LOW | P1 |
| SFTP password auth | HIGH | MEDIUM | P1 |
| SFTP private key auth | HIGH | MEDIUM | P1 |
| SFTP TOFU host key | HIGH | MEDIUM | P1 |
| JSON-RPC HTTP endpoint | HIGH | LOW | P1 |
| JSON-RPC auth token | HIGH | LOW | P1 |
| JSON-RPC aria2 methods subset | HIGH | MEDIUM | P1 |
| JSON-RPC WebSocket notifications | HIGH | MEDIUM | P1 |
| Protocol-agnostic downloader interface | HIGH | MEDIUM | P1 (enabler) |
| FTPS implicit TLS | LOW | LOW | P2 |
| FTP keep-alive NoOp | LOW | LOW | P2 |
| SFTP SSH agent | MEDIUM | HIGH | P2 |
| JSON-RPC `system.listMethods` | LOW | LOW | P2 |
| JSON-RPC `getGlobalStat` | MEDIUM | LOW | P2 |
| FTP recursive directory download | MEDIUM | HIGH | P3 |
| JSON-RPC batch requests | LOW | HIGH | P3 |

---

## Competitor Feature Analysis

| Feature | aria2 | WinSCP (FTP/SFTP) | curl | WarpDL (target) |
|---------|-------|-------------------|------|-----------------|
| HTTP redirect follow | Auto (20 hops max) | N/A | Auto (-L flag) | Auto (10 hops, configurable) |
| FTP passive mode | Default | Default | Default (-P flag for active) | Default (EPSV/PASV only) |
| FTP resume | Yes (REST) | Yes | Yes (-C -) | Yes (via RetrFrom offset) |
| FTPS explicit | Yes | Yes | Yes (--ftp-ssl) | Yes |
| FTPS implicit | Yes | Yes | Yes (--ftp-ssl-reqd) | P2 |
| SFTP password auth | Yes | Yes | Yes | Yes |
| SFTP key auth | Yes | Yes | Yes (-i flag) | Yes |
| SFTP SSH agent | Yes | Yes | Yes | P2 |
| SFTP host key verify | Yes (known_hosts) | Yes | Yes | TOFU |
| JSON-RPC API | Yes (port 6800) | No | No | Yes (port+1) |
| JSON-RPC auth token | Yes (--rpc-secret) | N/A | N/A | Yes (--rpc-secret) |
| AriaNG compatible | Yes | N/A | N/A | Partial (download methods) |
| WebSocket notifications | Yes | N/A | N/A | Yes |
| Parallel HTTP segments | Yes | No | No | Yes (existing) |
| FTP parallel segments | No | No | No | No (protocol limit) |
| SFTP parallel segments | No (concurrent pipeline only) | No | No | No (protocol limit) |

---

## Sources

- aria2 JSON-RPC documentation: [aria2c(1) — aria2 1.37.0 documentation](https://aria2.github.io/manual/en/html/aria2c.html) (HIGH confidence — official docs)
- `github.com/jlaffaye/ftp` package documentation: [ftp package - pkg.go.dev](https://pkg.go.dev/github.com/jlaffaye/ftp) (HIGH confidence — official Go package docs)
- `github.com/pkg/sftp` package documentation: [sftp package - pkg.go.dev](https://pkg.go.dev/github.com/pkg/sftp) (HIGH confidence — official Go package docs)
- JSON-RPC 2.0 specification: [jsonrpc.org/specification](https://www.jsonrpc.org/specification) (HIGH confidence — specification)
- aria2 security (RCE via exposed RPC): [vulhub/aria2/rce](https://github.com/vulhub/vulhub/blob/master/aria2/rce/README.md) (HIGH confidence — documented CVE, informs anti-feature decision)
- FTP connection modes: [WinSCP FTP modes documentation](https://winscp.net/eng/docs/ftp_modes) (MEDIUM confidence — authoritative FTP resource)
- Go redirect handling: [pkg.go.dev/net/http](https://pkg.go.dev/net/http) (HIGH confidence — official Go docs)
- aria2 notification events (onDownloadStart, etc.): [aria2.js README](https://github.com/sonnyp/aria2.js/blob/main/README.md) (MEDIUM confidence — multiple implementations confirm same event names)

---

*Feature research for: WarpDL protocol expansion & JSON-RPC API milestone*
*Researched: 2026-02-26*
