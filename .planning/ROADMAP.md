# Roadmap: WarpDL Issues Fix Milestone

## Overview

This milestone adds four capabilities to WarpDL in dependency order: HTTP redirect following ships first (zero-dependency fix, unblocks all other work), protocol interface abstraction ships second (structural prerequisite for FTP/SFTP), FTP/FTPS and SFTP ship third and fourth (new single-stream downloaders), and JSON-RPC 2.0 ships last (exposes all protocols to external integrations). Every phase delivers a complete, verifiable capability with no partial features.

## Phases

**Phase Numbering:**
- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

- [x] **Phase 1: HTTP Redirect** - WarpDL transparently follows redirect chains for all HTTP/HTTPS downloads
- [x] **Phase 2: Protocol Interface** - Download engine supports pluggable protocol backends via a common interface (in progress — 1/2 plans done)
- [ ] **Phase 3: FTP/FTPS** - Users can download from ftp:// and ftps:// URLs with auth and resume
- [ ] **Phase 4: SFTP** - Users can download from sftp:// URLs with password/key auth and resume
- [ ] **Phase 5: JSON-RPC 2.0** - Daemon exposes JSON-RPC 2.0 API for programmatic control over HTTP/WebSocket

## Phase Details

### Phase 1: HTTP Redirect
**Goal**: WarpDL transparently follows HTTP redirect chains so users can download files from CDNs, URL shorteners, and any server that redirects before serving content
**Depends on**: Nothing (first phase)
**Requirements**: REDIR-01, REDIR-02, REDIR-03, REDIR-04
**Success Criteria** (what must be TRUE):
  1. User can run `warpdl <url>` on a URL that redirects through 301/302/303/307/308 and the file downloads successfully without manual intervention
  2. After a redirect chain resolves, all parallel segment requests use the final URL (not the original)
  3. If a redirect chain exceeds the configured max hops (default 10), the download fails with a clear "redirect loop" error message
  4. When a redirect crosses domains (cross-origin), the Authorization header is not forwarded to the new domain
**Plans**: TBD

Plans:
- [x] 01-01: Implement redirect following in HTTP client with final URL capture
- [x] 01-02: Add max hops enforcement and cross-origin header stripping, with tests

### Phase 2: Protocol Interface
**Goal**: The download engine has a protocol-agnostic interface so FTP and SFTP downloaders can plug in alongside the existing HTTP downloader without modifying the manager or API layers
**Depends on**: Phase 1
**Requirements**: PROTO-01, PROTO-02, PROTO-03
**Success Criteria** (what must be TRUE):
  1. A new download initiated with an `ftp://` or `sftp://` URL does not panic or return "unsupported scheme" — the manager routes it to the correct downloader
  2. Existing HTTP/HTTPS downloads behave identically to before this phase (zero regression)
  3. GOB-persisted downloads from before this phase load correctly after — backward-compatible zero value for the protocol field defaults to HTTP
**Plans**: TBD

Plans:
- [x] 02-01: Extract DownloaderI interface, add URL-scheme router, update Manager dispatch
- [ ] 02-02: Add PROTO-03 GOB compatibility test with fixture (must pass before any item.go merge)

### Phase 3: FTP/FTPS
**Goal**: Users can download files from FTP and FTPS servers with credential auth, passive mode, and resume support
**Depends on**: Phase 2
**Requirements**: FTP-01, FTP-02, FTP-03, FTP-04, FTP-05, FTP-06, FTP-07, FTP-08
**Success Criteria** (what must be TRUE):
  1. User can run `warpdl ftp://ftp.example.com/file.iso` and the file downloads (anonymous login by default)
  2. User can run `warpdl ftp://user:pass@ftp.example.com/file.iso` and the file downloads with credential auth
  3. If a download is interrupted, re-running the same command resumes from the byte offset rather than restarting from zero
  4. User can download from an FTPS server (explicit TLS) and the connection is encrypted
  5. Progress bar displays file size and download speed (file size fetched before transfer starts)
**Plans**: TBD

