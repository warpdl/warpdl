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
- [x] **Phase 3: FTP/FTPS** - Users can download from ftp:// and ftps:// URLs with auth and resume
- [x] **Phase 4: SFTP** - Users can download from sftp:// URLs with password/key auth and resume
- [x] **Phase 5: JSON-RPC 2.0** - Daemon exposes JSON-RPC 2.0 API for programmatic control over HTTP/WebSocket
- [x] **Phase 6: Fix Integration Defects** - Fix 3 code defects: SFTP resume key loss, RPC resume notifications, web.go CheckRedirect
- [x] **Phase 7: Verification & Documentation Closure** - Write missing VERIFICATIONs, SUMMARYs, fix traceability for all 29 stale requirements
- [x] **Phase 8: Fix RPC FTP/SFTP Download Add Handlers** - Wire missing notifier handlers in RPC download.add FTP/SFTP path
- [ ] **Phase 9: Fix RPC FTP/SFTP Resume Handler Pass-Through** - Fix Item.Resume() nil handler pass to ProtocolDownloader.Resume() for FTP/SFTP
- [ ] **Phase 10: SUMMARY Frontmatter Backfill** - Add missing requirements-completed frontmatter to 6 SUMMARY files

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
  1. The manager has a scheme router with a `Register()` method — FTP/SFTP downloaders (Phases 3/4) can plug in without modifying the manager or API layers
  2. Existing HTTP/HTTPS downloads behave identically to before this phase (zero regression)
  3. GOB-persisted downloads from before this phase load correctly after — backward-compatible zero value for the protocol field defaults to HTTP
**Plans**: TBD

Plans:
- [x] 02-01: Extract DownloaderI interface, add URL-scheme router, update Manager dispatch
- [x] 02-02: Add PROTO-03 GOB compatibility test with fixture (must pass before any item.go merge)

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
- [x] 03-01: Implement FTPDownloader struct with single-stream download, anonymous and credential auth, passive mode
- [x] 03-02: Add FTP resume via RetrFrom offset and FTPS explicit TLS support
- [x] 03-03: Add FTP tests with mock server; verify credentials are not persisted in stored URL

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
- [x] 04-01: Implement core SFTP downloader with TOFU host key verification, password/key auth, SchemeRouter registration
- [x] 04-02: Wire Manager.ResumeDownload for SFTP protocol dispatch alongside FTP/FTPS
- [x] 04-03: Add SFTP resume via Seek offset, --ssh-key flag, and tests; add CI gate that rejects InsecureIgnoreHostKey outside test files

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
- [x] 05-01: Add JSON-RPC 2.0 HTTP endpoint with auth token and localhost binding to existing port+1 server
- [x] 05-02: Implement method suite (download.add/pause/resume/remove/status/list, system.getVersion) as thin adapter over existing Api
- [x] 05-03: Add WebSocket endpoint with real-time push notifications (started/progress/complete/error)
- [x] 05-04: Integration tests and CI gate (race-free, 80%+ coverage, build verification)

### Phase 6: Fix Integration Defects
**Goal**: Fix 3 integration defects identified by milestone audit that break user-facing flows (SFTP resume with custom key, RPC resume push notifications, web.go redirect policy)
**Depends on**: Phase 4, Phase 5
**Requirements**: SFTP-04, SFTP-06, RPC-06, RPC-11, REDIR-04
**Gap Closure:** Closes defects from v1.0 audit
**Success Criteria** (what must be TRUE):
  1. SFTP download started with `--ssh-key /custom/key` can be resumed and still uses the custom key (not default)
  2. RPC `download.resume` delivers push notifications (progress/complete/error) to connected WebSocket clients
  3. `web.go processDownload` creates `http.Client` with explicit `CheckRedirect` matching Phase 1 redirect policy

Plans:
- [x] 06-01: Fix SFTP resume SSH key persistence (Item.SSHKeyPath field, threading through AddProtocolDownload/ResumeDownload) and web.go redirect policy enforcement
- [x] 06-02: Fix RPC download.resume push notification wiring and gate verification

