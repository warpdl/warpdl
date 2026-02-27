# Requirements: WarpDL Issues Fix Milestone

**Defined:** 2026-02-26
**Core Value:** Expand WarpDL's protocol coverage and integration surface so it can download from more sources and be controlled programmatically

## v1 Requirements

Requirements for this milestone. Each maps to roadmap phases.

### HTTP Redirect

- [x] **REDIR-01**: User can download files behind HTTP 301/302/303/307/308 redirects transparently
- [x] **REDIR-02**: Downloader tracks and uses final URL after redirect chain for all segment requests
- [x] **REDIR-03**: Redirect chain is limited to configurable max hops (default 10) with clear error on loop
- [x] **REDIR-04**: Authorization headers are not leaked across cross-origin redirects (CVE-2024-45336 regression guard)

### Protocol Abstraction

- [x] **PROTO-01**: Download engine supports a protocol-agnostic downloader interface so FTP/SFTP can plug in alongside HTTP
- [x] **PROTO-02**: Manager dispatches to correct downloader based on URL scheme (http/https/ftp/ftps/sftp)
- [x] **PROTO-03**: Item persistence (GOB) supports protocol field with backward-compatible zero value defaulting to HTTP

### FTP

- [ ] **FTP-01**: User can download files from `ftp://` URLs
- [ ] **FTP-02**: Anonymous FTP login is used by default when no credentials in URL
- [ ] **FTP-03**: User can authenticate with username/password via URL (`ftp://user:pass@host/path`)
- [ ] **FTP-04**: FTP uses passive mode (EPSV/PASV) by default
- [ ] **FTP-05**: FTP downloads are single-stream (no parallel segments)
- [ ] **FTP-06**: User can resume interrupted FTP downloads via REST/RetrFrom offset
- [ ] **FTP-07**: User can download from FTPS servers with explicit TLS
- [ ] **FTP-08**: File size is reported before download starts for progress tracking

### SFTP

- [ ] **SFTP-01**: User can download files from `sftp://` URLs
- [ ] **SFTP-02**: User can authenticate with password via URL (`sftp://user:pass@host/path`)
- [ ] **SFTP-03**: User can authenticate with SSH private key file (default keys `~/.ssh/id_rsa`, `~/.ssh/id_ed25519`)
- [ ] **SFTP-04**: User can specify custom SSH key path via `--ssh-key` flag
- [ ] **SFTP-05**: SFTP downloads are single-stream (no parallel segments)
- [ ] **SFTP-06**: User can resume interrupted SFTP downloads via Seek offset
- [ ] **SFTP-07**: Host key verification uses TOFU policy (accept first use, reject on change) with `~/.config/warpdl/known_hosts`
- [ ] **SFTP-08**: Custom port support via URL (`sftp://user@host:2222/path`)
- [ ] **SFTP-09**: File size is reported before download starts for progress tracking

### JSON-RPC

- [ ] **RPC-01**: Daemon exposes JSON-RPC 2.0 endpoint over HTTP at `/jsonrpc` on existing web server port
- [ ] **RPC-02**: Daemon exposes WebSocket endpoint at `/jsonrpc/ws` for real-time communication
- [ ] **RPC-03**: Auth token required for all RPC requests (`--rpc-secret` flag, `WARPDL_RPC_SECRET` env var)
- [ ] **RPC-04**: RPC binds to localhost only by default, `--rpc-listen-all` for explicit opt-in to all interfaces
- [ ] **RPC-05**: `download.add` method accepts URL and options, starts download
- [ ] **RPC-06**: `download.pause` and `download.resume` methods control active downloads
- [ ] **RPC-07**: `download.remove` method removes download from queue
- [ ] **RPC-08**: `download.status` method returns download state (status, totalLength, completedLength, speed)
- [ ] **RPC-09**: `download.list` method returns downloads filtered by state (active/waiting/stopped)
- [ ] **RPC-10**: `system.getVersion` method returns daemon version info
- [ ] **RPC-11**: WebSocket pushes real-time notifications (download.started, download.progress, download.complete, download.error)
- [ ] **RPC-12**: Standard JSON-RPC 2.0 error codes for parse errors, invalid requests, method not found

