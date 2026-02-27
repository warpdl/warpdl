# Phase 7: Verification & Documentation Closure - Research

**Researched:** 2026-02-27
**Domain:** Documentation/verification closure (no code changes)
**Confidence:** HIGH

## Summary

This is a pure documentation phase. All code is implemented, all tests pass, and all 36 requirements are functionally wired. The gaps are entirely process/documentation: missing VERIFICATION.md files, missing SUMMARY files, and a REQUIREMENTS.md traceability table that was never updated past Phase 2.

The v1.0 milestone audit confirmed this precisely: only 2/36 requirements are formally satisfied by 3-source cross-reference (VERIFICATION.md + SUMMARY frontmatter + traceability table). Post-Phase-6, the correct target is 36/36 because Phase 6 fixed the 3 integration defects that were blocking full verification.

This phase creates no code. It creates/updates exactly these document classes:
1. VERIFICATION.md files for Phases 1, 3, 4, 5 (Phase 2 and 6 already have them)
2. SUMMARY files for Phase 3 (3 missing: 03-01, 03-02, 03-03) and Phase 5 (3 missing: 05-01, 05-02, 05-03)
3. SUMMARY frontmatter fixes for Phase 1 (01-01, 01-02) and Phase 5 (05-04)
4. REQUIREMENTS.md traceability table: flip 29 rows from Pending -> Complete, flip 29 checkboxes from [ ] -> [x]
5. PROTO-02 in Phase 2 VERIFICATION.md: update from PARTIAL -> SATISFIED

**Primary recommendation:** Work phase-by-phase. Each phase gets its VERIFICATION.md written from evidence in existing code/commits/SUMMARYs, then SUMMARY frontmatter is fixed, then REQUIREMENTS.md is updated. Do not skip any phase or try to batch across phases.

