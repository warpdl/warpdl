# Tasks: Download Scheduling & Browser Cookie Import

**Input**: Design documents from `/specs/001-scheduling-cookie-import/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/cli-commands.md

**Tests**: Required — project mandates strict TDD+Trophy (red-green-refactor) with 80%+ coverage per package.

**Organization**: Tasks grouped by user story. US7 (Speed Limits by Time Schedule, P3) is DEFERRED per plan.md — no tasks generated.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- Go project at repository root
- Core library: `pkg/warplib/`
- Internal packages: `internal/cookies/`, `internal/scheduler/`
- CLI: `cmd/`
- Shared types: `common/`
- API handlers: `internal/api/`
- Daemon lifecycle: `internal/daemon/`
- E2E tests: `tests/e2e/`

---

## Phase 1: Setup

**Purpose**: Add dependencies and create package directory scaffolding

- [x] T001 Add `modernc.org/sqlite` v1.46.1 and `adhocore/gronx` dependencies via `go get` and run `go mod tidy`
- [x] T002 [P] Create `internal/cookies/` package with doc.go package comment
- [x] T003 [P] Create `internal/scheduler/` package with doc.go package comment

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Extend core types and build shared infrastructure that ALL user stories depend on

**CRITICAL**: No user story work can begin until this phase is complete

### Tests (write first, must fail)

- [x] T004 [P] Write GOB round-trip tests for new Item scheduling fields (ScheduledAt, CronExpr, ScheduleState) and CookieSourcePath in `pkg/warplib/item_test.go` — verify zero-value backward compat with pre-existing serialized data
- [x] T005 [P] Write min-heap tests in `internal/scheduler/heap_test.go` — push/pop ordering, empty heap, duplicate trigger times, remove by hash

### Implementation

- [x] T006 Define `ScheduleState` string type with constants (`""`, `"scheduled"`, `"triggered"`, `"missed"`, `"cancelled"`) in `pkg/warplib/item.go`
- [x] T007 Add scheduling fields (`ScheduledAt time.Time`, `CronExpr string`, `ScheduleState ScheduleState`) and cookie field (`CookieSourcePath string`) to `Item` struct in `pkg/warplib/item.go`
- [x] T008 Add `StartAt`, `StartIn`, `Schedule`, `CookiesFrom` string fields to `DownloadParams` struct in `common/types.go`
- [x] T009 [P] Define `Cookie`, `CookieSource`, `CookieFormat` types in `internal/cookies/types.go` per data-model.md
- [x] T010 [P] Define `ScheduleEvent` type (ItemHash, TriggerAt, CronExpr) in `internal/scheduler/types.go` per data-model.md
- [x] T011 Implement min-heap (`container/heap` interface) for `ScheduleEvent` sorted by `TriggerAt` in `internal/scheduler/heap.go` — include `RemoveByHash(hash string)` method

**Checkpoint**: Foundation ready — T004 and T005 tests pass green. User story implementation can begin.

---

## Phase 3: User Story 1 — Schedule Download for Later (Priority: P1) MVP

**Goal**: Users can schedule a download for a specific absolute time with `--start-at "YYYY-MM-DD HH:MM"` and have it start automatically within 60 seconds of the target time.

**Independent Test**: Schedule a download with `--start-at` for a near-future time, run `warpdl list` to see "scheduled" status, verify download starts at the specified time.

### Tests for User Story 1

> **NOTE**: Write these tests FIRST, ensure they FAIL before implementation

- [x] T012 [P] [US1] Write `--start-at` flag validation tests in `cmd/download_test.go` — valid format, invalid format rejection with error message, past-time warning
- [x] T013 [P] [US1] Write scheduler core loop tests in `internal/scheduler/scheduler_test.go` — add event, fire when time arrives (use short durations like 100ms), cancel event by hash, shutdown via context cancellation
- [x] T014 [P] [US1] Write schedule-aware item query tests in `pkg/warplib/manager_test.go` — filter items by ScheduleState, list only "scheduled" items

### Implementation for User Story 1

- [x] T015 [US1] Add `--start-at` flag to `dlFlags` slice in `cmd/download.go` — validate with `time.Parse("2006-01-02 15:04", value)`, reject invalid format per contracts/cli-commands.md error messages
- [x] T016 [US1] Add past-time detection for `--start-at`: if parsed time is before `time.Now()`, print warning `"warning: scheduled time is in the past, starting download immediately"` and clear the start-at value in `cmd/download.go`
- [x] T017 [US1] Wire `StartAt` from `DownloadParams` into `Item.ScheduledAt` and set `Item.ScheduleState = "scheduled"` in `internal/api/download.go` download handler
- [x] T018 [US1] Implement scheduler core goroutine in `internal/scheduler/scheduler.go` — active-object pattern with addChan/removeChan/ctx.Done, min-heap, 60s max-sleep-cap loop per research.md §5 design
- [x] T019 [US1] Add `OnTrigger` callback to scheduler — when event fires, call provided function with `ItemHash` to enqueue download via existing flow
- [x] T020 [US1] Add schedule-aware query method `GetScheduledItems() []*Item` to `Manager` in `pkg/warplib/manager.go` — returns items where `ScheduleState == "scheduled"`
- [x] T021 [US1] Instantiate scheduler in daemon startup in `internal/daemon/runner.go` — pass context for lifecycle, register trigger callback that starts download through existing API path
- [x] T022 [US1] On daemon startup, scan all items via `Manager.GetItems()`: if `ScheduleState == "scheduled"` and `ScheduledAt` is in the future, add to scheduler heap in `internal/daemon/runner.go`. **Note**: This implements the minimal US1 startup scan. T049/T050 (Phase 6, US4) introduce `LoadSchedules()` which supersedes this with missed-schedule detection. Implement T022 as a simple future-only scan; T050 will replace the call site with `LoadSchedules()`.
- [x] T023 [US1] Extend `warpdl list` output to show `Scheduled` column — display `ScheduledAt` time for one-shot scheduled items, `—` for unscheduled items in `cmd/list.go`
- [x] T024 [US1] Extend `warpdl stop <id>` to handle scheduled (not-yet-started) items: set `ScheduleState = "cancelled"`, remove from scheduler heap, print cancellation confirmation in `cmd/stop.go` (or relevant handler)

**Checkpoint**: User Story 1 fully functional — can schedule via `--start-at`, see in `warpdl list`, cancel via `warpdl stop`, download auto-starts at target time.

---

## Phase 4: User Story 2 — Schedule Download with Relative Time (Priority: P1)

**Goal**: Users can schedule a download with `--start-in 2h` (Go duration syntax) instead of calculating an absolute clock time.

**Independent Test**: Schedule with `--start-in 5s`, verify download starts approximately 5 seconds later.

### Tests for User Story 2

- [x] T025 [P] [US2] Write `--start-in` flag validation tests in `cmd/download_test.go` — valid durations (`2h`, `30m`, `1h30m`), invalid format rejection, `0s` treated as immediate, mutual exclusion with `--start-at`

### Implementation for User Story 2

- [x] T026 [US2] Add `--start-in` flag to `dlFlags` slice in `cmd/download.go` — validate with `time.ParseDuration(value)`, reject invalid format per contracts error messages
- [x] T027 [US2] Add mutual exclusion check: if both `--start-at` and `--start-in` are set, reject with `"error: flags --start-at and --start-in are mutually exclusive"` in `cmd/download.go`
- [x] T028 [US2] Resolve `--start-in` to absolute time (`time.Now().Add(duration)`) and set `DownloadParams.StartAt` to the resolved value in `cmd/download.go` — daemon only sees absolute times
- [x] T029 [US2] Extend `warpdl list` to show both countdown (remaining time) and resolved absolute time for items that were submitted with `--start-in` in `cmd/list.go`

**Checkpoint**: US1 + US2 both work. Users can schedule with either absolute or relative time.

---

## Phase 5: User Story 3 — Import Browser Cookies for Authenticated Download (Priority: P1)

**Goal**: Users download authenticated files by pointing `--cookies-from` at a browser cookie file (Firefox SQLite, Chrome SQLite unencrypted, or Netscape text). Cookies for the target domain are imported and sent as HTTP headers.

**Independent Test**: Point `--cookies-from` at a Firefox `cookies.sqlite` fixture containing cookies for a test domain, verify cookies appear in the HTTP request headers.

### Tests for User Story 3

> **NOTE**: Write these tests FIRST, ensure they FAIL before implementation

- [x] T030 [P] [US3] Write format detection tests in `internal/cookies/detect_test.go` — identify Firefox SQLite (moz_cookies table), Chrome SQLite (cookies table), Netscape text (header line), unknown format, empty file, truncated file
- [x] T031 [P] [US3] Write Firefox parser tests in `internal/cookies/firefox_test.go` — parse moz_cookies rows, domain filtering (exact, dot-prefix, subdomain wildcard), skip expired cookies, empty result for unmatched domain. Create test SQLite fixture with `modernc.org/sqlite`.
- [x] T032 [P] [US3] Write Chrome parser tests in `internal/cookies/chrome_test.go` — parse cookies rows (unencrypted only), skip rows with only encrypted_value, Chrome timestamp conversion (microseconds since 1601-01-01), domain filtering, skip expired. Create test SQLite fixture.
- [x] T033 [P] [US3] Write Netscape parser tests in `internal/cookies/netscape_test.go` — parse standard lines, skip comments, handle `#HttpOnly_` prefix, skip malformed lines with warning, handle CRLF line endings, skip expired cookies, domain filtering
- [x] T034 [P] [US3] Write safe-copy tests in `internal/cookies/copy_test.go` — copy SQLite + WAL + SHM to temp dir, cleanup on success, cleanup on error, reject directories, reject empty files
- [x] T035 [P] [US3] Write integration import tests in `internal/cookies/import_test.go` — end-to-end: detect format → copy → parse → filter → build Cookie header string. Test with Firefox fixture, Chrome fixture, Netscape fixture.
- [x] T036 [P] [US3] Write `--cookies-from` flag validation tests in `cmd/cookies_from_test.go` — file-not-found error, directory rejection, valid path accepted, `auto` keyword accepted

