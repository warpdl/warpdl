# Implementation Plan: Download Scheduling & Browser Cookie Import

**Branch**: `001-scheduling-cookie-import` | **Date**: 2026-02-28 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/001-scheduling-cookie-import/spec.md`
**Audit**: [spec-audit.md](checklists/spec-audit.md) — 70 items addressed below

## Summary

Add download scheduling (one-shot and recurring) and browser cookie import to WarpDL. Scheduling extends the existing `Item` struct with optional fields and adds a new scheduler goroutine in the daemon. Cookie import adds a new `internal/cookies/` package that reads Firefox/Chrome/Netscape cookie stores and injects cookies as HTTP headers. Both features integrate through the existing daemon architecture and CLI framework with no new external databases or wire protocol changes.

## Technical Context

**Language/Version**: Go 1.24.9+ (CGO_ENABLED=0)
**Primary Dependencies**: `modernc.org/sqlite` v1.46.1 (pure-Go SQLite, BSD-3-Clause), `adhocore/gronx` (cron parser, MIT), existing `urfave/cli` v1
**Storage**: GOB-encoded files (`~/.config/warpdl/userdata.warp`) — extended with new `Item` fields. SQLite read-only for browser cookie databases.
**Testing**: `go test` with 80%+ coverage per package, race detection, E2E tests with `//go:build e2e`
**Target Platform**: Linux (amd64/arm64/386/arm), macOS (amd64/arm64), Windows (amd64/386/arm64), FreeBSD/OpenBSD/NetBSD (best-effort)
**Project Type**: CLI + daemon (download manager)
**Performance Goals**: Cookie import < 2s for 10k cookies (SC-005), schedule trigger within 60s of target time (SC-001)
**Constraints**: No CGO, no external runtime dependencies, cross-platform binary
**Scale/Scope**: Single-user daemon, hundreds of scheduled items, cookie stores up to 10k entries

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Evidence |
|-----------|--------|----------|
| I. Cross-Platform First | PASS | Cookie paths defined for macOS, Linux, Windows. Browser path resolution uses `runtime.GOOS` switch (existing pattern at `internal/nativehost/manifest.go:76-113`). `modernc.org/sqlite` is pure-Go (CGO_ENABLED=0 safe). Platform-specific cookie paths isolated in `internal/cookies/paths_{unix,windows}.go`. |
| II. Proven Libraries | PASS | `modernc.org/sqlite` (2,562+ importers, BSD-3-Clause) for SQLite reading. `adhocore/gronx` (MIT, zero deps) for cron parsing. Both justified in research.md. |
| III. Test-First Development | PASS | All new packages (`internal/cookies/`, `internal/scheduler/`) will follow strict TDD. 80%+ coverage enforced. |
| IV. Package Isolation | PASS | Cookie import → `internal/cookies/` (private, platform-specific). Scheduler → `internal/scheduler/` (private, daemon-only). Item extensions → `pkg/warplib/item.go` (existing struct). No cross-package coupling. |
| V. Daemon Architecture | PASS | Scheduler goroutine runs in daemon process. CLI remains stateless — sends scheduling params via existing socket protocol. State persisted through Manager (GOB). |
| VI. Performance & Reliability | PASS | Scheduler uses min-heap + 60s max-sleep-cap for bounded latency. Cookie import copies SQLite to temp for lock safety. Missed schedules detected and enqueued on restart. |
| VII. Simplicity & YAGNI | PASS | No new abstraction layers. Item struct extended directly (no separate entity). Scheduler is a single goroutine, not a framework. No feature flags. |

**Post-Phase 1 Re-check**: All principles remain satisfied. Two new `internal/` packages are justified by single-responsibility (cookies ≠ scheduling ≠ download engine). No constitution violations.

## Project Structure

### Documentation (this feature)