---

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| REDIR-01 | User can download files behind HTTP 301/302/303/307/308 redirects transparently | Phase 1 code verified by audit (redirect.go, dloader.go fetchInfo, commit e194548). No VERIFICATION.md exists yet. |
| REDIR-02 | Downloader tracks and uses final URL after redirect chain for all segment requests | Same Phase 1 evidence. fetchInfo() captures d.url = resp.Request.URL.String(). |
| REDIR-03 | Redirect chain is limited to configurable max hops (default 10) with clear error on loop | RedirectPolicy enforces max hops. TestFTPSExplicitTLS and TestFTPResume cover retry; redirect_test.go covers loop detection. |
| PROTO-02 | Manager dispatches to correct downloader based on URL scheme (http/https/ftp/ftps/sftp) | Phase 2 VERIFICATION.md marks PARTIAL — was accurate at Phase 2 time. Post-Phase-3/4, ftp/ftps/sftp factories are all registered. Now SATISFIED. |
| FTP-01 | User can download files from ftp:// URLs | ftpProtocolDownloader + SchemeRouter + API dispatch (commit e053d93, 9e61af2). No VERIFICATION.md or SUMMARY exists. |
| FTP-02 | Anonymous FTP login is used by default when no credentials in URL | newFTPProtocolDownloader defaults user/password to anonymous/anonymous. Audit confirms WIRED. |
| FTP-03 | User can authenticate with username/password via URL | URL credential extraction in factory. TestFTPCredentialAuth passes. |
| FTP-04 | FTP uses passive mode (EPSV/PASV) by default | jlaffaye/ftp defaults to EPSV. No explicit config needed. |
| FTP-05 | FTP downloads are single-stream (no parallel segments) | Capabilities() returns {SupportsParallel: false}. GetMaxConnections()=1. |
| FTP-06 | User can resume interrupted FTP downloads via REST/RetrFrom offset | Resume() uses RetrFrom. Manager.ResumeDownload dispatches FTP via SchemeRouter. |
| FTP-07 | User can download from FTPS servers with explicit TLS | DialWithExplicitTLS used for ftps:// URLs. |
| FTP-08 | File size is reported before download starts for progress tracking | Probe() calls FileSize, returns in ProbeResult.ContentLength. DownloadResponse carries ContentLength. |
| SFTP-01 | User can download files from sftp:// URLs | sftpProtocolDownloader + SchemeRouter + API dispatch. Phase 4 SUMMARY frontmatter lists it. No VERIFICATION.md. |
| SFTP-02 | User can authenticate with password via URL | buildAuthMethods: password from URL takes priority. SUMMARY frontmatter lists it. |
| SFTP-03 | User can authenticate with SSH private key file (default keys) | buildAuthMethods tries ~/.ssh/id_ed25519, ~/.ssh/id_rsa as defaults. SUMMARY frontmatter lists it. |
| SFTP-05 | SFTP downloads are single-stream (no parallel segments) | Capabilities() returns {SupportsParallel: false}. SUMMARY frontmatter lists it. |
| SFTP-07 | Host key verification uses TOFU policy | known_hosts.go implements TOFU. SUMMARY frontmatter lists it. |
| SFTP-08 | Custom port support via URL | URL parsing extracts port. SUMMARY frontmatter lists it. |
| SFTP-09 | File size is reported before download starts | Probe() uses sftp.Stat().Size(). SUMMARY frontmatter lists it. |
| RPC-01 | Daemon exposes JSON-RPC 2.0 endpoint over HTTP at /jsonrpc | internal/server/rpc.go registers /jsonrpc. 05-04-SUMMARY body covers it. No VERIFICATION.md. |
| RPC-02 | Daemon exposes WebSocket endpoint at /jsonrpc/ws | internal/server/rpc.go registers /jsonrpc/ws. 05-04-SUMMARY body covers it. |
| RPC-03 | Auth token required for all RPC requests | requireToken middleware. 05-04-SUMMARY body covers it. |
| RPC-04 | RPC binds to localhost only by default | localhost binding in rpc setup. 05-04-SUMMARY body covers it. |
| RPC-05 | download.add method accepts URL and options, starts download | Implemented in rpc_methods.go. 05-04-SUMMARY body covers it. |
| RPC-07 | download.remove method removes download from queue | Implemented in rpc_methods.go. 05-04-SUMMARY body covers it. |
| RPC-08 | download.status method returns download state | Implemented in rpc_methods.go. 05-04-SUMMARY body covers it. |
| RPC-09 | download.list method returns downloads filtered by state | Implemented in rpc_methods.go. 05-04-SUMMARY body covers it. |
| RPC-10 | system.getVersion method returns daemon version info | Implemented in rpc_methods.go. 05-04-SUMMARY body covers it. |
| RPC-12 | Standard JSON-RPC 2.0 error codes | Error codes implemented. 05-04-SUMMARY body covers it. |
</phase_requirements>

---

## Current State Inventory

### VERIFICATION.md Status

| Phase | File | Status | Notes |
|-------|------|--------|-------|
| 1: HTTP Redirect | `.planning/phases/01-http-redirect/01-VERIFICATION.md` | MISSING | Must create |
| 2: Protocol Interface | `.planning/phases/02-protocol-interface/02-VERIFICATION.md` | EXISTS (gaps_found) | Update PROTO-02 from PARTIAL -> SATISFIED |
| 3: FTP/FTPS | `.planning/phases/03-ftp-ftps/03-VERIFICATION.md` | MISSING | Must create |
| 4: SFTP | `.planning/phases/04-sftp/04-VERIFICATION.md` | MISSING | Must create |
| 5: JSON-RPC 2.0 | `.planning/phases/05-json-rpc-20/05-VERIFICATION.md` | MISSING | Must create |
| 6: Fix Defects | `.planning/phases/06-fix-integration-defects/06-VERIFICATION.md` | EXISTS (PASS) | No changes needed |

### SUMMARY File Status