Plans:
- [ ] 03-01: Implement FTPDownloader struct with single-stream download, anonymous and credential auth, passive mode
- [ ] 03-02: Add FTP resume via RetrFrom offset and FTPS explicit TLS support
- [ ] 03-03: Add FTP tests with mock server; verify credentials are not persisted in stored URL

### Phase 4: SFTP
**Goal**: Users can download files from SFTP servers with password or SSH key authentication, TOFU host key verification, and resume support
**Depends on**: Phase 2
**Requirements**: SFTP-01, SFTP-02, SFTP-03, SFTP-04, SFTP-05, SFTP-06, SFTP-07, SFTP-08, SFTP-09
**Success Criteria** (what must be TRUE):
  1. User can run `warpdl sftp://user:pass@host/path` and the file downloads with password auth
  2. User can run `warpdl sftp://user@host/path` and the file downloads using the default SSH private key (`~/.ssh/id_rsa` or `~/.ssh/id_ed25519`)
  3. On first connection to a new host, the host key fingerprint is displayed and accepted; on subsequent connections, a changed key is rejected with a clear error (TOFU policy)
  4. If a download is interrupted, re-running the same command resumes from the byte offset
  5. User can specify a non-default SSH key with `--ssh-key /path/to/key` and a non-standard port with `sftp://user@host:2222/path`
**Plans**: TBD

Plans:
- [ ] 04-01: Design SFTP TOFU UX (known_hosts file, first-use accept flow, mismatch error, --sftp-insecure flag)
- [ ] 04-02: Implement SFTPDownloader with password and private key auth, custom port, TOFU host key policy
- [ ] 04-03: Add SFTP resume via Seek offset, --ssh-key flag, and tests; add CI gate that rejects InsecureIgnoreHostKey outside test files

### Phase 5: JSON-RPC 2.0
**Goal**: The daemon exposes a JSON-RPC 2.0 API over HTTP and WebSocket so external tools can control downloads programmatically without using the CLI
**Depends on**: Phase 2
**Requirements**: RPC-01, RPC-02, RPC-03, RPC-04, RPC-05, RPC-06, RPC-07, RPC-08, RPC-09, RPC-10, RPC-11, RPC-12
**Success Criteria** (what must be TRUE):
  1. `curl -s -X POST http://localhost:<port+1>/jsonrpc -d '{"jsonrpc":"2.0","method":"download.add","params":{"url":"...","token":"<secret>"},"id":1}'` starts a download and returns a download ID
  2. A WebSocket client connected to `ws://localhost:<port+1>/jsonrpc/ws` receives real-time progress notifications as downloads progress
  3. Any RPC request without the correct auth token receives a JSON-RPC error response (not a 200 with data)
  4. By default the endpoint is only reachable on localhost; `--rpc-listen-all` makes it reachable on all interfaces
  5. `download.status`, `download.list`, `download.pause`, `download.resume`, `download.remove`, and `system.getVersion` methods all return correct JSON-RPC 2.0 responses
  6. A malformed request (invalid JSON, missing method, unknown method) returns a standard JSON-RPC 2.0 error object with the correct error code
**Plans**: TBD

Plans:
- [ ] 05-01: Add JSON-RPC 2.0 HTTP endpoint with auth token and localhost binding to existing port+1 server
- [ ] 05-02: Implement method suite (download.add/pause/resume/remove/status/list, system.getVersion) as thin adapter over existing Api
- [ ] 05-03: Add WebSocket endpoint with real-time push notifications (started/progress/complete/error)
- [ ] 05-04: Add tests covering auth enforcement, error codes, WebSocket notifications, and localhost binding

## Progress

**Execution Order:**
Phases execute in numeric order: 1 → 2 → 3 → 4 → 5

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. HTTP Redirect | 2/2 | Complete | 2026-02-27 |
| 2. Protocol Interface | 1/2 | In Progress | - |
| 3. FTP/FTPS | 0/3 | Not started | - |
| 4. SFTP | 0/3 | Not started | - |
| 5. JSON-RPC 2.0 | 0/4 | Not started | - |
