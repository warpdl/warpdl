---
phase: 04-sftp
plan: 01
subsystem: core
tags: [sftp, ssh, tofu, known_hosts, x/crypto, pkg/sftp]

# Dependency graph
requires:
  - phase: 02-protocol-interface
    provides: ProtocolDownloader interface, SchemeRouter, DownloaderFactory
  - phase: 03-ftp-ftps
    provides: StripURLCredentials, classifyError pattern, ftpProgressWriter pattern
provides:
  - sftpProtocolDownloader implementing ProtocolDownloader for sftp:// URLs
  - TOFU host key verification subsystem (known_hosts.go)
  - SSH password and key auth methods (buildAuthMethods)
  - SchemeRouter sftp factory registration
  - SSHKeyPath field in DownloaderOpts
  - In-process mock SFTP server test infrastructure
affects: [04-02-sftp-resume, 04-03-sftp-api]

# Tech tracking
tech-stack:
  added: [github.com/pkg/sftp v1.13.10, golang.org/x/crypto v0.48.0 (direct)]
  patterns: [TOFU host key callback, in-process SSH/SFTP mock server, credential stripping for sftp://]

key-files:
  created:
    - pkg/warplib/known_hosts.go
    - pkg/warplib/known_hosts_test.go
    - pkg/warplib/protocol_sftp.go
    - pkg/warplib/protocol_sftp_test.go
  modified:
    - pkg/warplib/dloader.go
    - pkg/warplib/protocol_router.go
    - go.mod
    - go.sum

key-decisions:
  - "TOFU auto-accepts unknown hosts silently (daemon is headless, no interactive prompt)"
  - "Known hosts file isolated at ~/.config/warpdl/known_hosts (not system ~/.ssh/known_hosts)"
  - "Resume fully implemented in 04-01 (not deferred to 04-02) since pattern mirrors FTP exactly"
  - "Mock SFTP server uses real filesystem paths (not sftp.ReadOnly/WithServerWorkingDirectory)"
  - "Password auth takes priority over key auth when both available"
  - "Passphrase-protected SSH keys return clear error (not supported, no interactive prompt)"

patterns-established:
  - "TOFU callback: re-reads known_hosts on every call (no caching) for concurrent connection visibility"
  - "In-process mock SFTP server: ecdsa key gen + ssh.ServerConfig + sftp.NewServer on channel"
  - "mockSFTPResult helper: fsRoot-relative paths for test file creation and URL construction"

requirements-completed: [SFTP-01, SFTP-02, SFTP-03, SFTP-04, SFTP-05, SFTP-06, SFTP-07, SFTP-08, SFTP-09]

# Metrics
duration: 45min
completed: 2026-02-27
---

# Plan 04-01: Core SFTP Downloader Summary

**SFTP single-stream downloader with TOFU host key verification, password/key SSH auth, and in-process mock server tests**

## Performance

- **Duration:** ~45 min
- **Completed:** 2026-02-27
- **Tasks:** 7 phases (A-G)
- **Files created:** 4
- **Files modified:** 4

## Accomplishments
- sftpProtocolDownloader fully implements ProtocolDownloader with Probe, Download, and Resume
- TOFU known_hosts subsystem auto-accepts unknown hosts, rejects changed keys with MITM warning
- SSH auth chain: password from URL > explicit SSH key > default key paths (~/.ssh/id_ed25519, ~/.ssh/id_rsa)
- In-process mock SFTP server enables realistic testing without network dependencies
- 50+ test functions covering factory, auth, probe, download, resume, error classification, TOFU, routing

## Task Commits

1. **Dependencies** - `1312bfb` (chore: add SFTP dependencies)
2. **Tests** - `b938f2b` (test: add SFTP protocol downloader and TOFU known_hosts tests)
3. **Implementation** - `40c0cb4` (feat: implement SFTP protocol downloader with TOFU host key verification)

## Files Created/Modified
- `pkg/warplib/known_hosts.go` - TOFU host key callback, appendKnownHost, KnownHostsPath
- `pkg/warplib/known_hosts_test.go` - 7 test functions for TOFU policy
- `pkg/warplib/protocol_sftp.go` - sftpProtocolDownloader, buildAuthMethods, classifySFTPError
- `pkg/warplib/protocol_sftp_test.go` - 25+ test functions with in-process mock SFTP server
- `pkg/warplib/dloader.go` - Added SSHKeyPath to DownloaderOpts
- `pkg/warplib/protocol_router.go` - Registered sftp factory in SchemeRouter
- `go.mod` / `go.sum` - pkg/sftp v1.13.10, x/crypto v0.48.0

## Decisions Made
- TOFU auto-accepts silently because daemon is headless (no tty for interactive prompt)
- Resume implemented in 04-01 rather than deferring to 04-02 since the pattern is identical to FTP
- Mock SFTP server serves real filesystem (not sftp.ReadOnly) to avoid path resolution issues
- knownhosts.Normalize handles port formatting (22 implicit, non-22 bracketed)

## Deviations from Plan

### Auto-fixed Issues

**1. Resume implemented early**
- **Found during:** Phase E (protocol_sftp.go implementation)
- **Issue:** Plan specified Resume as stub returning "not yet implemented" for 04-02
- **Fix:** Implemented full Resume since it's identical to FTP pattern (seek-based offset)
- **Verification:** TestSFTPResumeFlow passes all subtests (partial file, all compiled, no file)

---

**Total deviations:** 1 (scope expansion, not creep -- reduces 04-02 work)
**Impact on plan:** Positive -- 04-02 can focus on Manager.ResumeDownload SFTP dispatch instead

## Issues Encountered
- Mock SFTP server path resolution: initially used relative paths but sftp.NewServer serves real filesystem. Fixed by using absolute paths via mockSFTPResult helper.
- SSH key PEM encoding: ssh.MarshalPrivateKey returns *pem.Block, needed pem.EncodeToMemory for proper encoding.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- sftpProtocolDownloader.Resume() is already implemented (not a stub)
- Plan 04-02 can focus on Manager.ResumeDownload SFTP dispatch and protocol guard
- Plan 04-03 needs API handler (downloadSFTPHandler), --ssh-key CLI flag

---
*Phase: 04-sftp, Plan: 01*
*Completed: 2026-02-27*