| Phase | Plan | File | Status | Frontmatter |
|-------|------|------|--------|-------------|
| 1 | 01-01 | `.planning/phases/01-http-redirect/01-01-SUMMARY.md` | EXISTS | requirements-completed: MISSING (empty) |
| 1 | 01-02 | `.planning/phases/01-http-redirect/01-02-SUMMARY.md` | EXISTS | requirements-completed: MISSING (empty) |
| 2 | 02-01 | `.planning/phases/02-protocol-interface/02-01-SUMMARY.md` | EXISTS | requirements-completed: [PROTO-01, PROTO-02] ✓ |
| 2 | 02-02 | `.planning/phases/02-protocol-interface/02-02-SUMMARY.md` | EXISTS | requirements-completed: [PROTO-03] ✓ (assumed) |
| 3 | 03-01 | `.planning/phases/03-ftp-ftps/03-01-SUMMARY.md` | MISSING | Must create |
| 3 | 03-02 | `.planning/phases/03-ftp-ftps/03-02-SUMMARY.md` | MISSING | Must create |
| 3 | 03-03 | `.planning/phases/03-ftp-ftps/03-03-SUMMARY.md` | MISSING | Must create |
| 4 | 04-01 | `.planning/phases/04-sftp/04-01-SUMMARY.md` | EXISTS | requirements-completed: [SFTP-01..09 except SFTP-06] ✓ |
| 4 | 04-02 | `.planning/phases/04-sftp/04-02-SUMMARY.md` | EXISTS | requirements-completed: [SFTP-06] ✓ |
| 4 | 04-03 | `.planning/phases/04-sftp/04-03-SUMMARY.md` | EXISTS | requirements-completed: [SFTP-01..09 subset] ✓ |
| 5 | 05-01 | `.planning/phases/05-json-rpc-20/05-01-SUMMARY.md` | MISSING | Must create |
| 5 | 05-02 | `.planning/phases/05-json-rpc-20/05-02-SUMMARY.md` | MISSING | Must create |
| 5 | 05-03 | `.planning/phases/05-json-rpc-20/05-03-SUMMARY.md` | MISSING | Must create |
| 5 | 05-04 | `.planning/phases/05-json-rpc-20/05-04-SUMMARY.md` | EXISTS | requirements-completed: EMPTY (body text covers all RPC reqs) |
| 6 | 06-01 | `.planning/phases/06-fix-integration-defects/06-01-SUMMARY.md` | EXISTS | requirements-completed: [SFTP-04, SFTP-06, REDIR-04] ✓ |
| 6 | 06-02 | `.planning/phases/06-fix-integration-defects/06-02-SUMMARY.md` | EXISTS | requirements-completed: [RPC-06, RPC-11] ✓ (assumed) |

### REQUIREMENTS.md Traceability Status

29 rows need updating: all Phase 1 (REDIR-01..03), all Phase 3 (FTP-01..08), all Phase 4 SFTP (SFTP-01..03, SFTP-05, SFTP-07..09), all Phase 5 RPC (RPC-01..05, RPC-07..10, RPC-12). Also 29 body checkboxes need `[ ]` -> `[x]`.

Note: REDIR-04, SFTP-04, SFTP-06, RPC-06, RPC-11 are already `[x]` in body but show "Pending" in traceability table — they should flip to Complete too (were remapped to Phase 6).

Coverage line needs to read `36/36` (currently describes the total as mapped but status column still says Pending for most).

---

## Document Format Templates

### VERIFICATION.md Format

Based on `06-VERIFICATION.md` (simpler, cleaner) and `02-VERIFICATION.md` (detailed):

**Use `06-VERIFICATION.md` as the primary template** — it is the most recent and cleaner format:

