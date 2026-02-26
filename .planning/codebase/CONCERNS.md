# Codebase Concerns

**Analysis Date:** 2025-02-26

## Tech Debt

**JavaScript URL Injection in Extension Module Runtime:**
- Issue: String concatenation with user input directly into JavaScript code
- Files: `internal/extl/module.go` line 114
- Code: `m.runtime.RunString(EXTRACT_CALLBACK + `("` + url + `")`)`
- Impact: Malicious URLs containing quotes or backticks could break JavaScript syntax or inject arbitrary code execution into the module's runtime
- Fix approach: Use JavaScript parameter passing or proper escaping. Create a `SetVar()` method on Runtime to safely inject the URL as a JavaScript variable, then invoke `extract(extractURL)` where extractURL is predefined
- Severity: **High** - User-controlled URLs directly interpolated into executable JavaScript

**GOB Serialization for Cookie Persistence:**
- Issue: Uses Go's `encoding/gob` package for cookie storage without version stability guarantees
- Files: `pkg/credman/manager.go` lines 48-73, 76-95
- Impact: Future changes to `types.Cookie` struct may break deserialization. GOB format is Go-specific and not stable across versions. Any field renames or type changes break existing cookie files
- Fix approach: Switch to JSON or Protocol Buffers for persistence. Add a migration path for existing .gob files
- Severity: **Medium** - Impacts production data persistence

**Direct HTTP Client Configuration Without Timeout Validation:**
- Issue: Downloader accepts custom HTTP client without validating timeout settings
- Files: `pkg/warplib/dloader.go` lines 30-34, 80-82
- Impact: If caller provides client without timeouts, requests may hang indefinitely. No guard against malicious servers that never respond
- Fix approach: Enforce minimum requestTimeout at Downloader creation. Add validation in NewDownloader to ensure timeouts are set if custom client is provided
- Severity: **Medium** - Can cause goroutine leaks and denial of service

**Work Stealing Algorithm Integer Division on Small Remainders:**
- Issue: Work stealing uses integer division `remaining / 2` without explicit handling of remainders
- Files: `pkg/warplib/worksteal.go` line 83
- Impact: Off-by-one errors possible when calculating stolen byte ranges. May cause parts to download overlapping or non-adjacent byte ranges in edge cases
- Fix approach: Add comprehensive unit tests for 2-byte, 3-byte remainders. Document rounding behavior explicitly
- Severity: **Low** - Unlikely to manifest in practice (5MB minimum threshold)

## Known Bugs

**Panic Recovery in Download Progress Callbacks:**
- Symptoms: Progress handler panics that should not crash the download process may propagate if panic recovery fails
- Files: `pkg/warplib/panic_recovery_test.go` shows the recovery mechanism exists, but coverage incomplete
- Trigger: Handler panic during `copyBufferChunk()` goroutine (line 228 in parts.go context)
- Status: Recovery implemented with defer+recover pattern, but needs verification across all callback types
- Workaround: Ensure progress handlers never panic internally
- Severity: **Low** - Recovery mechanism in place

**Config Directory Initialization Falls Back to Temp:**
- Symptoms: Silent fallback to `/tmp` when user config dir creation fails
- Files: `pkg/warplib/misc.go` lines 94-124
- Trigger: Permission denied in ~/.config or failure to create warpdl subdirectory
- Impact: Downloads persist in /tmp instead of user's expected location. Data loss on reboot if /tmp is cleared
- Fix approach: Fail loudly with actionable error. Allow environment variable override `WARPDL_CONFIG_DIR` before falling back
- Severity: **Medium** - Data loss risk
- Note: `WARPDL_CONFIG_DIR` env var already exists, but fallback happens silently

## Security Considerations

**Credential File Permissions Too Permissive:**
- Risk: Cookie file opened with mode 0666 (world-readable/writable)
- Files: `pkg/credman/manager.go` line 50
- Current mitigation: Encryption with AES-GCM before storage, but file permissions are wrong
- Recommendations:
  1. Change file mode to 0600 (owner read/write only)
  2. Add validation at manager creation to reject files with wrong permissions
  3. Log warning if insecure permissions detected on existing files
- Severity: **High** - Even encrypted cookies exposed to other local users