```text
specs/001-scheduling-cookie-import/
├── plan.md              # This file
├── research.md          # Phase 0 output — dependency decisions, schemas, paths
├── data-model.md        # Phase 1 output — entity diagrams, state machines
├── quickstart.md        # Phase 1 output — usage examples
├── contracts/
│   └── cli-commands.md  # Phase 1 output — CLI flag spec, wire protocol
└── checklists/
    ├── requirements.md  # Pre-planning gate checklist
    └── spec-audit.md    # Full artifact audit (70 items)
```

### Source Code (repository root)

```text
pkg/warplib/
├── item.go              # MODIFY — add ScheduledAt, CronExpr, ScheduleState, CookieSourcePath fields
├── manager.go           # MODIFY — add schedule-aware item queries
└── item_test.go         # MODIFY — test new fields, GOB round-trip

internal/cookies/
├── types.go             # NEW — Cookie, CookieSource, CookieFormat types
├── import.go            # NEW — ImportCookies(path, domain) entry point
├── detect.go            # NEW — format detection (SQLite magic bytes, table names, Netscape header)
├── firefox.go           # NEW — moz_cookies SQLite parser
├── chrome.go            # NEW — Chrome cookies SQLite parser (unencrypted only)
├── netscape.go          # NEW — Netscape text format parser
├── paths.go             # NEW — browser path constants and auto-detection interface
├── paths_unix.go        # NEW — macOS/Linux browser paths (//go:build unix)
├── paths_windows.go     # NEW — Windows browser paths (//go:build windows)
├── copy.go              # NEW — safe SQLite copy (file + WAL + SHM) to temp
├── import_test.go       # NEW — integration tests with test fixtures
├── detect_test.go       # NEW — format detection tests
├── firefox_test.go      # NEW — Firefox parser tests
├── chrome_test.go       # NEW — Chrome parser tests
├── netscape_test.go     # NEW — Netscape parser tests
├── paths_unix_test.go   # NEW — platform path tests
└── paths_windows_test.go # NEW — platform path tests

internal/scheduler/
├── types.go             # NEW — ScheduleEvent type
├── scheduler.go         # NEW — scheduler goroutine with min-heap
├── heap.go              # NEW — min-heap implementation for ScheduleEvent
├── scheduler_test.go    # NEW — scheduler logic tests
└── heap_test.go         # NEW — heap tests

cmd/
├── download.go          # MODIFY — add --start-at, --start-in, --schedule, --cookies-from flags
└── download_test.go     # MODIFY — flag validation tests

common/
└── types.go             # MODIFY — add StartAt, StartIn, Schedule, CookiesFrom to DownloadParams

internal/api/
└── download.go          # MODIFY — hook cookie import + schedule check into download flow

internal/daemon/
└── runner.go            # MODIFY — instantiate scheduler goroutine

tests/e2e/
└── schedule_cookie_test.go  # NEW — E2E tests (//go:build e2e)
```

**Structure Decision**: Follows existing Go project layout. New `internal/` packages for domain-isolated functionality (cookies and scheduling). Core model extensions in `pkg/warplib/`. CLI extensions in `cmd/`. No new top-level directories.

## Audit Response Matrix

This section maps every item from `checklists/spec-audit.md` to a design decision, artifact update, or explicit deferral.

### Requirement Completeness (CHK001–CHK012)