```markdown
---
phase: {phase-slug}
verified: {date}
result: PASS
requirements-verified: [{REQ-IDs}]
---

# Phase N: {Name} -- Verification

## Success Criteria

### SC1: {success criterion from ROADMAP}

**Result: PASS**

Evidence:
- {specific code file/function/test evidence}
- {specific test name that verifies it}

### SC2: ...

## Requirements Verification

| Requirement | Status | Evidence |
|-------------|--------|----------|
| REQ-XX | PASS | {specific evidence} |

## Gate Results

- All tests pass: YES (`go test -race -short ./...` -- 0 failures)
- Race detection: CLEAN
- Build: CLEAN (`go build -ldflags="-w -s" .`)
- Vet: CLEAN (`go vet ./...`)

## Files Modified/Created

### Plan {NN-NN}
- `path/to/file.go` -- description

---
*Verified: {date}*
```

**Key rules for VERIFICATION.md content:**
- `result` frontmatter field: `PASS` (not `gaps_found` — phases are all complete now)
- Evidence must cite specific files, function names, test names from the actual code
- Pull evidence from: existing SUMMARY body text, commit messages, audit WIRED status
- Gate results must reflect actual final state (all packages pass, race clean, build clean)
- Do NOT invent evidence — trace from what commits/SUMMARYs say was built

### SUMMARY Frontmatter Format

Based on `02-01-SUMMARY.md` (richest example) and `04-01-SUMMARY.md`:

```yaml
---
phase: {phase-slug}
plan: {number}
subsystem: {core|api|cli|daemon}
tags: [{relevant tags}]

requires:
  - phase: {dependency}
    provides: "{what it provides}"
provides:
  - "{exported capability}"

tech-stack:
  added: [{new dependencies}]
  patterns: [{patterns used}]

key-files:
  created:
    - path/to/file.go
  modified:
    - path/to/file.go

key-decisions:
  - "{decision made}"

requirements-completed: [{REQ-IDs covered by this specific plan}]

duration: {Nmin}
completed: {date}
---
```

**For Phase 1 plans (incomplete frontmatter):** Add `requirements-completed` key to existing frontmatter. Phase 01-01 covers REDIR-01, REDIR-02, REDIR-03. Phase 01-02 covers REDIR-04.

**For Phase 3 plans (missing entirely):** Create full SUMMARY files synthesized from PLAN file content and commit messages:
- 03-01-SUMMARY: `requirements-completed: [FTP-01, FTP-02, FTP-03, FTP-04, FTP-05, FTP-08]` (from PLAN requirements field + commit e053d93 message — includes Resume and FTPS consolidated)
- 03-02-SUMMARY: `requirements-completed: [FTP-06, FTP-07]` (from PLAN requirements + commit 28eab3c)
- 03-03-SUMMARY: `requirements-completed: [FTP-01, FTP-03, FTP-05, FTP-08]` (API layer wiring — from PLAN requirements)

**For Phase 5 plans (missing entirely):** Create SUMMARY files from commits:
- 05-01-SUMMARY: `requirements-completed: [RPC-01, RPC-03, RPC-04]` (HTTP endpoint, auth, localhost — commit a1484b2)
- 05-02-SUMMARY: `requirements-completed: [RPC-05, RPC-07, RPC-08, RPC-09, RPC-10, RPC-12]` (method suite — commit 2df0db3)
- 05-03-SUMMARY: `requirements-completed: [RPC-02, RPC-11]` (WebSocket + push notifications — commit b62f9cf)

**For Phase 5 05-04-SUMMARY (incomplete frontmatter):** Add `requirements-completed: [RPC-01, RPC-02, RPC-03, RPC-04, RPC-05, RPC-07, RPC-08, RPC-09, RPC-10, RPC-12]` — all RPC reqs covered by the integration test plan (RPC-06 and RPC-11 will have defects noted but were fixed in Phase 6).

---

## Evidence Sources for Each Phase

### Phase 1 Evidence (for VERIFICATION.md)

**Commits:**
- `cb4cedf` — test(01-01): add failing tests for HTTP redirect following and URL capture
- `ac7cfdf` — core: feat: implement HTTP redirect following with final URL capture
- `e194548` — core: feat: cross-origin header stripping on redirect and client hardening

