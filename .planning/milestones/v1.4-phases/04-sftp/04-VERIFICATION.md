---
phase: 04-sftp
verified: 2026-02-27
result: PASS
requirements-verified: [SFTP-01, SFTP-02, SFTP-03, SFTP-04, SFTP-05, SFTP-06, SFTP-07, SFTP-08, SFTP-09]
---

# Phase 4: SFTP -- Verification

## Success Criteria

### SC1 (SFTP-01, SFTP-02): Password auth downloads work

**Result: PASS**

Evidence:
- `pkg/warplib/protocol_sftp.go` -- sftpProtocolDownloader implements ProtocolDownloader
- buildAuthMethods: password from URL takes priority over key auth
- `pkg/warplib/protocol_sftp_test.go` -- 50+ tests with in-process mock SFTP server
- Factory registered in SchemeRouter for "sftp" scheme

### SC2 (SFTP-03): Default SSH key paths used

**Result: PASS**

Evidence:
- buildAuthMethods tries ~/.ssh/id_ed25519 then ~/.ssh/id_rsa as default paths
- Key auth used when no password in URL and key files exist
- Tests verify key auth fallback behavior

### SC3 (SFTP-07): TOFU host key verification

**Result: PASS**

Evidence:
- `pkg/warplib/known_hosts.go` -- TOFU callback, appendKnownHost, KnownHostsPath (~/.config/warpdl/known_hosts)
- `pkg/warplib/known_hosts_test.go` -- 7 TOFU test functions
- Auto-accepts unknown hosts silently (daemon is headless, no interactive prompt)
- Rejects changed keys with MITM error message
- knownhosts.Normalize handles port formatting (22 implicit, non-22 bracketed)

### SC4 (SFTP-06): Resume from byte offset

**Result: PASS**

Evidence:
- Resume() uses sftp.File.Seek(offset, io.SeekStart) for byte-offset resume
- Manager.ResumeDownload dispatches SFTP via SchemeRouter alongside FTP/FTPS
- Phase 6 fixed custom key persistence (Item.SSHKeyPath field persisted via GOB)

Note: Initial implementation in Phase 4; resume key persistence defect fixed in Phase 6 (Item.SSHKeyPath field).

### SC5 (SFTP-04, SFTP-08): --ssh-key flag, custom port via URL

**Result: PASS**

Evidence:
- SSHKeyPath threaded end-to-end: CLI --ssh-key -> warpcli -> DownloadParams -> API -> DownloaderOpts -> SFTP factory
- Port extracted from URL (sftp://user@host:2222/path)
- Phase 6 fixed SSHKeyPath persistence for resume across pause/resume cycles

Note: Initial implementation in Phase 4; SSHKeyPath persistence for resume defect fixed in Phase 6.

## Requirements Verification

| Requirement | Status | Evidence |
|-------------|--------|----------|
| SFTP-01 | PASS | sftp:// URLs dispatched through SchemeRouter to sftpProtocolDownloader |
| SFTP-02 | PASS | Password auth from URL userinfo; priority over key auth |
| SFTP-03 | PASS | Default key paths (~/.ssh/id_ed25519, ~/.ssh/id_rsa) tried automatically |
| SFTP-04 | PASS | --ssh-key flag threaded end-to-end; persistence fixed in Phase 6 |
| SFTP-05 | PASS | SupportsParallel=false, single-stream download enforced |
| SFTP-06 | PASS | sftp.File.Seek offset resume; key persistence fixed in Phase 6 |
| SFTP-07 | PASS | TOFU policy: auto-accept unknown, reject changed with MITM error |
| SFTP-08 | PASS | Port extracted from URL; non-22 ports supported |
| SFTP-09 | PASS | Probe() calls sftp.Stat, returns file size in ProbeResult.ContentLength |

## Gate Results

- All tests pass: YES (`go test ./...` -- 19 packages, 0 failures)
- Race detection: CLEAN (`go test -race -short ./...`)
- Build: CLEAN (`go build -ldflags="-w -s" .`)
- Vet: CLEAN (`go vet ./...`)

## Files Modified

### Plan 04-01 (SFTP downloader core + TOFU)
- `pkg/warplib/known_hosts.go` -- TOFU callback, appendKnownHost, KnownHostsPath
- `pkg/warplib/known_hosts_test.go` -- 7 TOFU test functions
- `pkg/warplib/protocol_sftp.go` -- sftpProtocolDownloader, buildAuthMethods, classifySFTPError
- `pkg/warplib/protocol_sftp_test.go` -- 25+ tests with in-process mock SFTP server
- `pkg/warplib/dloader.go` -- SSHKeyPath in DownloaderOpts
- `pkg/warplib/protocol_router.go` -- sftp factory registration
- `go.mod` / `go.sum` -- pkg/sftp v1.13.10, x/crypto v0.48.0

### Plan 04-02 (Manager resume dispatch)
- `pkg/warplib/manager.go` -- ResumeDownload ProtoSFTP wired alongside ProtoFTP/ProtoFTPS

### Plan 04-03 (API layer wiring + --ssh-key + tests)
- `internal/api/download.go` -- downloadProtocolHandler generalized for FTP/FTPS/SFTP
- `cmd/cmd.go` -- --ssh-key CLI flag
- `pkg/warpcli/ops.go` -- SSHKeyPath in DownloadParams
- CI gate: scripts/check_insecure_hostkey.sh rejects InsecureIgnoreHostKey outside test files

---
*Verified: 2026-02-27*