| ID | Issue | Resolution | Artifact |
|----|-------|------------|----------|
| CHK001 | Daemon not running at submission time | CLI already errors when daemon is unreachable. No schedule can be created without daemon. Existing behavior is correct — no spec change needed. | N/A (existing) |
| CHK002 | Recurring download URL returns different file | FR-009a already mandates timestamp suffix on filename. Each trigger is independent. URL returning different content is handled normally — the timestamp suffix prevents collision. | N/A (spec covers) |
| CHK003 | Cookie source metadata in `warpdl list` | **Design decision**: Add `Cookies` column to list output showing browser name when cookies are configured. Format: `Firefox` or `cookies.txt` or `—`. | contracts |
| CHK004 | Max concurrent recurring schedules | **Design decision**: Unbounded. Queue concurrency cap (FR-010) naturally throttles. No artificial limit on schedule count. Document in plan, no spec change. | plan |
| CHK005 | Recurring download failure → next cron trigger | **Design decision**: YES, next cron occurrence still triggers regardless of previous failure. Each occurrence is independent. Failed runs retain their failure status; new occurrence creates new download attempt. | contracts |
| CHK006 | `warpdl stop` mid-download for recurring | Already specified: FR-008 + clarification say stop permanently cancels entire schedule. Spec and contracts already aligned. | N/A (spec covers) |
| CHK007 | Multiple Firefox profiles in auto-detect | Research.md §4 already specifies: parse `profiles.ini`, check `[Install*]` sections for `Default=`, fall back to `[Profile*]` with `Default=1`. Use first default profile found. | N/A (research covers) |
| CHK008 | Cookie re-import triggers | FR-023 + FR-024 cover resume, retry, recurring. Work-steal doesn't respawn segments — it redistributes byte ranges within the same download session (cookies already in-memory). No gap. | N/A (spec covers) |
| CHK009 | Speed limit format formal grammar | P3 feature — deferred. No design work in this plan. | deferred (P3) |
| CHK010 | `--schedule` + `--start-at`/`--start-in` | Already in contracts/cli-commands.md §Flag Interactions. **Add to spec FR-009**: "MAY be combined with `--start-at` or `--start-in` to delay the first occurrence." | spec addendum, contracts |
| CHK011 | Notification format for missed schedules | **Design decision**: On daemon restart, log `"Missed schedule: {name} (was {time}), starting now"` to daemon log. CLI `warpdl list` shows `missed` status with `"was {time} (starting now)"`. | contracts |
| CHK012 | `--cookies-from` with directory path | **Design decision**: Reject with error: `"error: {path} is a directory, expected a cookie file path or 'auto'"`. Validate with `os.Stat().IsDir()` before processing. | contracts |

### Requirement Clarity (CHK013–CHK020)