**Code files:**
- `pkg/warplib/redirect.go` — RedirectPolicy, ErrTooManyRedirects, StripUnsafeFromHeaders
- `pkg/warplib/dloader.go` — fetchInfo() captures d.url after redirect; NewDownloader sets CheckRedirect
- `pkg/warplib/proxy.go` — All 3 NewHTTPClient* functions set CheckRedirect
- `pkg/warplib/redirect_test.go` — 20+ test cases

**Success criteria mapping (from ROADMAP):**
- SC1 (REDIR-01): User downloads files behind redirects → redirect.go + fetchInfo()
- SC2 (REDIR-02): Final URL used for all segment requests → d.url = resp.Request.URL.String()
- SC3 (REDIR-03): Max hops enforced with clear error → RedirectPolicy(DefaultMaxRedirects) + ErrTooManyRedirects
- SC4 (REDIR-04): Authorization headers not leaked cross-origin → StripUnsafeFromHeaders + fetchInfo cross-origin detection + web.go fix (Phase 6)

**Gate results:** Audit confirmed all 19 packages pass, race clean.

### Phase 3 Evidence (for VERIFICATION.md and SUMMARYs)

**Commits:**
- `e053d93` — Plan 03-01: ftpProtocolDownloader + SchemeRouter + Manager.AddProtocolDownload
- `28eab3c` — Plan 03-02: Manager.ResumeDownload FTP dispatch + integrity guard
- `9e61af2` — Plan 03-03: API layer FTP dispatch + daemon_core.go SchemeRouter init
- `74c97e0` — api: test: add FTP download handler tests for coverage

**Code files (confirmed existing):**
- `pkg/warplib/protocol_ftp.go` — ftpProtocolDownloader, StripURLCredentials, patchProtocolHandlers
- `pkg/warplib/protocol_ftp_test.go` — 48+ test cases with fclairamb/ftpserverlib mock server
- `pkg/warplib/protocol_router.go` — ftp/ftps factories registered in NewSchemeRouter
- `pkg/warplib/manager.go` — AddProtocolDownload, patchProtocolHandlers, SetSchemeRouter, ResumeDownload FTP dispatch
- `internal/api/api.go` — schemeRouter field added, NewApi signature updated
- `internal/api/download.go` — downloadFTPHandler, downloadHTTPHandler extracted
- `cmd/daemon_core.go` — SchemeRouter init, manager.SetSchemeRouter, NewApi with router

**Success criteria mapping (from ROADMAP):**
- SC1 (FTP-01, FTP-02, FTP-03): Anonymous and credential auth downloads
- SC2 (FTP-03): URL credential auth with ftp://user:pass@host/path
- SC3 (FTP-06): Resume via RetrFrom offset — file byte offset from disk
- SC4 (FTP-07): FTPS via DialWithExplicitTLS
- SC5 (FTP-08): File size in DownloadResponse.ContentLength from Probe

**Note on consolidation:** Commit e053d93 implemented ALL of 03-01 PLUS the Resume() method and FTPS TLS path that were planned for 03-02. Commit 28eab3c focused on Manager.ResumeDownload dispatch only. The SUMMARY files should reflect this accurately — 03-01 completed more than its planned scope.

### Phase 4 Evidence (for VERIFICATION.md only — SUMMARYs exist)

All 3 SUMMARY files exist with correct frontmatter. Only VERIFICATION.md is missing.

**Code files:**
- `pkg/warplib/known_hosts.go` — TOFU callback, appendKnownHost, KnownHostsPath
- `pkg/warplib/protocol_sftp.go` — sftpProtocolDownloader, buildAuthMethods, classifySFTPError
- `pkg/warplib/protocol_sftp_test.go` — 50+ tests with in-process mock SFTP server
- `pkg/warplib/known_hosts_test.go` — 7 TOFU test functions

**Success criteria mapping (from ROADMAP):**
- SC1 (SFTP-01, SFTP-02): Password auth downloads work
- SC2 (SFTP-03): Default SSH key paths used
- SC3 (SFTP-07): TOFU policy — accept first, reject changed key with MITM error
- SC4 (SFTP-06): Resume works (Phase 6 also fixed custom key persistence)
- SC5 (SFTP-04, SFTP-08): --ssh-key flag, port 2222 in URL

