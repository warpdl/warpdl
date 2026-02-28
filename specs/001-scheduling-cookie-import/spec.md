# Feature Specification: Download Scheduling & Browser Cookie Import

**Feature Branch**: `001-scheduling-cookie-import`
**Created**: 2026-02-28
**Status**: Draft
**Input**: GitHub Issues #140 (download scheduling) and #141 (browser cookie import)
**Related Issues**: [#140](https://github.com/warpdl/warpdl/issues/140), [#141](https://github.com/warpdl/warpdl/issues/141), [#135](https://github.com/warpdl/warpdl/issues/135) (queue manager)

## Clarifications

### Session 2026-02-28

- Q: FR-014 says MUST support Chrome cookies, but Assumptions say encrypted Chrome cookies are out of scope. Chrome encrypts by default on macOS/Linux — which takes precedence? → A: FR-014 limited to unencrypted Chrome DBs + Netscape export fallback. Chrome decryption is out of scope.
- Q: Are imported cookies persisted with the download item (GOB store) or held in-memory only? → A: In-memory only. Re-import from source path on resume, retry, or recurring trigger. Cookie values are never written to disk.
- Q: Should scheduling metadata be a separate entity, a wrapper struct, or added directly to the existing Item struct? → A: Extend Item directly with optional scheduling fields (start time, cron expression, schedule state). Keeps data model flat and compatible with existing GOB persistence.
- Q: When multiple missed downloads trigger on daemon restart, start all at once or stagger? → A: Enqueue all at normal priority into existing queue. Queue concurrency cap handles throttling naturally — no artificial stagger needed.
- Q: Should --start-at support explicit timezone specification or local timezone only? → A: Local timezone only. No timezone suffix or --timezone flag. Users schedule on their own machine in their own timezone.
- Q: When a recurring download triggers repeatedly, what happens with the output file? → A: Each trigger appends a timestamp suffix to the filename (e.g., `backup-2026-03-01T020000.sql.gz`). Prevents silent data loss from overwriting; expected behavior for automated/cron-style workflows.
- Q: Does `warpdl stop <id>` on a recurring download cancel only the current execution or the entire schedule? → A: Permanently cancels the entire recurring schedule (all future executions). A recurring download is one logical entity with one ID — "stop" means stop everything.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Schedule Download for Later (Priority: P1)

A user finds a large file to download but wants it to start later — during off-peak hours when bandwidth is cheaper or when the network is less congested. They specify an absolute time and the download begins automatically at that time without requiring the user to be present.

**Why this priority**: Core scheduling capability. Without this, no other scheduling features are useful. Addresses the most common use case: "download this tonight while I sleep."

**Independent Test**: Can be fully tested by scheduling a download with `--start-at` and verifying it starts at the specified time. Delivers immediate value for users who want unattended off-peak downloads.

**Acceptance Scenarios**:

1. **Given** the daemon is running and a valid URL, **When** the user runs `warpdl download --start-at "2026-03-01 02:00" <url>`, **Then** the download is queued with a "scheduled" status and starts automatically at 02:00 on March 1st.
2. **Given** a scheduled download exists, **When** the user runs `warpdl list`, **Then** the scheduled download appears with its scheduled start time clearly displayed.
3. **Given** a download is scheduled for a future time, **When** the scheduled time arrives and the daemon is running, **Then** the download begins automatically without user intervention.
4. **Given** a download is scheduled for a time that has already passed (e.g., 1 hour ago), **When** the command is executed, **Then** the system warns the user that the time is in the past and starts the download immediately.

---

### User Story 2 - Schedule Download with Relative Time (Priority: P1)

A user wants to start a download "in 2 hours" without calculating the exact clock time. They use a human-friendly relative time offset.

**Why this priority**: Equally fundamental as absolute scheduling — different expression of the same core need. Many users think in relative terms ("after dinner", "in a few hours") rather than clock times.

**Independent Test**: Can be tested by scheduling with `--start-in 2h` and verifying the download starts approximately 2 hours later.

**Acceptance Scenarios**:

1. **Given** the daemon is running, **When** the user runs `warpdl download --start-in 2h <url>`, **Then** the download is scheduled for 2 hours from now and starts automatically when the time elapses.
2. **Given** a relative-time scheduled download, **When** the user runs `warpdl list`, **Then** the listing shows both the remaining countdown and the resolved absolute start time.
3. **Given** a user specifies `--start-in 30m`, **When** 30 minutes pass, **Then** the download transitions from "scheduled" to "downloading" state.

---

### User Story 3 - Import Browser Cookies for Authenticated Download (Priority: P1)

A user needs to download a file from a site where they are already logged in via their browser. Instead of manually extracting cookies, they point WarpDL at their browser's cookie store and the tool imports the relevant cookies automatically.

**Why this priority**: Authenticated downloads are a top pain point. Users currently must manually copy cookies per download — tedious and error-prone. Auto-importing from browsers removes significant friction.

**Independent Test**: Can be tested by importing cookies from a browser cookie file and verifying an authenticated download succeeds where it would otherwise fail with a 403.

**Acceptance Scenarios**:

1. **Given** the user is logged into a site in Firefox, **When** they run `warpdl download --cookies-from ~/.mozilla/firefox/<profile>/cookies.sqlite <protected-url>`, **Then** WarpDL imports the relevant cookies for that domain and the download succeeds with authentication.
2. **Given** a Chrome cookie store exists at the default location, **When** the user runs `warpdl download --cookies-from auto <protected-url>`, **Then** WarpDL auto-detects the Chrome cookie file, imports domain-relevant cookies, and completes the authenticated download.
3. **Given** the user provides a Netscape-format cookie text file, **When** they run `warpdl download --cookies-from cookies.txt <url>`, **Then** WarpDL parses the Netscape format and applies the cookies to the download request.
4. **Given** cookies are imported, **When** the download begins, **Then** the system displays a message like "Imported N cookies for example.com" so the user knows cookies were applied.

---

### User Story 4 - Persist Schedules Across Daemon Restarts (Priority: P2)

A user schedules a download for 3 AM, then the system reboots or the daemon restarts. When the daemon comes back up, the scheduled download must still fire at the correct time.

**Why this priority**: Without persistence, scheduling is unreliable. Users will lose trust if scheduled downloads silently disappear after a daemon restart.

**Independent Test**: Can be tested by scheduling a download, restarting the daemon, and verifying the download still executes at the scheduled time.

**Acceptance Scenarios**:

1. **Given** a download is scheduled for a future time, **When** the daemon is stopped and restarted, **Then** the scheduled download is restored and will still execute at the originally scheduled time.
2. **Given** a download was scheduled for a time that passed during daemon downtime, **When** the daemon restarts, **Then** the download starts immediately and the user is notified that it was delayed.

---

### User Story 5 - Auto-Detect Browser Cookie Store (Priority: P2)

A user doesn't know where their browser stores cookies. They just want WarpDL to find the right cookie file automatically by scanning known browser locations.

**Why this priority**: Reduces friction for non-technical users. The `auto` mode tries known browser paths in a documented priority order (see research.md §4) and uses the first valid store found.

**Independent Test**: Can be tested by having a browser installed and running `--cookies-from auto` to verify auto-detection works.

**Acceptance Scenarios**:

1. **Given** Firefox is installed with a default profile, **When** `--cookies-from auto` is used, **Then** WarpDL finds and uses the Firefox cookie store.
2. **Given** multiple browsers are installed, **When** `--cookies-from auto` is used, **Then** WarpDL uses the first detected browser in the documented priority order (Firefox, LibreWolf, Chrome, Chromium, Edge, Brave — unencrypted stores preferred) and informs the user which browser's cookies were selected.
3. **Given** no supported browser cookie store is found, **When** `--cookies-from auto` is used, **Then** WarpDL displays a clear error listing supported browsers (Firefox, LibreWolf, Chrome, Chromium, Edge, Brave) and expected paths.

---

### User Story 6 - Recurring Download Schedules (Priority: P3)

A user wants to download a file every night at 2 AM (e.g., a daily database backup or log dump). They define a cron-like recurring schedule.

**Why this priority**: Power-user feature. Most users schedule one-off downloads. Recurring schedules serve niche automation workflows.

**Independent Test**: Can be tested by creating a recurring schedule and verifying the download triggers on consecutive schedule hits.

**Acceptance Scenarios**:

1. **Given** the daemon is running, **When** the user runs `warpdl download --schedule "0 2 * * *" <url>`, **Then** the download executes every day at 2:00 AM.
2. **Given** a recurring schedule exists, **When** the user runs `warpdl list`, **Then** the recurring download shows the cron expression and the next scheduled execution time.
3. **Given** a recurring download triggers multiple times, **When** the download completes each time, **Then** the output file has a timestamp suffix (e.g., `backup-2026-03-01T020000.sql.gz`) and previous downloads are preserved.
4. **Given** a recurring download, **When** the user runs `warpdl stop <id>`, **Then** the entire recurring schedule is permanently cancelled — no future executions will trigger.

---

### User Story 7 - Speed Limits by Time Schedule (Priority: P3)

A user wants downloads to run at full speed overnight but throttled during work hours to avoid saturating their connection. They define time-based speed limit rules.

**Why this priority**: Nice-to-have for bandwidth management. Most users either don't care or use external tools (router QoS) for this.

**Independent Test**: Can be tested by setting a speed limit schedule and verifying download speed changes at the scheduled boundary.

**Acceptance Scenarios**:

1. **Given** a speed limit schedule of `09:00-17:00:512KB`, **When** a download is active and the clock crosses 09:00, **Then** the download speed is throttled to 512 KB/s.
2. **Given** the same schedule, **When** the clock crosses 17:00, **Then** the speed limit is lifted and the download proceeds at full speed.

---

### Edge Cases

- What happens when the user specifies `--start-at` with an invalid date format? System must reject with a clear format example.
- What happens when `--start-at` and `--start-in` are both specified? System must reject with an error — mutually exclusive flags.
- What happens when the cookie SQLite database is locked by the browser? System must handle the lock gracefully (copy the file first or retry with a clear message).
- What happens when the cookie file is corrupted or uses an unexpected schema version? System must report the error clearly, not crash.
- What happens when `--cookies-from auto` finds a browser but the cookie store is empty for the target domain? System must warn "No cookies found for domain X" and proceed without cookies.
- What happens when a scheduled download's URL becomes unavailable by the scheduled time? System must report the failure just like a normal download failure — retry logic applies.
- What happens when the system clock changes (NTP sync, manual change, DST) while a download is scheduled? System must use monotonic time for relative schedules and wall-clock time for absolute schedules, handling the difference gracefully.
- What happens when multiple downloads are scheduled for the exact same time? System must respect the existing queue/concurrency limits and queue overflow downloads.
- What happens when the daemon runs out of disk space before a scheduled download starts? System must check available space before starting and report the issue.
- What happens when a Netscape cookie file has malformed lines? System must skip invalid lines, warn the user, and continue with valid cookies.
- What happens on platforms where browser cookie paths differ (macOS vs Linux)? System must use platform-appropriate default paths.
- What happens when Chrome cookies are encrypted (as they are by default on macOS/Linux)? System must handle decryption or inform the user that encrypted cookies require a Netscape-format export.

## Requirements *(mandatory)*

### Functional Requirements

**Scheduling (Issue #140)**

- **FR-001**: System MUST accept an `--start-at` flag with an absolute datetime in `YYYY-MM-DD HH:MM` format (local timezone only — no timezone suffix or explicit timezone flag supported) to schedule a download for a specific time.
- **FR-002**: System MUST accept a `--start-in` flag with a relative duration to schedule a download for a time offset from now. Duration format uses Go's `time.ParseDuration` syntax: `h` (hours), `m` (minutes), `s` (seconds), and compounds (e.g., `1h30m`). Days are not supported — use `24h`. A zero duration (`0s`, `0m`) is valid and equivalent to immediate start.
- **FR-003**: System MUST reject commands where both `--start-at` and `--start-in` are provided, with a clear error message.
- **FR-004**: System MUST display scheduled downloads in `warpdl list` output with their scheduled start time and current status ("scheduled", "downloading", "completed", "failed").
- **FR-005**: System MUST persist all scheduled downloads to disk so they survive daemon restarts.
- **FR-006**: System MUST automatically start scheduled downloads that were missed during daemon downtime upon daemon restart by enqueuing them at normal priority into the existing queue. The queue concurrency cap naturally throttles simultaneous starts.
- **FR-007**: System MUST warn the user when `--start-at` specifies a time in the past and start immediately. No interactive prompt — the warning is informational only. This applies to all contexts (interactive CLI, piped input, scripted invocations).
- **FR-008**: System MUST support cancelling a scheduled download before it starts via the existing `warpdl stop <id>` command. For recurring downloads, `warpdl stop <id>` permanently cancels the entire recurring schedule (all future executions). A recurring download is a single logical entity — stop means stop everything.
- **FR-009**: System MUST accept a `--schedule` flag with a cron expression for recurring downloads. MAY be combined with `--start-at` or `--start-in` to delay the first occurrence; subsequent occurrences follow the cron expression.
- **FR-009a**: System MUST append a timestamp suffix to the output filename for each recurring download trigger (format: `<basename>-<YYYY-MM-DDTHHMMSS>.<ext>`, e.g., `backup-2026-03-01T020000.sql.gz`). This prevents silent data loss from overwriting previous downloads.
- **FR-010**: System MUST respect existing queue concurrency limits when multiple scheduled downloads trigger simultaneously.

**Cookie Import (Issue #141)**

- **FR-011**: System MUST accept a `--cookies-from <path|auto>` flag to import cookies from a file.
- **FR-012**: System MUST auto-detect the cookie file format (Firefox SQLite, Chrome SQLite, Netscape text) based on file content, not just extension.
- **FR-013**: System MUST support Firefox cookie stores (`cookies.sqlite` with `moz_cookies` table).
- **FR-014**: System MUST support Chrome/Chromium cookie stores (SQLite `Cookies` database with `cookies` table) when cookies are unencrypted. Encrypted Chrome cookies (default on macOS/Linux) are out of scope; users with encrypted Chrome cookies should export to Netscape format using a browser extension.
- **FR-015**: System MUST support Netscape/Mozilla cookie text format (tab-separated, standard header line).
- **FR-016**: When `auto` is specified, system MUST scan known browser paths in a documented priority order and use the first valid cookie store found.
- **FR-017**: System MUST only import cookies matching the download URL's domain (including subdomains).
- **FR-018**: System MUST skip expired cookies during import.
- **FR-019**: System MUST display the count of imported cookies to the user (e.g., "Imported 12 cookies for example.com").
- **FR-020**: System MUST NOT log or display actual cookie values (names and domains only for debugging).
- **FR-021**: System MUST handle locked cookie databases gracefully (e.g., copy to temp file before reading, or clear error message).
- **FR-022**: System MUST support platform-specific browser cookie paths (macOS `~/Library/Application Support/`, Linux `~/.config/`, `~/.mozilla/`).
- **FR-023**: System MUST NOT persist imported cookie values to disk. Cookies are held in-memory only and re-imported from the source path on download resume, retry, or recurring schedule trigger.
- **FR-024**: System MUST persist the cookie source path (`--cookies-from` value) with the download item so that cookies can be re-imported on resume or recurring trigger.

### Key Entities

- **Scheduled Download**: An `Item` (from `pkg/warplib`) extended with optional scheduling fields: `ScheduledAt time.Time` (zero value = not scheduled), `CronExpr string` (empty = one-shot), `ScheduleState string` (enum: `""`, `"scheduled"`, `"triggered"`, `"missed"`, `"cancelled"`). No separate entity — scheduling is part of the `Item` struct, persisted via existing GOB serialization.
- **Cookie Source**: A reference to a cookie store file with its detected format (Firefox, Chrome, Netscape), the source browser name, and the file path. The source path is persisted with the download item; cookie values are never persisted.
- **Imported Cookie Set**: A collection of cookies filtered by domain, with metadata about source browser, import timestamp, and count. Cookie values are treated as opaque sensitive data — never logged.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Users can schedule a download for a specific time using a single command and have it start within 60 seconds of the scheduled time.
- **SC-002**: Scheduled downloads survive daemon restarts with zero data loss — 100% of persisted schedules are restored correctly.
- **SC-003**: Users can import browser cookies and complete an authenticated download in a single command, without manually extracting or formatting cookies.
- **SC-004**: Auto-detection of browser cookie stores works correctly on both macOS and Linux for Firefox and Chrome with default profile locations.
- **SC-005**: Cookie import adds no more than 2 seconds of overhead to download startup time for stores containing up to 10,000 cookies.
- **SC-006**: All scheduling and cookie import features maintain the existing 80%+ test coverage requirement per package.
- **SC-007**: Invalid inputs (bad date formats, corrupt cookie files, locked databases) produce clear, actionable error messages — no panics or cryptic stack traces.

## Assumptions

- The daemon is the sole process managing scheduled downloads. If the daemon is not running at the scheduled time, the download starts when the daemon next starts.
- Relative time (`--start-in`) is resolved to an absolute time at command submission and persisted as an absolute time. The daemon does not track "2 hours from submission."
- Chrome encrypted cookies (DPAPI on Windows, Keychain on macOS, Secret Service on Linux) are out of scope. FR-014 applies only to unencrypted Chrome cookie databases. Users with encrypted Chrome cookies should export to Netscape format using a browser extension.
- Firefox cookies are stored unencrypted in SQLite and can be read directly.
- The cron expression parser follows standard 5-field cron syntax (minute, hour, day-of-month, month, day-of-week).
- Speed limit scheduling (P3) builds on top of an existing or new bandwidth throttling capability in the download engine.
- The queue manager (#135) integration is out of scope for this spec — scheduled downloads simply initiate a normal download when triggered, and the queue handles concurrency.
