---
phase: 03-ftp-ftps
verified: 2026-02-27
result: PASS
requirements-verified: [FTP-01, FTP-02, FTP-03, FTP-04, FTP-05, FTP-06, FTP-07, FTP-08]
---

# Phase 3: FTP/FTPS -- Verification

## Success Criteria

### SC1 (FTP-01, FTP-02): User can download from ftp:// with anonymous auth by default

**Result: PASS**

Evidence:
- `pkg/warplib/protocol_ftp.go` -- newFTPProtocolDownloader defaults to anonymous/anonymous when no credentials in URL
- `pkg/warplib/protocol_ftp_test.go` -- 48+ test cases with ftpserverlib mock server
- Factory function registered in SchemeRouter for "ftp" and "ftps" schemes

### SC2 (FTP-03): User can download with ftp://user:pass@host/path credential auth

**Result: PASS**

Evidence:
- URL credential extraction in factory function parses userinfo from URL
- TestFTPCredentialAuth passes with mock server verifying correct credentials
- StripURLCredentials removes credentials from stored URL (security)

### SC3 (FTP-06): Resume works via RetrFrom offset

**Result: PASS**

Evidence:
- Resume() uses RetrFrom(path, offset) for byte-offset resume
- Manager.ResumeDownload dispatches FTP via SchemeRouter (protocol guard in integrity validation)
- Resume offset derived from destination file size on disk (WarpStat), not from parts map
- Resume tests with partial file scenarios pass

### SC4 (FTP-07): FTPS with explicit TLS works

**Result: PASS**

Evidence:
- DialWithExplicitTLS used for ftps:// URLs with tls.Config{ServerName: hostname}
- Factory function detects "ftps" scheme and sets TLS flag
- FTPS tests verify TLS connection establishment

### SC5 (FTP-04, FTP-05, FTP-08): Passive mode, single stream, file size

**Result: PASS**

Evidence:
- jlaffaye/ftp defaults to EPSV (passive mode) -- no additional configuration needed
- Capabilities() returns SupportsParallel=false, MaxConnections=1 (single-stream enforced at type level)
- Probe() calls FTP SIZE command, returns in ProbeResult.ContentLength for progress tracking

## Requirements Verification

| Requirement | Status | Evidence |
|-------------|--------|----------|
| FTP-01 | PASS | ftp:// URLs dispatched through SchemeRouter to ftpProtocolDownloader |
| FTP-02 | PASS | Anonymous auth default (anonymous/anonymous) when no URL credentials |
| FTP-03 | PASS | URL userinfo parsed for username/password authentication |
| FTP-04 | PASS | jlaffaye/ftp uses EPSV (passive mode) by default |
| FTP-05 | PASS | SupportsParallel=false, MaxConnections=1 enforced |
| FTP-06 | PASS | RetrFrom(path, offset) for byte-offset resume; Manager dispatch wired |
| FTP-07 | PASS | DialWithExplicitTLS for ftps:// with proper TLS config |
| FTP-08 | PASS | Probe() calls SIZE command, returns ContentLength |

## Gate Results

- All tests pass: YES (`go test ./...` -- 19 packages, 0 failures)
- Race detection: CLEAN (`go test -race -short ./...`)
- Build: CLEAN (`go build -ldflags="-w -s" .`)
- Vet: CLEAN (`go vet ./...`)

## Files Modified

### Plan 03-01 (FTP downloader core)
- `pkg/warplib/protocol_ftp.go` -- ftpProtocolDownloader, factory, Probe/Download/Resume, classifyFTPError, StripURLCredentials
- `pkg/warplib/protocol_ftp_test.go` -- 48+ test cases with mock FTP server
- `pkg/warplib/protocol_router.go` -- ftp/ftps factory registration
- `pkg/warplib/manager.go` -- AddProtocolDownload, patchProtocolHandlers
- `go.mod` / `go.sum` -- github.com/jlaffaye/ftp v0.2.0

### Plan 03-02 (Manager resume dispatch)
- `pkg/warplib/manager.go` -- ResumeDownload FTP/FTPS dispatch, protocol guard, SetSchemeRouter
- `pkg/warplib/protocol_ftp.go` -- Resume flow adjustments
- `pkg/warplib/protocol_ftp_test.go` -- Resume dispatch tests

### Plan 03-03 (API layer wiring)
- `internal/api/api.go` -- schemeRouter field, NewApi signature update
- `internal/api/download.go` -- downloadHTTPHandler + downloadFTPHandler split
- `internal/api/download_ftp_test.go` -- FTP handler tests, GOB credential round-trip test
- `internal/api/resume.go` -- FTP/FTPS resume dispatch
- `internal/api/api_test.go` -- Updated for new NewApi signature
- `cmd/daemon_core.go` -- SchemeRouter initialization

---
*Verified: 2026-02-27*