## v2 Requirements

Deferred to future release. Tracked but not in current roadmap.

### SFTP Enhanced

- **SFTP-P1-01**: SSH agent integration for credential forwarding
- **SFTP-P1-02**: Passphrase-protected key support with interactive prompt
- **SFTP-P1-03**: SCP URL format support (`scp://...`)

### FTP Enhanced

- **FTP-P1-01**: FTPS implicit TLS (`ftps://` scheme, port 990)
- **FTP-P1-02**: Configurable connection timeout
- **FTP-P1-03**: Keep-alive NoOp pings during long downloads

### JSON-RPC Enhanced

- **RPC-P1-01**: Batch requests (`system.multicall`)
- **RPC-P1-02**: `system.listMethods` for API discovery
- **RPC-P1-03**: `aria2.getGlobalStat` for aggregate stats
- **RPC-P1-04**: CORS support for browser clients (`--rpc-allow-origin`)

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| FTP parallel segment downloads | Protocol limitation — FTP has no byte-range support |
| SFTP parallel segment downloads | Protocol limitation — not analogous to HTTP ranges |
| FTP active mode | NAT-unfriendly, fails through firewalls, almost never works for end users |
| FTP directory/recursive download | Major scope expansion beyond single-file download |
| SFTP directory/recursive download | Major scope expansion beyond single-file download |
| FTP/SFTP upload | Different use case, different UX, different command surface |
| SSH agent support | Platform complexity, P1 enhancement per issue #139 |
| JSON-RPC rate limiting | Overkill for local daemon API |
| JSON-RPC batch requests | Rarely used, high complexity |
| OAuth2/JWT authentication | Inappropriate for local desktop tool |
| XML-RPC support | Legacy protocol, no modern tooling uses it |
| AriaNG web UI bundling | Downstream usage, not WarpDL's responsibility |
| Cross-protocol redirect following (HTTP → FTP) | Security risk, RFC violation |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| REDIR-01 | Phase 1 | Pending |
| REDIR-02 | Phase 1 | Pending |
| REDIR-03 | Phase 1 | Pending |
| REDIR-04 | Phase 1 | Pending |
| PROTO-01 | Phase 2 | Pending |
| PROTO-02 | Phase 2 | Pending |
| PROTO-03 | Phase 2 | Complete |
| FTP-01 | Phase 3 | Pending |
| FTP-02 | Phase 3 | Pending |
| FTP-03 | Phase 3 | Pending |
| FTP-04 | Phase 3 | Pending |
| FTP-05 | Phase 3 | Pending |
| FTP-06 | Phase 3 | Pending |
| FTP-07 | Phase 3 | Pending |
| FTP-08 | Phase 3 | Pending |
| SFTP-01 | Phase 4 | Pending |
| SFTP-02 | Phase 4 | Pending |
| SFTP-03 | Phase 4 | Pending |
| SFTP-04 | Phase 4 | Pending |
| SFTP-05 | Phase 4 | Pending |
| SFTP-06 | Phase 4 | Pending |
| SFTP-07 | Phase 4 | Pending |
| SFTP-08 | Phase 4 | Pending |
| SFTP-09 | Phase 4 | Pending |
| RPC-01 | Phase 5 | Pending |
| RPC-02 | Phase 5 | Pending |
| RPC-03 | Phase 5 | Pending |
| RPC-04 | Phase 5 | Pending |
| RPC-05 | Phase 5 | Pending |
| RPC-06 | Phase 5 | Pending |
| RPC-07 | Phase 5 | Pending |
| RPC-08 | Phase 5 | Pending |
| RPC-09 | Phase 5 | Pending |
| RPC-10 | Phase 5 | Pending |
| RPC-11 | Phase 5 | Pending |
| RPC-12 | Phase 5 | Pending |

**Coverage:**
- v1 requirements: 36 total
- Mapped to phases: 36
- Unmapped: 0

---
*Requirements defined: 2026-02-26*
*Last updated: 2026-02-26 after roadmap creation — all 36 requirements mapped*