| ID | Issue | Resolution | Artifact |
|----|-------|------------|----------|
| CHK013 | "Within 60 seconds" precision | 0–60s late. Scheduler's max-sleep-cap is 60s, so worst case the trigger fires 60s after the target time. Typically much sooner (when heap head is close, sleep is exactly `time.Until(target)`). | plan (documented here) |
| CHK014 | "Clear error message" format | Follow existing codebase pattern: `"error: <description>"` or `"warning: <description>"`. No structural format spec — implementer discretion matching existing style. | plan (documented here) |
| CHK015 | "Documented priority order" location | In `--cookies-from` flag help text and in `warpdl download --help` output. Also documented in spec and quickstart. | contracts |
| CHK016 | Domain matching precision | Exact domain match (`example.com`) OR dot-prefixed subdomain match (`.example.com`) OR wildcard suffix match (`%.example.com`). This covers: the domain itself, cookies set for the domain, and all subdomains. Does NOT match parent domains (cookies for `example.com` don't apply to `sub.example.com` unless dot-prefixed). Consistent with research.md SQL queries. | data-model |
| CHK017 | Non-interactive past-time `--start-at` | **Design decision**: Always start immediately with a warning. No interactive prompt. The spec's "prompt for confirmation" is downgraded to "warn and start immediately" since WarpDL is primarily used in scripted/daemon contexts. | contracts, spec addendum |
| CHK018 | `--start-in` duration format | Go's `time.ParseDuration` is the parser. Supported units: `h` (hours), `m` (minutes), `s` (seconds), and compounds like `1h30m`. Days (`d`) NOT supported — users must use `24h`. Document in help text. | contracts |
| CHK019 | "First valid cookie store" definition | "Valid" = file exists AND is readable AND has correct format (SQLite magic bytes with expected table, or Netscape header). Does NOT check for target domain cookies — that check happens after format detection. | plan (documented here) |
| CHK020 | `warpdl list` format for missed downloads | `"was 2026-02-27 03:00 (starting now)"` is the canonical format, moved from contracts-only to spec-level. | contracts |

### Requirement Consistency (CHK021–CHK026)

| ID | Issue | Resolution | Artifact |
|----|-------|------------|----------|
| CHK021 | FR-007 "prompt or start" vs contracts "start immediately" | **Resolved**: Contracts are canonical. Always start immediately with warning. No interactive prompt. See CHK017. | contracts (already aligned) |
| CHK022 | ScheduleState enum consistency | Verified: spec §Key Entities lists 5 states (`""`, `"scheduled"`, `"triggered"`, `"missed"`, `"cancelled"`). data-model.md lists same 5 states. Names match exactly. | N/A (already consistent) |
| CHK023 | Browser priority order inconsistency | **Resolved**: Research.md order is canonical (Firefox, LibreWolf, Chrome, Chromium, Edge, Brave). User Story 5's parenthetical list was an informal example. Update spec's User Story 5 to reference "documented priority order" without listing browsers inline. | spec addendum |
| CHK024 | `YYYY-MM-DD HH:MM` format consistency | Verified: Spec FR-001, contracts §New Flags, and error messages all use `YYYY-MM-DD HH:MM`. Consistent. Go format string: `"2006-01-02 15:04"`. | N/A (already consistent) |
| CHK025 | Domain matching rule consistency | **Resolved**: Research SQL queries are the canonical implementation. FR-017 "including subdomains" means the SQL pattern: exact match + dot-prefix + LIKE wildcard. See CHK016 resolution. | data-model |
| CHK026 | `warpdl stop` for recurring | **Resolved**: Spec FR-008 + clarification (Session 2026-02-28, Q about `warpdl stop`) explicitly says "permanently cancels the entire recurring schedule." Contracts already aligned. | N/A (already consistent) |

### Acceptance Criteria Quality (CHK027–CHK032)

| ID | Issue | Resolution | Artifact |
|----|-------|------------|----------|
| CHK027 | SC-001 60s measurability | Test with `time.AfterFunc` mock or short schedule (5s in future). Assert trigger within 60s. CI timing jitter handled by generous bounds — test asserts trigger happened, not exact timing. | plan (test strategy) |
| CHK028 | SC-002 100% schedule restoration | Test: create N items with various schedule states, serialize to GOB, deserialize, assert all fields match. Exhaustive over the 5 ScheduleState values. | plan (test strategy) |
| CHK029 | SC-004 auto-detection on macOS + Linux in CI | CI runs on Ubuntu and macOS. Install Firefox test fixture (mock `cookies.sqlite` + `profiles.ini`) in test temp dir. No real browser install needed — test the path resolution and parsing, not the browser itself. | plan (test strategy) |
| CHK030 | SC-005 2s for 10k cookies | Benchmark test: generate 10k-row SQLite fixture, time `ImportCookies()`. Assert < 2s. Run on CI hardware (standard GitHub Actions runner). Cold start (no warm cache). | plan (test strategy) |
| CHK031 | Recurring during daemon downtime | Already covered by FR-006 + scheduler's missed-schedule detection on startup. Add explicit acceptance scenario to test: schedule cron, simulate daemon restart after missed time, assert download starts. | plan (test strategy) |
| CHK032 | Speed limit edge cases | P3 — deferred. | deferred (P3) |

### Security & Privacy Coverage (CHK033–CHK040)

| ID | Issue | Resolution | Artifact |
|----|-------|------------|----------|
| CHK033 | FR-020 "display" scope | **Design decision**: "MUST NOT display" applies to ALL output modes: normal, debug, error messages, and crash dumps. Cookie values are never formatted into strings. Only cookie names and domains appear in logs (debug level only). | data-model |
| CHK034 | Sanitize cookies from HTTP request logs | **Design decision**: The Cookie header is set on `Item.Headers["Cookie"]`. Existing debug logging in `pkg/warplib/dloader.go` logs headers. Add header sanitization: if key is `Cookie` or `Set-Cookie`, log `"[REDACTED]"` instead of value. | plan (implementation note) |
| CHK035 | In-memory only vs core dumps | Out of scope for application-level code. Go does not expose heap contents in standard crash output. Core dumps are an OS-level concern. Document as accepted risk — cookies are inherently in-memory in every HTTP client. | plan (accepted risk) |
| CHK036 | Cookie path outside home directory | **Design decision**: No path restriction. Users may legitimately point to cookie files in non-standard locations. Symlinks are followed (standard `os.Open` behavior). The user invoked the command — they own the risk. | plan (documented here) |
| CHK037 | Temp file cleanup on failure | **Design decision**: Use `defer os.RemoveAll(tempDir)` immediately after temp dir creation. Cleanup runs on all paths (success, error, panic). Standard Go pattern. | data-model (cookie import flow) |
| CHK038 | CookieSourcePath exposure in shared systems | **Design decision**: `warpdl list` only shows browser name (e.g., "Firefox"), not the full path. Full path visible only in debug mode. CookieSourcePath is in the GOB file which is user-owned (`~/.config/warpdl/`). | contracts |
| CHK039 | FR-020 log level scope | Cookie names/domains logged at DEBUG level only (requires `--debug` / `-d` flag). No cookie information at any other log level. | plan (documented here) |
| CHK040 | Overly broad Netscape import | **Design decision**: FR-017 already mandates domain filtering. Cookies for other domains are silently skipped. No dry-run mode — domain filtering IS the safety mechanism. The warning "Imported N cookies for {domain}" tells the user exactly what was applied. | N/A (spec covers via FR-017) |

### Cross-Platform Coverage (CHK041–CHK048)

| ID | Issue | Resolution | Artifact |
|----|-------|------------|----------|
| CHK041 | Windows browser cookie paths | **Add to research.md**: Windows paths for all browsers using `%LOCALAPPDATA%` and `%APPDATA%`. | research |
| CHK042 | Windows `--cookies-from auto` | **Design decision**: Windows auto-detection follows same priority order. Uses `os.Getenv("LOCALAPPDATA")` and `os.Getenv("APPDATA")` for path construction. Build-tag isolated in `paths_windows.go`. | research, plan |
| CHK043 | Firefox Snap path in auto-detection | Already in research.md §4. Include in `paths_unix.go` — check `~/snap/firefox/common/.mozilla/firefox/` as fallback after standard `~/.mozilla/firefox/`. | plan (implementation note) |
| CHK044 | Chrome `Network/Cookies` migration | Already in research.md §4. Implementation checks both `Default/Cookies` and `Default/Network/Cookies`, preferring the newer path if both exist. | plan (implementation note) |
| CHK045 | Brave in auto-detection | Research lists Brave. Spec User Story 5 listed examples parenthetically. Brave is included in the canonical priority order (research.md). No spec change needed — the spec says "documented priority order" which is in research. | N/A (research covers) |
| CHK046 | Windows named pipe for schedule notifications | No change needed. Scheduled downloads use the existing download flow once triggered. Progress updates already broadcast via connection pool over named pipes on Windows. The scheduler just triggers an enqueue — no new IPC needed. | N/A (existing architecture) |
| CHK047 | `--start-at` across DST on Windows | Go's `time.Parse` with `time.Local` handles DST on all platforms (uses OS timezone database). Windows uses its own timezone database. No special handling needed. Ambiguous times (fall-back DST where 1:30 AM occurs twice) resolve to the first occurrence — this is Go's `time.Parse` default behavior. | plan (documented here) |
| CHK048 | Flatpak/AppImage browser paths | **Design decision**: Deferred. Flatpak and AppImage browser installations use non-standard paths that vary by distribution. Users with these installations should use explicit `--cookies-from <path>` or `--cookies-from cookies.txt` (Netscape export). Not worth the complexity for a niche case. | plan (deferred) |

### Edge Case & Error Handling (CHK049–CHK060)

| ID | Issue | Resolution | Artifact |
|----|-------|------------|----------|
| CHK049 | Zero-byte or truncated SQLite file | SQLite open will fail with driver error. Catch and report: `"error: cookie file at {path} is empty or corrupted"`. | contracts |
| CHK050 | WAL file present but main file missing/corrupted | Same as CHK049 — SQLite driver handles this. If the main file is missing, `os.Stat` fails first. If corrupted, driver returns error. | contracts |
| CHK051 | `--start-in 0s` or `--start-in 0m` | **Design decision**: Valid. Resolves to "now" — starts immediately. Equivalent to no scheduling flag. No warning needed. | contracts |
| CHK052 | Cron expression that never fires (e.g., Feb 30) | `gronx.NextTickAfter()` returns the next valid occurrence. For impossible dates, it may return a far-future time or error. **Design decision**: If no next occurrence within 1 year, warn: `"warning: cron expression '{expr}' has no occurrence in the next year"`. | contracts |
| CHK053 | Disk full during temp file copy | `io.Copy` returns error. Report: `"error: failed to copy cookie database: {err}"`. Standard error handling — no special case. | contracts |
| CHK054 | URL returns 301 redirect between cron occurrences | Normal download behavior — WarpDL already follows redirects. The redirected URL becomes the actual download source. No special handling for recurring. | N/A (existing behavior) |
| CHK055 | Older browser version with different schema | SQLite query will fail with "no such table" or "no such column" error. Report: `"error: unsupported cookie database schema at {path} — expected Firefox moz_cookies or Chrome cookies table"`. | contracts |
| CHK056 | Maximum cron expression or `--start-at` length | **Design decision**: No explicit limit. `gronx.IsValid()` rejects malformed expressions. `time.Parse` rejects malformed dates. Go string limits are sufficient. No abuse vector since WarpDL is single-user. | plan (documented here) |
| CHK057 | Ambiguous local time during DST fall-back | Go's `time.Parse` with `time.Local` returns the first occurrence of the ambiguous time. This is deterministic and consistent. Document: "Ambiguous times during DST fall-back resolve to the first occurrence." | plan (documented here) |
| CHK058 | `--cookies-from` with symlink | **Design decision**: Follow symlinks. Standard `os.Open` behavior. No symlink detection or rejection. See CHK036. | plan (documented here) |
| CHK059 | Overlapping recurring schedules for same URL | **Design decision**: Allowed. Each `warpdl download --schedule` creates a new Item with its own hash. Two recurring schedules for the same URL produce two independent download streams with different timestamp suffixes. The queue handles concurrency. | plan (documented here) |
| CHK060 | Netscape cookie file with CRLF line endings | **Design decision**: `bufio.Scanner` with default `ScanLines` handles both `\n` and `\r\n`. No special handling needed — Go's standard library handles this. | plan (documented here) |

### Dependencies & Assumptions (CHK061–CHK066)

| ID | Issue | Resolution | Artifact |
|----|-------|------------|----------|
| CHK061 | GOB zero-initializes missing fields | **Validated**: Go's `encoding/gob` specification states that fields present in the type but absent in the encoded data are set to their zero values. This is the documented behavior, confirmed by existing WarpDL usage (Protocol and SSHKeyPath fields added to Item without migration). | research (add validation note) |
| CHK062 | Firefox cookies unencrypted across versions | **Validated**: Firefox stores cookies unencrypted in SQLite across all variants (Release, ESR, Developer Edition, Nightly, LibreWolf). Firefox does not encrypt cookie values — this has been consistent since the moz_cookies schema was introduced. Unlike Chrome, Firefox has never implemented application-level cookie encryption. | research (add validation note) |
| CHK063 | Queue manager interface stability | The scheduler does not depend on the queue manager (#135) interface. Scheduled downloads call the same `Download()` function that CLI-initiated downloads use. The queue is internal to warplib. No interface dependency. | plan (documented here) |
| CHK064 | `modernc.org/sqlite` WAL mode | **Validated**: Using `?immutable=1` URI parameter bypasses WAL entirely — the file is treated as read-only. WAL checkpoint is handled by copying the WAL file alongside the main database. This is standard practice used by every cookie extraction tool. | research (add validation note) |
| CHK065 | Bandwidth throttling for speed limits (P3) | Speed limit scheduling (User Story 7, P3) depends on bandwidth throttling capability that does NOT exist today. This is explicitly noted in spec Assumptions. P3 is deferred — no implementation in this plan. | deferred (P3) |
| CHK066 | `adhocore/gronx` DST handling | Cron evaluation uses wall-clock time. `gronx.NextTickAfter(expr, time.Now(), false)` computes next occurrence in the local timezone, which inherently handles DST transitions. Spring-forward gaps (e.g., 2:30 AM doesn't exist) cause the next valid occurrence to be returned. Fall-back duplicates resolve via Go's `time.Parse` (first occurrence). | research (add validation note) |

### Ambiguities & Conflicts (CHK067–CHK070)

| ID | Issue | Resolution | Artifact |
|----|-------|------------|----------|
| CHK067 | Chrome unencrypted cookies practical usefulness | On macOS/Linux, Chrome encrypts cookies by default — unencrypted cookies are rare. The Chrome parser exists primarily for: (1) Windows where Chrome encryption uses DPAPI (different story), (2) Chromium builds without encryption, (3) Netscape export fallback is the documented recommendation for Chrome users on macOS/Linux. The feature is still useful via Netscape format. | plan (documented here) |
| CHK068 | "missed" → "cancelled" transition | **Design decision**: NOT allowed. "Missed" downloads are enqueued immediately on daemon restart — they transition to "triggered", then follow normal download flow. Once enqueued, the download cannot be in "missed" state long enough for a user to cancel it. If the user runs `warpdl stop` after the missed download starts, it follows the normal stop flow (cancels the active download). | data-model |
| CHK069 | CookieSourcePath sensitivity | CookieSourcePath reveals browser choice and profile path. **Design decision**: Acceptable risk for a single-user daemon. The GOB file is in `~/.config/warpdl/` (user-owned, 0600 permissions). `warpdl list` shows only browser name, not full path (see CHK038). | plan (documented here) |
| CHK070 | Auto-detect preference for unencrypted stores | The priority order already prefers Firefox (unencrypted) over Chrome (encrypted). This is by design — see research.md §4 priority order. The order IS a requirement (FR-016 "documented priority order"), not an implementation detail. | N/A (research covers) |

## Spec Addenda

The following items require minor spec updates before implementation. These are NOT spec gaps — they are precision improvements surfaced by the audit.

1. **FR-007 simplification**: Change "prompt for confirmation or start immediately" → "warn the user and start immediately." No interactive prompt. (CHK017, CHK021)
2. **FR-009 addendum**: Add "MAY be combined with `--start-at` or `--start-in` to delay the first occurrence." (CHK010)
3. **User Story 5**: Replace inline browser list with "in a documented priority order" reference. (CHK023)
4. **FR-002 clarification**: Add "Duration format uses Go's `time.ParseDuration` syntax: `h` (hours), `m` (minutes), `s` (seconds), and compounds (e.g., `1h30m`). Days are not supported — use `24h`." (CHK018)

## Complexity Tracking

> No constitution violations found. No complexity justification needed.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Two new `internal/` packages | `cookies` and `scheduler` have zero domain overlap | Single package would violate Principle IV (package isolation) |