**Missing URL Validation in Batch Download:**
- Risk: Input file parsing only checks http/https scheme, doesn't validate URL structure
- Files: `cmd/input_file.go` lines 100-125
- Current mitigation: Basic scheme validation, but malformed URLs (missing host, invalid ports) not caught
- Recommendations:
  1. Use `net/url.Parse()` and validate result (Host, Scheme fields populated)
  2. Add validation for reserved/private IP ranges if batch downloads should skip them
  3. Report invalid URLs to stderr with line numbers for user correction
- Severity: **Medium** - Invalid URLs fail gracefully at HTTP layer, but UX poor

**OS Keyring Fallback Without Verification:**
- Risk: If OS keyring unavailable, system may use file-based storage silently
- Files: `pkg/credman/keyring/` implementation uses fallback mechanism
- Current mitigation: Logs warning, but no way for user to verify credentials are encrypted
- Recommendations:
  1. Add explicit mode indicator (keyring vs file-based) to manager
  2. Fail instead of silently degrading if keyring unavailable
  3. Allow environment variable to force file-based or keyring-only mode
- Severity: **Medium** - Degrades security posture silently

**Extension Module File Path Traversal Risk:**
- Risk: Module entrypoint path construction in `Load()` method
- Files: `internal/extl/module.go` lines 81, 82
- Current mitigation: Path joined from manifest.json Entrypoint field
- Recommendations:
  1. Validate Entrypoint field doesn't contain ".." or absolute paths
  2. Ensure final path is still within modulePath (use `filepath.Rel()` or `filepath.IsLocal()` Go 1.20+)
  3. Add unit test for traversal attempts
- Severity: **Medium** - Malicious extension manifest could load files outside module

## Performance Bottlenecks

**Large File Size Causes Single-Part Downloads:**
- Problem: Files >100GB default to single connection
- Files: `pkg/warplib/misc.go` lines 42-44
- Cause: DEF_MAX_FILE_SIZE = 100GB limit exists but should be configurable per-download
- Improvement path:
  1. Remove hardcoded limit or make configurable via DownloaderOpts
  2. Document rationale if limit is intentional
  3. Add option for ultra-large file handling (archive streaming, resumable segments)
- Severity: **Low** - Affects niche use case (>100GB downloads)

**GOB Encoding/Decoding in Cookie Manager on Every Operation:**
- Problem: SetCookie/UpdateCookie/DeleteCookie all call `saveCookies()` which re-encodes entire cookie map
- Files: `pkg/credman/manager.go` lines 76-95, 102-159
- Cause: No delta/patch encoding for individual cookie changes
- Improvement path:
  1. Batch multiple cookie operations before persist
  2. Add `StartTransaction()` / `CommitTransaction()` pattern
  3. Consider lazy persistence with dirty flag
- Current impact: Minor for typical usage (10-100 cookies), but compounds under load
- Severity: **Low** - Premature optimization, acceptable for current scale

**Work Stealing Calculation Adds Latency to Part Completion:**
- Problem: Work stealing logic runs on every completed part, involves atomic loads and comparisons
- Files: `pkg/warplib/worksteal.go` lines 23-107, 198-270
- Cause: Synchronous check during part completion before cleanup
- Impact: ~microseconds added per part on fast networks (10MB+/s speed threshold)
- Severity: **Low** - Overhead negligible, feature provides benefit

## Fragile Areas

**Downloader Resource Cleanup with Cascading Error Handling:**
- Files: `pkg/warplib/dloader.go` lines 1012-1031
- Why fragile: `Close()` calls `Stop()` which sets stopped flag and cancels context, then tries to close lw and f. Multiple error types aggregated with `errors.Join()`. If either Close() fails, error contains both, but caller may not distinguish between log file vs download file failure
- Safe modification:
  1. Test Close() in all states (before Start, during Start, after completion, after Stop)
  2. Log individual errors before aggregating
  3. Add "partial close" state if one resource closes successfully but other fails
- Test coverage: Resource lifecycle tests exist (`pkg/warplib/resource_lifecycle_test.go`) but should expand to concurrent Close/Stop scenarios
- Severity: **Medium** - Affects reliability of cleanup, not critical path