### Implementation for User Story 3

- [x] T037 [US3] Implement format detection in `internal/cookies/detect.go` — read first 16 bytes for SQLite magic (`SQLite format 3\000`), then query table names (`moz_cookies` → Firefox, `cookies` → Chrome). For non-SQLite: check first line for `# Netscape HTTP Cookie File` or `# HTTP Cookie File` header.
- [x] T038 [US3] Implement safe SQLite copy in `internal/cookies/copy.go` — copy `.sqlite` file + `-wal` + `-shm` (if exist) to `os.MkdirTemp`, return temp dir path. Use `defer os.RemoveAll` pattern per CHK037. Validate: reject directories (`os.Stat().IsDir()`), reject zero-byte files.
- [x] T039 [US3] Implement Firefox cookie parser in `internal/cookies/firefox.go` — open copied SQLite with `?immutable=1`, execute domain-filtered query from research.md §3, map rows to `[]Cookie`, skip expired (expiry > unix now). Close DB before returning.
- [x] T040 [US3] Implement Chrome cookie parser in `internal/cookies/chrome.go` — open copied SQLite with `?immutable=1`, query unencrypted cookies only (`value != ''`), convert Chrome timestamps (microseconds since 1601-01-01 → Unix seconds: `(chrome_usec / 1_000_000) - 11_644_473_600`), domain filter, skip expired. Warn if ALL cookies for domain are encrypted.
- [x] T041 [US3] Implement Netscape cookie parser in `internal/cookies/netscape.go` — `bufio.Scanner` line-by-line, skip comments (except `#HttpOnly_`), split by tab (7 fields), parse domain/subdomain_flag/path/secure/expiry/name/value, domain filter, skip expired, skip malformed lines with logged warning.
- [x] T042 [US3] Implement `ImportCookies(sourcePath string, domain string) ([]Cookie, *CookieSource, error)` entry point in `internal/cookies/import.go` — orchestrate: detect format → safe copy (SQLite) or direct read (Netscape) → parse → filter → return cookies + source metadata. Build `Cookie` HTTP header string: `name1=val1; name2=val2`.
- [x] T043 [US3] Add `--cookies-from` flag to `dlFlags` slice in `cmd/download.go` — validate: file exists (unless `auto`), not a directory, pass value through to `DownloadParams.CookiesFrom`
- [x] T044 [US3] Hook cookie import into download flow in `internal/api/download.go` — if `CookiesFrom != ""` and `CookiesFrom != "auto"`: call `cookies.ImportCookies(path, domain)`, merge returned Cookie header into `Item.Headers["Cookie"]`, set `Item.CookieSourcePath`, log `"Imported N cookies for {domain} from {browser} ({path})"`
- [x] T045 [US3] Add Cookie/Set-Cookie header redaction in `pkg/warplib/dloader.go` debug logging — if header key is `Cookie` or `Set-Cookie`, log value as `[REDACTED]` per CHK034
- [x] T046 [US3] Add cookie re-import on download resume: in resume flow, if `Item.CookieSourcePath != ""`, re-import cookies and update `Item.Headers["Cookie"]` in `internal/api/resume.go`