**Defect resolved:** The original audit found SFTP-04/SFTP-06 defect (custom key lost on resume). Phase 6 fixed this with Item.SSHKeyPath persistence. Phase 4 VERIFICATION.md must note the fix came in Phase 6.

### Phase 5 Evidence (for VERIFICATION.md and SUMMARYs)

**Commits:**
- `a1484b2` — Plan 05-01: JSON-RPC 2.0 HTTP endpoint with auth + localhost binding
- `2df0db3` — Plan 05-02: download.* method suite
- `b62f9cf` — Plan 05-03: WebSocket endpoint + push notifications
- `fc4312a` — Plan 05-04: Integration tests + race condition fixes

**Code files:**
- `internal/server/rpc.go` (or rpc_handler.go) — /jsonrpc and /jsonrpc/ws routes
- `internal/server/rpc_methods.go` — all RPC methods (download.add/pause/resume/remove/status/list, system.getVersion)
- `internal/server/rpc_integration_test.go` — 14 end-to-end integration tests
- `internal/server/rpc_methods_test.go` — unit tests

**Requirements split across plans:**
- 05-01 (a1484b2): RPC-01 (HTTP endpoint), RPC-03 (auth token), RPC-04 (localhost binding)
- 05-02 (2df0db3): RPC-05 (download.add), RPC-07 (download.remove), RPC-08 (download.status), RPC-09 (download.list), RPC-10 (system.getVersion), RPC-12 (error codes)
- 05-03 (b62f9cf): RPC-02 (WebSocket), RPC-11 (push notifications)
- 05-04 (fc4312a): Integration verification + race fixes (covers all RPC reqs via integration tests)

**Defects resolved:** RPC-06 and RPC-11 had defects (resumed downloads got no push notifications). Phase 6 fixed this. Phase 5 VERIFICATION.md must note the fix came in Phase 6.

---

## Work Order for Planning

The plan should structure work in this exact sequence to minimize redundancy:

**Wave 1: Create missing SUMMARY files** (Phase 3 and Phase 5)
These must come first because the VERIFICATION.md files reference them.

- Task 7-01: Create 03-01-SUMMARY.md, 03-02-SUMMARY.md, 03-03-SUMMARY.md
- Task 7-02: Create 05-01-SUMMARY.md, 05-02-SUMMARY.md, 05-03-SUMMARY.md

**Wave 2: Fix incomplete SUMMARY frontmatter** (Phase 1, Phase 5/05-04)

- Task 7-03: Add `requirements-completed` to 01-01-SUMMARY.md and 01-02-SUMMARY.md
- Task 7-04: Add `requirements-completed` to 05-04-SUMMARY.md

**Wave 3: Create missing VERIFICATION.md files** (Phases 1, 3, 4, 5)

- Task 7-05: Create 01-VERIFICATION.md (Phase 1 — REDIR-01, REDIR-02, REDIR-03, REDIR-04)
- Task 7-06: Create 03-VERIFICATION.md (Phase 3 — FTP-01 through FTP-08)
- Task 7-07: Create 04-VERIFICATION.md (Phase 4 — SFTP-01..09)
- Task 7-08: Create 05-VERIFICATION.md (Phase 5 — RPC-01..12)

**Wave 4: Update existing VERIFICATION.md** (Phase 2 PROTO-02 fix)

- Task 7-09: Update 02-VERIFICATION.md PROTO-02 from PARTIAL -> SATISFIED

**Wave 5: Update REQUIREMENTS.md** (traceability table + coverage count)

- Task 7-10: Update REQUIREMENTS.md traceability table (29 rows + 29 checkboxes + coverage count)

---

## Critical Accuracy Notes for Planner