**Concurrent Part Download with Work Stealing Mutation:**
- Files: `pkg/warplib/worksteal.go` lines 109-118, 174-188, 198-270
- Why fragile: `activePartInfo.foff` is a pointer to part's final offset, modified during work stealing via `mu` lock. Concurrent downloads reading foff without snapshot risk seeing partial work steal
- Safe modification:
  1. Always snapshot foff value before using in calculations
  2. Document that foff can change mid-download due to work stealing
  3. Add invariant tests: foff never decreases below current read position
- Test coverage: `worksteal_test.go` has dedicated tests, but race conditions possible under high concurrency
- Severity: **Medium** - Design is careful but mutation pattern is fragile

**Manager State Persistence with GOB Encoded State:**
- Files: `pkg/warplib/manager.go` - persists download state to `userdata.warp` using GOB
- Why fragile: State file format tied directly to Manager struct. Any refactoring of Manager (adding fields, changing types) breaks resume capability for existing downloads
- Safe modification:
  1. Add versioning to state file format
  2. Implement migration functions before unmarshaling
  3. Test data compatibility across versions
  4. Document all breaking changes in changelog
- Test coverage: No version migration tests visible
- Severity: **Medium** - Affects resume reliability across updates

**Input File Parsing with Line Number Tracking in InvalidLines:**
- Files: `cmd/input_file.go` lines 84-111
- Why fragile: Line number tracking uses manual counter `i + 1`. If parsing logic changes (e.g., preserving blank lines), line numbers become unreliable for user reference
- Safe modification:
  1. Simplify - track line number only for invalid URLs
  2. Test with various line ending combinations (CRLF, LF, mixed)
  3. Consider file encoding detection (UTF-8 BOM, etc.)
- Test coverage: `input_file_test.go` covers basic cases but missing edge cases (empty file, only comments, non-UTF8)
- Severity: **Low** - UX issue, not functional bug

## Scaling Limits

**Maximum Connections Constraint:**
- Current capacity: Default 1 connection, can be set to int32 max
- Limit: System file descriptor limit (ulimit -n). Each connection consumes FD
- Scaling path:
  1. Document recommended max based on system limits
  2. Add pre-download check: validate fd count available
  3. Implement connection pooling if multiple sequential downloads use same config
- Files affected: `pkg/warplib/dloader.go` lines 46-47 (maxConn, numConn)
- Severity: **Low** - Users unlikely to exceed system FD limits for single download

**JavaScript Extension Runtime Isolation:**
- Current capacity: Each module gets isolated Goja runtime (~10-50MB per module)
- Limit: Memory exhaustion if many modules loaded or module has infinite loops
- Scaling path:
  1. Add memory limits to Goja runtime via interrupt handler
  2. Implement module unload/reload to free memory
  3. Add registry of currently loaded modules with memory usage tracking
- Files affected: `internal/extl/module.go` lines 72-79, `internal/extl/engine.go`
- Severity: **Low** - Extension system niche use case

**Disk Space Validation Only at Download Start:**
- Current capacity: Checks available disk space before download starts
- Limit: Space can fill after check but before completion (especially for large files)
- Scaling path:
  1. Periodic re-check during download (every 100MB or 30s)
  2. Graceful pause if space low
  3. Resume once space available
- Files affected: `pkg/warplib/diskspace_*.go` platform-specific implementations
- Severity: **Low** - Handles gracefully with error, no data corruption

## Dependencies at Risk

**Goja JavaScript Runtime (dop251/goja):**
- Risk: Maintained by single contributor, last update indicates active maintenance but community size small
- Usage: Core extension system relies on this for executing extension code
- Impact: Breaking changes in Goja could require major extension rewrite. Security issues in JS runtime affect WarpDL
- Mitigation:
  1. Pin version in go.mod (currently using specific version)
  2. Monitor release notes monthly
  3. Have fallback (disable extensions) if Goja breaks
- Migration plan: Switch to alternative JS runtime (otto, javy, etc.) if Goja unmaintained, but significant effort
- Severity: **Low** - Currently active, but single-contributor risk