**Checkpoint**: US3 fully functional — can import Firefox, Chrome (unencrypted), and Netscape cookies. Authenticated downloads work. Cookie values never logged.

---

## Phase 6: User Story 4 — Persist Schedules Across Daemon Restarts (Priority: P2)

**Goal**: Scheduled downloads survive daemon restarts. Missed schedules (daemon was down when trigger time passed) are detected and started immediately on restart.

**Independent Test**: Schedule a download, kill the daemon, restart it, verify the schedule fires correctly. For missed schedules: schedule for a past time, restart daemon, verify download starts immediately with notification.

### Tests for User Story 4

- [x] T047 [P] [US4] Write missed-schedule detection tests in `internal/scheduler/scheduler_test.go` — simulate: items with `ScheduleState=="scheduled"` and `ScheduledAt` in the past → detect as missed, transition to `"triggered"`, enqueue. Items with future `ScheduledAt` → re-add to heap.
- [x] T048 [P] [US4] Write schedule persistence round-trip test in `pkg/warplib/manager_test.go` — create items with all 5 ScheduleState values + ScheduledAt + CronExpr, serialize Manager to GOB, deserialize, assert 100% field preservation (SC-002)

### Implementation for User Story 4

- [x] T049 [US4] Implement `LoadSchedules(items ItemsMap, now time.Time) (missed []*Item, future []ScheduleEvent)` in `internal/scheduler/scheduler.go` — scan items: if scheduled + past → mark missed + return for immediate enqueue; if scheduled + future → return as heap events
- [x] T050 [US4] Wire `LoadSchedules` into daemon startup in `internal/daemon/runner.go` — after Manager load, call LoadSchedules, enqueue all missed items, add all future events to scheduler heap
- [x] T051 [US4] Log missed schedule notification: `"Missed schedule: {name} (was {time}), starting now"` in daemon log when missed items are detected on startup in `internal/daemon/runner.go`
- [x] T052 [US4] Update `warpdl list` to show missed status: display `"was YYYY-MM-DD HH:MM (starting now)"` for items with `ScheduleState == "missed"` in `cmd/list.go`