### Phase 3 SUMMARY consolidation note
Commit e053d93 (Plan 03-01) actually implemented Resume() and FTPS TLS in addition to the Download() path — the commit message explicitly says this. So 03-01-SUMMARY should include FTP-06 and FTP-07 in its requirements-completed, and 03-02-SUMMARY should note that Resume() was already implemented by 03-01 and only Manager.ResumeDownload dispatch was added in 03-02. Cross-reference against PLAN files' `requirements` fields:
- 03-01-PLAN: requirements: [FTP-01, FTP-02, FTP-03, FTP-04, FTP-05, FTP-08]
- 03-02-PLAN: requirements: [FTP-06, FTP-07]
- 03-03-PLAN: requirements: [FTP-01, FTP-03, FTP-05, FTP-08]

The actual commit consolidated Resume into 03-01. The SUMMARY should reflect what was actually built (use commit message as truth), not just what the PLAN said it would build. This means 03-01-SUMMARY.md should claim [FTP-01, FTP-02, FTP-03, FTP-04, FTP-05, FTP-06, FTP-07, FTP-08] since Resume and FTPS were consolidated.

### PROTO-02 update in Phase 2 VERIFICATION.md
The existing 02-VERIFICATION.md at row 11 (SC1) shows FAILED because ftp/sftp were not yet registered at Phase 2 time. In the Requirements Coverage table, PROTO-02 is marked PARTIAL. After Phase 3/4 shipped, PROTO-02 is fully satisfied. The update is surgical: change PARTIAL → SATISFIED and add note "functionally complete after Phase 3/4 shipped ftp/ftps/sftp factories."

The frontmatter also needs updating:
- `status: gaps_found` → `status: re_verified_complete` (or similar)
- `re_verification: false` → `re_verification: true`
- Add `re_verified: 2026-02-27` field

### REQUIREMENTS.md traceability table rows to update
Current state shows these as Pending. All need `Complete` status:

Phase 1 (already [x] in body but table says Pending):
- REDIR-01, REDIR-02, REDIR-03 → Phase 1 → Complete
- REDIR-04 is already Complete (remapped to Phase 6)

Phase 3 ([ ] in body AND Pending in table):
- FTP-01..FTP-08 → Phase 3 → Complete; flip [ ] → [x]

Phase 4 ([ ] in body AND Pending in table):
- SFTP-01..SFTP-03, SFTP-05, SFTP-07..SFTP-09 → Phase 4 → Complete; flip [ ] → [x]
- SFTP-04 is already [x], remapped to Phase 6
- SFTP-06 is already [x], remapped to Phase 6

Phase 5 ([ ] in body AND Pending in table):
- RPC-01..RPC-05, RPC-07..RPC-10, RPC-12 → Phase 5 → Complete; flip [ ] → [x]
- RPC-06 is already [x], remapped to Phase 6
- RPC-11 is already [x], remapped to Phase 6

Coverage line must change to: `36/36 Complete` (or equivalent)

---

## Common Pitfalls

### Pitfall 1: Inventing evidence
**What goes wrong:** Writing VERIFICATION.md with fabricated file paths, test names, or line numbers that don't exist in the codebase.
**How to avoid:** Pull all evidence from: (1) existing SUMMARY body text, (2) git commit messages, (3) PLAN file must_haves/artifacts sections, (4) audit WIRED status. If in doubt, say "Integration audit confirms WIRED" rather than citing a specific line number you haven't verified.

### Pitfall 2: Wrong requirements-completed in SUMMARY frontmatter
**What goes wrong:** Assigning requirements to the wrong plan — e.g., putting FTP-06 in 03-01-SUMMARY when the plan only officially targeted it in 03-02.
**How to avoid:** Use the commit message as ground truth for what was actually implemented per plan. The commit for e053d93 explicitly states it consolidated Resume, so 03-01-SUMMARY may legitimately claim FTP-06. But also ensure no requirement is claimed by zero plans — every FTP requirement must appear in at least one 03-XX-SUMMARY.