**urfave/cli (CLI Framework):**
- Risk: Major version 1.x (v1.22.17), v2.x exists with incompatible API
- Usage: All CLI command routing and flag parsing
- Impact: Upgrading to v2 requires rewriting all command definitions
- Mitigation: Pin current version, v1 still receives maintenance patches
- Migration plan: Plan v2 migration as separate initiative (several days work)
- Severity: **Low** - v1 stable and maintained

**zalando/go-keyring (OS Credential Storage):**
- Risk: Depends on `godbus` (Linux D-Bus), `github.com/danieljoos/wincred` (Windows)
- Usage: Stores HTTP cookies and credentials securely via OS keyring
- Impact: If keyring unavailable (server without D-Bus, restricted environments), falls back to file-based storage (see Security section)
- Mitigation: Fallback mechanism exists, but silent degradation is concerning
- Severity: **Medium** - Fallback lacks user visibility

## Missing Critical Features

**No Download Queue Prioritization:**
- Problem: Multiple sequential downloads process FIFO, no user control over priority
- Blocks: Power users cannot deprioritize slow/stalled downloads
- Improvement: Add priority field to download options, implement priority queue in manager
- Severity: **Low** - Works correctly, just not optimal

**No Partial Retry Configuration:**
- Problem: Retry logic hard-coded exponential backoff, no user control
- Blocks: Users with rate-limited servers cannot customize retry strategy
- Improvement: Add RetryConfig struct with customizable backoff, max retries, timeout
- Status: RetryConfig exists but not exposed to CLI
- Severity: **Low** - Defaults reasonable for most cases

**Missing Resume on Different System:**
- Problem: Resume downloads use part file hashes that are process-specific
- Blocks: Cannot resume on different machine or after OS reinstall
- Architecture: Part files stored with process-specific naming scheme
- Severity: **Low** - Limitation documented, acceptable tradeoff

## Test Coverage Gaps

**Extension Module Entrypoint Validation:**
- What's not tested: Malicious entrypoint values (.., /, absolute paths) don't break out of module directory
- Files: `internal/extl/module.go` lines 81-87 (no unit test for path validation)
- Risk: Manifest could load ../../../etc/passwd or system files
- Priority: **High** - Security test needed
- Estimated coverage: 0% (no traversal tests)

**Credential File Permission Validation:**
- What's not tested: Cookie manager rejects files with 0666 permissions
- Files: `pkg/credman/manager.go` line 50 (no permission check)
- Risk: Accepts insecure file modes without warning
- Priority: **High** - Security validation missing
- Estimated coverage: 0%

**URL Validation Edge Cases in Batch Download:**
- What's not tested: IPv6 URLs, URLs with ports, international domain names, query string edge cases
- Files: `cmd/input_file.go` lines 100-125 (only scheme validation)
- Risk: Valid URLs rejected or invalid URLs accepted with poor error messages
- Priority: **Medium** - User-facing validation
- Estimated coverage: ~30% (basic http/https check only)

**Work Stealing Calculation Edge Cases:**
- What's not tested: Remainders <2MB (below threshold), negative bytesRead scenarios, corruption cases
- Files: `pkg/warplib/worksteal.go` lines 46-88 (calculateStealWork function)
- Risk: Off-by-one errors in edge case byte ranges
- Priority: **Medium** - Affects download correctness
- Estimated coverage: ~70% (main path tested, edges less so)

**Config Directory Fallback Scenarios:**
- What's not tested: Permission denied on ~/.config/warpdl creation, /tmp unavailable, WARPDL_CONFIG_DIR set to invalid path
- Files: `pkg/warplib/misc.go` lines 94-124 (fallback logic)
- Risk: Silent failures, downloads lost to wrong directory
- Priority: **Medium** - Data integrity concern
- Estimated coverage: ~40% (basic init path works, fallbacks not tested)

**Concurrent Close/Stop on Downloader:**
- What's not tested: Multiple goroutines calling Close() simultaneously, Close() during active download, Stop() then Close()
- Files: `pkg/warplib/dloader.go` lines 1003-1031 (Close and Stop methods)
- Risk: Panic or resource leak if multiple threads call Close()
- Priority: **Medium** - Resource safety
- Estimated coverage: ~50% (basic close tested, concurrent scenarios not)

---

*Concerns audit: 2025-02-26*