**Checkpoint**: US4 done — schedules persist across restarts. Missed schedules auto-start with notification.

---

## Phase 7: User Story 5 — Auto-Detect Browser Cookie Store (Priority: P2)

**Goal**: `--cookies-from auto` scans known browser locations in priority order (Firefox, LibreWolf, Chrome, Chromium, Edge, Brave) and uses the first valid cookie store found.

**Independent Test**: With Firefox installed at default path, run `--cookies-from auto` and verify auto-detection picks up the Firefox cookie store.

### Tests for User Story 5

- [x] T053 [P] [US5] Write browser path resolution tests in `internal/cookies/paths_unix_test.go` — mock home dir, verify correct paths for Firefox, LibreWolf, Chrome, Chromium, Edge, Brave on Linux and macOS (use build tags)
- [x] T054 [P] [US5] Write browser path resolution tests in `internal/cookies/paths_windows_test.go` — mock LOCALAPPDATA/APPDATA env vars, verify correct paths for all browsers on Windows (use build tags)
- [x] T055 [P] [US5] Write Firefox profile resolution tests in `internal/cookies/paths_test.go` — parse `profiles.ini` with `[Install*]` Default key, fallback to `[Profile*]` with `Default=1`, handle missing profiles.ini, handle malformed profiles.ini
- [x] T056 [P] [US5] Write auto-detection integration tests in `internal/cookies/detect_test.go` — create temp dir with mock browser cookie files at expected paths, verify priority order (Firefox first), verify "no browser found" error when none exist