### Pitfall 3: Marking PROTO-02 as SATISFIED without noting Phase 6 context
**What goes wrong:** PROTO-02 update in Phase 2 VERIFICATION.md looks like revisionism without context.
**How to avoid:** Add a clear note: "Updated 2026-02-27 post-Phase-3/4: ftp/ftps/sftp factories now registered. PROTO-02 is fully satisfied."

### Pitfall 4: Missing the 05-04-SUMMARY frontmatter fix
**What goes wrong:** Creating 05-01/02/03 SUMMARYs but forgetting that 05-04-SUMMARY.md also has an empty `requirements-completed` field.
**How to avoid:** The 05-04-SUMMARY covers integration tests — it verified all 12 RPC reqs but the frontmatter has no `requirements-completed`. This needs adding too.

### Pitfall 5: Wrong total in REQUIREMENTS.md coverage
**What goes wrong:** Changing the traceability rows to Complete but not updating the "Coverage: X/36" summary line.
**How to avoid:** After updating all rows, update the coverage summary to `36/36 Complete`.

---

## Architecture Patterns

This phase involves only Markdown/YAML file creation and editing. No Go code patterns apply.

**File naming conventions (confirmed from existing files):**
- VERIFICATION: `{XX}-VERIFICATION.md` (single file per phase, e.g., `01-VERIFICATION.md`)
- SUMMARY: `{XX}-{YY}-SUMMARY.md` (one per plan, e.g., `03-01-SUMMARY.md`)
- Both live in `.planning/phases/{NN}-{phase-name}/`

**YAML frontmatter delimiters:** `---` start and `---` end (standard YAML front matter). Existing files confirm this.

**Requirements list format in YAML:** `[REQ-01, REQ-02]` (inline array, no quotes).

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead |
|---------|-------------|-------------|
| Evidence for code existence | Manual code inspection of every file | Audit report WIRED status + existing SUMMARY body text + commit messages |
| Test names in VERIFICATION.md | Inventing test function names | Pull from existing SUMMARY body text where they're explicitly listed |
| File modification lists | Guessing | Pull from PLAN's `files_modified` / `key-files` sections |

---

## Validation Architecture

The `config.json` does not have a `workflow.nyquist_validation` key — it only has `workflow.research`, `plan_check`, `verifier`, `auto_advance`. Nyquist validation is not configured. Skip Validation Architecture section.

---

## Sources

### Primary (HIGH confidence)
- `.planning/v1.0-MILESTONE-AUDIT.md` — audit findings, gap classification, wiring status
- `.planning/phases/06-fix-integration-defects/06-VERIFICATION.md` — template for VERIFICATION.md format
- `.planning/phases/02-protocol-interface/02-VERIFICATION.md` — alternative (detailed) VERIFICATION.md format
- `.planning/phases/02-protocol-interface/02-01-SUMMARY.md` — template for complete SUMMARY frontmatter
- `.planning/phases/04-sftp/04-01-SUMMARY.md` — another complete SUMMARY example
- Git log — commit messages confirm what was implemented per plan and when
- PLAN files — confirm `requirements` field (planned scope) and `files_modified` (artifacts)

### Secondary (MEDIUM confidence)
- Existing SUMMARY body text — describes what was implemented; frontmatter incomplete but body is accurate
- ROADMAP success criteria — defines what each phase must achieve

---

## Metadata

**Confidence breakdown:**
- What files need to be created/updated: HIGH — verified directly from file system and audit
- Template format: HIGH — directly observed from existing VERIFICATION.md and SUMMARY files
- Evidence content for VERIFICATION.md: HIGH — derived from audit WIRED status + commit messages + existing SUMMARY bodies
- Requirements-completed assignments per plan: HIGH — cross-referenced against PLAN `requirements` fields and commit messages
- Traceability table update scope: HIGH — verified each row in REQUIREMENTS.md

**Research date:** 2026-02-27
**Valid until:** Indefinite — pure documentation phase, no external dependencies