### Phase 7: Verification & Documentation Closure
**Goal**: Close all documentation and verification gaps so every phase has a VERIFICATION.md, all SUMMARY files exist with correct frontmatter, and REQUIREMENTS.md traceability is accurate
**Depends on**: Phase 6
**Requirements**: REDIR-01, REDIR-02, REDIR-03, PROTO-02, FTP-01, FTP-02, FTP-03, FTP-04, FTP-05, FTP-06, FTP-07, FTP-08, SFTP-01, SFTP-02, SFTP-03, SFTP-05, SFTP-07, SFTP-08, SFTP-09, RPC-01, RPC-02, RPC-03, RPC-04, RPC-05, RPC-07, RPC-08, RPC-09, RPC-10, RPC-12
**Gap Closure:** Closes verification/documentation gaps from v1.0 audit
**Success Criteria** (what must be TRUE):
  1. Every phase (1-6) has a VERIFICATION.md with pass/fail per requirement
  2. All SUMMARY files exist with correct `requirements-completed` frontmatter
  3. REQUIREMENTS.md traceability table shows Complete + [x] for all 36 requirements
  4. Phase 2 PROTO-02 updated from partial → passed (functionally complete after Phase 3/4)
  5. Coverage count in REQUIREMENTS.md reads 36/36

Plans:
- [x] 07-01: Create missing SUMMARY files (Phase 3, Phase 5) and fix incomplete SUMMARY frontmatter (Phase 1, Phase 5/05-04)
- [x] 07-02: Create missing VERIFICATION.md files (Phases 1, 3, 4, 5), update Phase 2 PROTO-02, update REQUIREMENTS.md traceability

### Phase 8: Fix RPC FTP/SFTP Download Add Handlers
**Goal**: Wire missing notifier handlers in RPC `download.add` FTP/SFTP code path so WebSocket push notifications are delivered and item progress is persisted during FTP/SFTP downloads started via JSON-RPC
**Depends on**: Phase 5
**Requirements**: RPC-05, RPC-11
**Gap Closure:** Closes INT-01 tech debt from v1.0 audit
**Success Criteria** (what must be TRUE):
  1. FTP/SFTP downloads started via JSON-RPC `download.add` emit WebSocket push notifications (started/progress/complete/error)
  2. `item.Downloaded` is updated during FTP/SFTP downloads started via RPC (not just at completion)

Plans:
- [x] 08-01: Fix nil handler pass in rpc_methods.go downloadAdd FTP/SFTP branch, add integration test

### Phase 9: Fix RPC FTP/SFTP Resume Handler Pass-Through
**Goal**: Fix `Item.Resume()` to pass handlers through to `ProtocolDownloader.Resume()` so FTP/SFTP downloads resumed via JSON-RPC deliver WebSocket push notifications
**Depends on**: Phase 8
**Requirements**: RPC-06, RPC-11
**Gap Closure:** Closes INT-02 from v1.0 audit
**Success Criteria** (what must be TRUE):
  1. FTP/SFTP downloads resumed via JSON-RPC `download.resume` emit WebSocket push notifications (progress/complete/error)
  2. `Item.Resume()` passes patched handlers to `ProtocolDownloader.Resume()` instead of `nil`
  3. Existing HTTP resume path remains unaffected (no regression)

Plans:
- [ ] 09-01: TBD

### Phase 10: SUMMARY Frontmatter Backfill
**Goal**: Add missing `requirements-completed` frontmatter to 6 SUMMARY files so all requirements have 3-source cross-reference coverage
**Depends on**: Phase 9
**Requirements**: PROTO-01, PROTO-03, SFTP-04, SFTP-06, RPC-06, RPC-11
**Gap Closure:** Closes documentation tech debt from v1.0 audit
**Success Criteria** (what must be TRUE):
  1. SUMMARY files 02-01, 02-02, 04-01, 04-02, 04-03, 06-01 all have `requirements-completed` in YAML frontmatter
  2. Every v1 requirement appears in at least one SUMMARY frontmatter
  3. Audit 3-source cross-reference shows 0 "missing" entries

Plans:
- [ ] 10-01: TBD

## Progress

**Execution Order:**
Phases execute in numeric order: 1 → 2 → 3 → 4 → 5 → 6 → 7 → 8 → 9 → 10

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. HTTP Redirect | 2/2 | Complete | 2026-02-27 |
| 2. Protocol Interface | 2/2 | Complete    | 2026-02-27 |
| 3. FTP/FTPS | 3/3 | Complete | 2026-02-27 |
| 4. SFTP | 3/3 | Complete | 2026-02-27 |
| 5. JSON-RPC 2.0 | 4/4 | Complete | 2026-02-27 |
| 6. Fix Integration Defects | 2/2 | Complete | 2026-02-27 |
| 7. Verification & Doc Closure | 2/2 | Complete | 2026-02-27 |
| 8. Fix RPC FTP/SFTP Handlers | 1/1 | Complete | 2026-02-27 |
| 9. Fix RPC FTP/SFTP Resume Handler | 0/1 | Pending | - |
| 10. SUMMARY Frontmatter Backfill | 0/1 | Pending | - |