### Implementation for User Story 5

- [x] T057 [US5] Define browser path constants and `DetectBrowserCookies(domain string) ([]Cookie, *CookieSource, error)` interface in `internal/cookies/paths.go`
- [x] T058 [US5] Implement macOS + Linux browser path resolution in `internal/cookies/paths_unix.go` (`//go:build unix`) — paths per research.md §4 for Firefox (+ Snap fallback), LibreWolf, Chrome (check both `Default/Cookies` and `Default/Network/Cookies`), Chromium, Edge, Brave
- [x] T059 [US5] Implement Windows browser path resolution in `internal/cookies/paths_windows.go` (`//go:build windows`) — use `os.Getenv("LOCALAPPDATA")` and `os.Getenv("APPDATA")` per research.md §4
- [x] T060 [US5] Implement Firefox `profiles.ini` parser in `internal/cookies/paths.go` — parse INI sections, find default profile path per research.md §4 rules (Install section → Profile section with Default=1)
- [x] T061 [US5] Implement auto-detection scan in `internal/cookies/detect.go` — iterate browser paths in priority order, check file existence + readability, return first valid `CookieSource`. On failure: return error listing all supported browsers and expected paths per contracts error message.
- [x] T062 [US5] Wire `auto` handling into cookie import in `internal/api/download.go` — if `CookiesFrom == "auto"`, call `DetectBrowserCookies(domain)`, log which browser was selected: `"Auto-detected {browser} cookie store at {path}"`

**Checkpoint**: US5 done — `--cookies-from auto` works on macOS, Linux, and Windows. Priority order respected.

---

## Phase 8: User Story 6 — Recurring Download Schedules (Priority: P3)

**Goal**: Users define cron-based recurring schedules with `--schedule "0 2 * * *"`. Each trigger produces a timestamped output file. `warpdl stop` cancels the entire recurring schedule.

**Independent Test**: Create a recurring schedule with a short cron interval, verify it triggers multiple times with timestamp-suffixed filenames.

### Tests for User Story 6

- [x] T063 [P] [US6] Write `--schedule` flag validation tests in `cmd/download_test.go` — valid 5-field cron accepted, invalid cron rejected with error, combination with `--start-at` allowed, combination with `--start-in` allowed, combination of `--start-at` + `--start-in` + `--schedule` rejected (mutual exclusion of first two still applies)
- [x] T064 [P] [US6] Write cron next-occurrence tests in `internal/scheduler/scheduler_test.go` — compute next tick with `gronx.NextTickAfter`, no-occurrence-in-1-year warning (CHK052), recurring re-schedule after fire
- [x] T065 [P] [US6] Write timestamp suffix tests in `pkg/warplib/item_test.go` or `internal/api/download_test.go` — input `backup.tar.gz` → output `backup-2026-03-01T020000.tar.gz`, files without extension, files with multiple dots

### Implementation for User Story 6

- [x] T066 [US6] Add `--schedule` flag to `dlFlags` slice in `cmd/download.go` — validate with `gronx.IsValid(expr)`, reject invalid expressions per contracts error messages. Warn if no occurrence in next year via `gronx.NextTickAfter` with 1-year horizon check.
- [x] T067 [US6] Wire `Schedule` from `DownloadParams` into `Item.CronExpr` in `internal/api/download.go` — compute first trigger time via `gronx.NextTickAfter(expr, time.Now(), false)`. If `--start-at` or `--start-in` also set, use their time as first occurrence instead.
- [x] T068 [US6] Implement timestamp suffix logic for recurring downloads in `internal/api/download.go` — format: `<basename>-<YYYY-MM-DDTHHMMSS>.<ext>` per FR-009a. Apply suffix before calling download start for recurring items.
- [x] T069 [US6] Implement recurring re-schedule in scheduler: after a recurring download completes or fails, compute next cron occurrence, set `Item.ScheduledAt` = next, `Item.ScheduleState = "scheduled"`, re-add to scheduler heap in `internal/scheduler/scheduler.go`
- [x] T070 [US6] Handle `warpdl stop` for recurring schedules: set `ScheduleState = "cancelled"`, remove from scheduler heap, cancel ALL future occurrences. Print `"Cancelled recurring schedule for {name}"` in stop handler
- [x] T071 [US6] Extend `warpdl list` for recurring items: show cron expression and next scheduled time, format: `"(recurring: 0 2 * * *, next: YYYY-MM-DD HH:MM)"` in `cmd/list.go`
- [x] T072 [US6] Handle missed recurring schedules on daemon restart: enqueue missed occurrence immediately AND compute next cron occurrence to re-add to heap (both happen) in `internal/scheduler/scheduler.go` via `LoadSchedules`
- [x] T073 [US6] Hook cookie re-import for recurring triggers: when a recurring download fires, re-import cookies from `Item.CookieSourcePath` (if set) before starting the download in `internal/api/download.go`

**Checkpoint**: US6 done — recurring schedules work with cron expressions, timestamp-suffixed filenames, and proper stop/restart behavior.

---

## Phase 9: Polish & Cross-Cutting Concerns

**Purpose**: E2E tests, integration validation, and cleanup across all stories

- [x] T074 [P] Write E2E test for scheduling + cookie import in `tests/e2e/schedule_cookie_test.go` (`//go:build e2e`) — schedule a download with `--start-at` near-future, verify it starts. Import cookies from Netscape fixture, verify download uses cookies.
- [x] T075 [P] Write cookie import benchmark test in `internal/cookies/import_test.go` — generate 10k-row SQLite fixture, assert `ImportCookies` completes in < 2 seconds (SC-005)
- [x] T075b [P] Write queue concurrency cap test for simultaneous schedule triggers in `internal/scheduler/scheduler_test.go` or `pkg/warplib/queue_test.go` — schedule N downloads for the same trigger time (N > queue concurrency limit), verify that at most `maxConcurrent` downloads are active simultaneously and the rest are queued (FR-010)
- [x] T076 Run `go vet ./...` and `go fmt ./...` across all new and modified files
- [x] T077 Run `scripts/check_coverage.sh` — ensure 80%+ coverage on `pkg/warplib/`, `internal/cookies/`, `internal/scheduler/`, `cmd/`
- [x] T078 Run `go test -race -short ./...` — verify no data races in scheduler goroutine, cookie import, or daemon startup paths
- [x] T079 Validate all quickstart.md examples work end-to-end against running daemon
- [x] T080 Run `go build -ldflags="-w -s" .` — verify clean build with no compilation errors

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — can start immediately
- **Foundational (Phase 2)**: Depends on Phase 1 — BLOCKS all user stories
- **US1 (Phase 3)**: Depends on Phase 2 — core scheduling infrastructure
- **US2 (Phase 4)**: Depends on Phase 3 (US1) — extends `--start-at` with `--start-in` variant
- **US3 (Phase 5)**: Depends on Phase 2 only — independent from scheduling stories
- **US4 (Phase 6)**: Depends on Phase 3 (US1) — persistence of scheduling state
- **US5 (Phase 7)**: Depends on Phase 5 (US3) — extends cookie import with auto-detection
- **US6 (Phase 8)**: Depends on Phase 3 (US1) + Phase 6 (US4) — recurring needs scheduler + persistence
- **Polish (Phase 9)**: Depends on all desired user stories being complete

### User Story Dependencies

```
Phase 1: Setup
  └─> Phase 2: Foundational
        ├─> Phase 3: US1 (--start-at)
        │     ├─> Phase 4: US2 (--start-in) — extends US1 flag handling
        │     ├─> Phase 6: US4 (persistence) — extends US1 scheduler
        │     │     └─> Phase 8: US6 (recurring) — needs scheduler + persistence
        │     └─> Phase 8: US6 (recurring) — needs scheduler core
        └─> Phase 5: US3 (cookie import) — independent of scheduling
              └─> Phase 7: US5 (auto-detect) — extends US3 path resolution
```

### Within Each User Story

1. Tests MUST be written and FAIL before implementation (strict TDD)
2. Types/models before services
3. Core logic before CLI integration
4. CLI flags before daemon wiring
5. Display (list/stop) after core logic works

### Parallel Opportunities

**After Phase 2 completes**:
- US1 (Phase 3) and US3 (Phase 5) can run in parallel — zero shared code
- Within US3: all parser tests (T030–T036) can run in parallel
- Within US3: all parser implementations (T037–T041) can run in parallel after their tests

**After US1 completes**:
- US2 (Phase 4) and US4 (Phase 6) can run in parallel

**After US3 completes**:
- US5 (Phase 7) can run in parallel with US2/US4/US6

---

## Parallel Example: User Story 3 (Cookie Import)

```bash
# Wave 1 — All test files in parallel (6 tasks):
T030: Format detection tests in internal/cookies/detect_test.go
T031: Firefox parser tests in internal/cookies/firefox_test.go
T032: Chrome parser tests in internal/cookies/chrome_test.go
T033: Netscape parser tests in internal/cookies/netscape_test.go
T034: Safe-copy tests in internal/cookies/copy_test.go
T035: Integration import tests in internal/cookies/import_test.go
T036: CLI flag validation tests in cmd/download_test.go

# Wave 2 — Parallel implementations (after tests exist):
T037: Format detection in internal/cookies/detect.go
T038: Safe SQLite copy in internal/cookies/copy.go
T039: Firefox parser in internal/cookies/firefox.go
T040: Chrome parser in internal/cookies/chrome.go
T041: Netscape parser in internal/cookies/netscape.go

# Wave 3 — Sequential integration:
T042: Import entry point (depends on T037–T041)
T043: CLI flag (independent)
T044: API hook (depends on T042)
T045: Header redaction (independent)
T046: Resume re-import (depends on T042)
```

---

## Parallel Example: User Story 1 (Scheduling)

```bash
# Wave 1 — All test files in parallel:
T012: --start-at flag tests in cmd/download_test.go
T013: Scheduler core loop tests in internal/scheduler/scheduler_test.go
T014: Schedule-aware query tests in pkg/warplib/manager_test.go

# Wave 2 — Parallel implementations:
T015 + T016: CLI flag + past-time detection in cmd/download.go
T018 + T019: Scheduler goroutine in internal/scheduler/scheduler.go
T020: Manager query method in pkg/warplib/manager.go

# Wave 3 — Sequential integration:
T017: API handler wiring (depends on T015, T018)
T021 + T022: Daemon startup (depends on T018, T020)
T023: List display (depends on T020)
T024: Stop handler (depends on T018)
```

---

## Implementation Strategy

### MVP First (US1 + US2 + US3 = Phase 1–5)

1. Complete Phase 1: Setup (dependencies)
2. Complete Phase 2: Foundational types (all tests pass green)
3. Complete Phase 3: US1 — `--start-at` scheduling works end-to-end
4. **STOP and VALIDATE**: Test US1 independently
5. Complete Phase 4: US2 — `--start-in` extends scheduling (lightweight)
6. Complete Phase 5: US3 — Cookie import works end-to-end
7. **STOP and VALIDATE**: All P1 stories independently functional. Deploy MVP.

### Incremental Delivery

1. Setup + Foundational → Foundation ready
2. US1 → Schedule with `--start-at` (testable MVP increment)
3. US2 → Schedule with `--start-in` (extends MVP, quick win)
4. US3 → Cookie import (independent P1, high-value)
5. US4 → Persistence across restarts (reliability for scheduling)
6. US5 → Auto-detect browser cookies (UX improvement)
7. US6 → Recurring schedules (power-user feature)
8. Polish → E2E, benchmarks, coverage validation

### Parallel Team Strategy

With 2 developers after Phase 2:
- **Dev A**: US1 → US2 → US4 → US6 (scheduling track)
- **Dev B**: US3 → US5 (cookie import track)

Stories integrate cleanly at Phase 8 (US6) and Phase 9 (Polish) where scheduling meets cookies for recurring authenticated downloads.

---

## Notes

- [P] tasks = different files, no dependencies on incomplete tasks in same phase
- [Story] label maps task to specific user story for traceability
- US7 (Speed Limits by Time Schedule, P3) is DEFERRED — depends on bandwidth throttling that doesn't exist yet
- All error messages match contracts/cli-commands.md exactly
- Cookie values are NEVER logged at any level — names and domains at DEBUG only
- `modernc.org/sqlite` is pure-Go (CGO_ENABLED=0 safe) — no CGO dependency
- Scheduler uses wall-clock time for absolute schedules, handles DST and NTP via 60s re-evaluation cap
- GOB backward compatibility guaranteed — all new fields have safe zero values
