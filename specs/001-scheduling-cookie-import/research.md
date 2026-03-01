# Research: Download Scheduling & Browser Cookie Import

**Date**: 2026-02-28 | **Branch**: `001-scheduling-cookie-import`

## 1. Pure-Go SQLite Library

### Decision: `modernc.org/sqlite` v1.46.1

### Rationale
- Only pure-Go SQLite implementation that supports WAL mode (Firefox uses WAL for cookies.sqlite while the browser is running)
- Standard `database/sql` interface — no custom API to learn
- Actively maintained (latest release 2026-02-18), 2,562+ importers
- BSD-3-Clause license — compatible with project MIT-like license
- Works with `CGO_ENABLED=0` (constitution Principle I mandate)

### Alternatives Considered

| Library | Verdict | Why Rejected |
|---------|---------|-------------|
| `zombiezen.com/go/sqlite` | Viable but unnecessary | Wraps modernc internally; forces non-standard callback API; ~105 importers vs 2,562 |
| `ncruces/go-sqlite3` | Viable but heavier | Uses WASM/wazero runtime; adds per-connection memory overhead; +4.7MB vs +4.1MB |
| `alicebob/sqlittle` | Disqualified | Does NOT support WAL mode; last commit Sep 2020; dead project |
| `go-sqlite/sqlite3` | Disqualified | Work-in-progress; cannot iterate row data |
| `mattn/go-sqlite3` | Disqualified | Requires CGO — violates constitution Principle I |

### Implementation Notes
- **Binary impact**: +4.1 MB (~24% increase from ~17MB to ~21MB). Unavoidable — no lighter pure-Go option handles WAL.
- **Lock handling**: Copy the database file (plus `-wal` and `-shm` files if present) to a temp directory before reading. Open with `?immutable=1` URI parameter. This is standard practice for every cookie extraction tool.
- **No dependency conflicts** with existing WarpDL dependency tree.

---

## 2. Cron Expression Parser

### Decision: `adhocore/gronx`

### Rationale
- Zero dependencies (`go.mod` is two lines) — aligns with constitution Principle VII (simplicity)
- MIT license — no GPL concerns
- Parser-only design — no unnecessary scheduler framework
- `NextTickAfter(expr, time, inclusive)` computes next cron occurrence
- `PrevTickBefore(expr, time, inclusive)` directly solves FR-006 (detecting missed schedules on daemon restart)
- `IsValid(expr)` provides cheap validation — aligns with SC-007 (no panics on invalid input)
- Returns errors instead of panicking (unlike the cronexpr family)
- Actively maintained (v1.19.6, April 2024)

### Alternatives Considered

| Library | Verdict | Why Rejected |
|---------|---------|-------------|
| `robfig/cron` v3 | Dead code | Unmaintained since Jan 2021; 111 open issues; 55 unmerged PRs; known panic bugs; known DST issues |
| `gorhill/cronexpr` | Disqualified | Archived since Jan 2019; GPL v3 dual-license |
| `aptible/supercronic/cronexpr` | Disqualified | Inherits GPL v3 from gorhill fork; embedded in larger project |
| `hashicorp/cronexpr` | License concern | GPL v3 / Apache 2.0 dual-license; maintained but GPL is problematic |
| `netresearch/go-cron` | Disqualified | 28 stars; behavioral deviations from standard cron; requires Go 1.25+ |

### Implementation Notes
- Re-parses expression on every `NextTickAfter` call (no compiled state). Irrelevant at WarpDL's scale — called once per schedule evaluation tick, not in a hot loop.
- Standard 5-field cron format supported out of the box.

---

## 3. Browser Cookie Database Schemas

### Firefox (`moz_cookies`)

```sql
CREATE TABLE moz_cookies (
  id                 INTEGER PRIMARY KEY,
  originAttributes   TEXT    NOT NULL DEFAULT '',
  name               TEXT,
  value              TEXT,
  host               TEXT,          -- leading dot = subdomain-inclusive
  path               TEXT,
  expiry             INTEGER,       -- Unix seconds
  lastAccessed       INTEGER,       -- microseconds since Unix epoch
  creationTime       INTEGER,       -- microseconds since Unix epoch
  isSecure           INTEGER,       -- 0 or 1
  isHttpOnly         INTEGER,       -- 0 or 1
  inBrowserElement   INTEGER DEFAULT 0,
  sameSite           INTEGER DEFAULT 0, -- 0=None, 1=Lax, 2=Strict, 256=Unset
  rawSameSite        INTEGER DEFAULT 0,
  schemeMap          INTEGER DEFAULT 0  -- bitmask: 0x01=HTTP, 0x02=HTTPS, 0x04=FILE
);
```

**Key facts**:
- Values are **unencrypted plaintext** — safe to read directly
- Expiry is **Unix seconds** (not milliseconds)
- Domain cookies have **leading dot** in `host` column (e.g., `.example.com`)
- Schema stable since Firefox 104 (2022)
- WAL mode used while browser is running

**Query for domain-filtered extraction**:
```sql
SELECT name, value, host, path, expiry, isSecure, isHttpOnly
FROM moz_cookies
WHERE (host = ? OR host = ? OR host LIKE ?)
  AND expiry > ?
ORDER BY path DESC, name ASC;
-- Params: 'example.com', '.example.com', '%.example.com', unixNow
```

### Chrome/Chromium (`cookies`)

```sql
CREATE TABLE cookies (
  creation_utc       INTEGER NOT NULL,   -- microseconds since 1601-01-01
  host_key           TEXT    NOT NULL,
  top_frame_site_key TEXT    NOT NULL,
  name               TEXT    NOT NULL,
  value              TEXT    NOT NULL,    -- plaintext (empty if encrypted)
  encrypted_value    BLOB   DEFAULT '',   -- encrypted blob
  path               TEXT    NOT NULL,
  expires_utc        INTEGER NOT NULL,    -- microseconds since 1601-01-01
  is_secure          INTEGER NOT NULL,
  is_httponly         INTEGER NOT NULL,
  samesite           INTEGER NOT NULL,    -- -1=Unspecified, 0=None, 1=Lax, 2=Strict
  last_access_utc    INTEGER NOT NULL,
  has_expires        INTEGER NOT NULL DEFAULT 1,
  is_persistent      INTEGER NOT NULL DEFAULT 1,
  priority           INTEGER NOT NULL DEFAULT 1,
  source_scheme      INTEGER NOT NULL DEFAULT 0,
  source_port        INTEGER NOT NULL DEFAULT -1,
  last_update_utc    INTEGER NOT NULL DEFAULT 0,
  source_type        INTEGER NOT NULL DEFAULT 0,
  has_cross_site_ancestor INTEGER NOT NULL DEFAULT 0
);
```

**Key facts**:
- `value` is plaintext; if empty, check `encrypted_value` BLOB
- **Encrypted by default on macOS/Linux** — most cookies will only have `encrypted_value` populated
- Timestamps are **microseconds since 1601-01-01** (Windows FILETIME epoch)
- Convert to Unix: `(chrome_usec / 1_000_000) - 11_644_473_600`
- Schema version 24 (Chrome 130+) introduced SHA256 domain prefix on encrypted values
- SameSite: -1=Unspecified, 0=None, 1=Lax, 2=Strict

**Query for unencrypted cookies only (per FR-014)**:
```sql
SELECT name, value, host_key, path, expires_utc, is_secure, is_httponly
FROM cookies
WHERE (host_key = ? OR host_key = ? OR host_key LIKE ?)
  AND value != ''
  AND expires_utc > ?
ORDER BY path DESC, name ASC;
-- Params: 'example.com', '.example.com', '%.example.com', chromeTimestampNow
```

### Netscape/Mozilla Cookie Text Format

```
# Netscape HTTP Cookie File
# https://curl.se/docs/http-cookies.html
.example.com	TRUE	/	FALSE	1893456000	session_id	abc123
```

**Field order** (tab-separated):
1. `domain` — Leading dot means subdomain-inclusive
2. `subdomain_flag` — `TRUE` or `FALSE` (redundant with leading dot; TRUE if dot-prefixed)
3. `path` — Cookie path
4. `secure` — `TRUE` or `FALSE`
5. `expiry` — Unix seconds (0 = session cookie)
6. `name` — Cookie name
7. `value` — Cookie value

**Edge cases**:
- Lines starting with `#` are comments — **EXCEPT** `#HttpOnly_` prefix on domain indicates HttpOnly cookie
- Empty lines and whitespace-only lines must be skipped
- Malformed lines (wrong field count) must be skipped with warning per edge case spec

### Browser Family Compatibility

| Browser | Cookie Schema | Encrypted? |
|---------|--------------|-----------|
| Firefox | `moz_cookies` | No (all platforms, all variants) |
| LibreWolf | `moz_cookies` (identical) | No |
| Chrome | `cookies` | Yes (DPAPI on Windows, Keychain on macOS, Secret Service on Linux) |
| Chromium | `cookies` (identical) | Yes (same as Chrome) |
| Microsoft Edge | `cookies` (identical) | Yes (same as Chrome) |
| Brave | `cookies` (identical) | Yes (same as Chrome) |

### Firefox Encryption Validation (CHK062)

Firefox stores cookies unencrypted in SQLite across ALL variants: Release, ESR, Beta, Developer Edition, Nightly, and LibreWolf. Firefox has never implemented application-level cookie encryption (Mozilla Bugzilla #1331238 — filed but no action). Mozilla's position is that OS-level file permissions are the intended protection layer.

All Firefox variants use the same `moz_cookies` schema (version 10 since Firefox 104, 2022). The schema is stable across channels — ESR, Developer Edition, and Nightly use identical table structures. A `cookies.sqlite` from any variant is readable by the same parser.

---

## 4. Browser Cookie File Paths

### Firefox

| Platform | Path |
|----------|------|
| macOS | `~/Library/Application Support/Firefox/Profiles/<profile>/cookies.sqlite` |
| Linux | `~/.mozilla/firefox/<profile>/cookies.sqlite` |
| Linux (Snap) | `~/snap/firefox/common/.mozilla/firefox/<profile>/cookies.sqlite` |
| Windows | `%APPDATA%\Mozilla\Firefox\Profiles\<profile>\cookies.sqlite` |

**Profile resolution**: Parse `profiles.ini` — check `[Install*]` sections first for `Default=` key, then fall back to `[Profile*]` sections with `Default=1`.

- macOS profiles.ini: `~/Library/Application Support/Firefox/profiles.ini`
- Linux profiles.ini: `~/.mozilla/firefox/profiles.ini`
- Linux Snap profiles.ini: `~/snap/firefox/common/.mozilla/firefox/profiles.ini`
- Windows profiles.ini: `%APPDATA%\Mozilla\Firefox\profiles.ini`

### Chrome

| Platform | Path |
|----------|------|
| macOS | `~/Library/Application Support/Google/Chrome/Default/Cookies` or `Default/Network/Cookies` |
| Linux | `~/.config/google-chrome/Default/Cookies` or `Default/Network/Cookies` |
| Windows | `%LOCALAPPDATA%\Google\Chrome\User Data\Default\Network\Cookies` (or legacy `Default\Cookies`) |

Must check both paths — Chrome v96 migrated Cookies into `Network/` subdirectory. On Windows, encryption uses DPAPI (AES-256-GCM with key from `Local State`).

### Chromium

| Platform | Path |
|----------|------|
| macOS | `~/Library/Application Support/Chromium/Default/Cookies` (or `Network/Cookies`) |
| Linux | `~/.config/chromium/Default/Cookies` (or `Network/Cookies`) |
| Windows | `%LOCALAPPDATA%\Chromium\User Data\Default\Network\Cookies` (or legacy `Default\Cookies`) |

### Microsoft Edge

| Platform | Path |
|----------|------|
| macOS | `~/Library/Application Support/Microsoft Edge/Default/Cookies` (or `Network/Cookies`) |
| Linux | `~/.config/microsoft-edge/Default/Cookies` (or `Network/Cookies`) |
| Windows | `%LOCALAPPDATA%\Microsoft\Edge\User Data\Default\Network\Cookies` (or legacy `Default\Cookies`) |

### LibreWolf

| Platform | Path |
|----------|------|
| macOS | `~/Library/Application Support/librewolf/Profiles/<profile>/cookies.sqlite` |
| Linux | `~/.librewolf/<profile>/cookies.sqlite` |
| Windows | `%APPDATA%\LibreWolf\Profiles\<profile>\cookies.sqlite` |

Note: LibreWolf uses `%APPDATA%` (Roaming) on Windows, not `%LOCALAPPDATA%`.

### Brave

| Platform | Path |
|----------|------|
| macOS | `~/Library/Application Support/BraveSoftware/Brave-Browser/Default/Cookies` (or `Network/Cookies`) |
| Linux | `~/.config/BraveSoftware/Brave-Browser/Default/Cookies` (or `Network/Cookies`) |
| Windows | `%LOCALAPPDATA%\BraveSoftware\Brave-Browser\User Data\Default\Network\Cookies` (or legacy `Default\Cookies`) |

### Auto-Detection Priority Order

1. **Firefox** — Unencrypted, most reliable. Highest success rate.
2. **LibreWolf** — Unencrypted (Firefox fork). Same schema.
3. **Chrome** — Encrypted by default; only unencrypted cookies are usable (FR-014). Warn user if all cookies are encrypted.
4. **Chromium** — Same as Chrome.
5. **Edge** — Same as Chrome.
6. **Brave** — Same as Chrome.

### Existing Codebase Pattern

`internal/nativehost/manifest.go` lines 76-113 already implements platform-specific browser path resolution using `switch runtime.GOOS`. This is the reference pattern to follow for cookie path resolution.

### File Locking Strategy

All browsers lock their SQLite databases while running. The approach:
1. Copy the `.sqlite` file to a temp directory
2. Also copy `-wal` and `-shm` files if they exist (required for WAL checkpoint)
3. Open the copy with `?immutable=1` URI parameter
4. Read cookies, close connection
5. Delete temp files

---

## 5. Daemon Scheduling Architecture

### Decision: Single Scheduler Goroutine with Min-Heap + 60s Max-Sleep-Cap

### Rationale

Go's `time.Timer` uses the monotonic clock internally. On macOS, the monotonic clock pauses during system sleep, causing timers to fire late. There is no `time.NewTimerAt(time.Time)` in stdlib.

The max-sleep-cap pattern solves this:
1. Compute `sleepDuration = min(timeUntilNextEvent, 60s)`
2. Sleep for `sleepDuration`
3. On wake, re-evaluate `time.Now()` against the heap head's wall-clock target
4. If target is in the past, fire the event
5. Repeat

This bounds worst-case delay to 60 seconds (meets SC-001), handles NTP steps, DST transitions, and system sleep.

### Alternatives Considered

| Approach | Verdict | Why Rejected |
|----------|---------|-------------|
| `time.Timer` per item | Fragile | Monotonic clock pauses on macOS sleep; 100 timers = 100 goroutine wakeups; timer reset races in Go <1.23 |
| Ticker-based polling (30s) | Viable but wasteful | CPU overhead on every tick even with zero scheduled items; 30s worst-case delay is fine but heap is no harder |
| `go-co-op/gocron` v2 | Overkill | 17 dependencies; no persistence; no missed-schedule detection; full framework when we need a simple loop |

### Design

```
Scheduler goroutine:
  heap = min-heap of (triggerTime, itemHash) sorted by triggerTime

  loop:
    if heap is empty:
      block on channel (wait for new schedule or shutdown)
    else:
      sleepDuration = min(time.Until(heap[0].triggerTime), 60s)
      select {
        case <-time.After(sleepDuration):
          now = time.Now()
          while heap[0].triggerTime <= now:
            fire(heap.Pop())
        case schedule := <-addChan:
          heap.Push(schedule)
        case <-ctx.Done():
          return
      }
```

### Channel-Based Active Object Pattern
- `addChan chan ScheduleEvent` — add new scheduled item
- `removeChan chan string` — cancel scheduled item by hash
- `ctx.Done()` — shutdown signal
- No mutexes needed — single goroutine owns all state

### Missed Schedule Detection (Daemon Restart)
On startup, scheduler scans all items via `manager.GetItems()`:
- If `ScheduleState == "scheduled"` and `ScheduledAt.Before(time.Now())`:
  - Set `ScheduleState = "missed"`, enqueue for immediate download
- If `CronExpr != ""`:
  - Compute next occurrence via `gronx.NextTickAfter()`
  - Re-add to heap

### Clock Change Handling
- **NTP correction**: Max-sleep-cap re-evaluates wall clock every 60s. NTP step of a few seconds is absorbed.
- **DST transition (spring-forward)**: A cron job scheduled for a non-existent time (e.g., 2:30 AM during spring-forward) will have `gronx.NextTickAfter()` return the next valid occurrence. Go's `time.Date` normalizes non-existent times (2:30 AM → 3:30 AM). The scheduler re-checks wall clock on every wake, so worst case it fires at the normalized time.
- **DST transition (fall-back)**: Go's `time.Parse` with `time.Local` returns the first occurrence of an ambiguous time. A cron job scheduled during the repeated hour may fire once at the first occurrence. This matches standard crontab behavior.
- **System sleep (macOS)**: Monotonic clock pauses. On wake, timer fires immediately, re-evaluates wall clock, fires any past-due events.

### gronx DST Validation (CHK066)

`adhocore/gronx` has no explicit DST handling — it delegates entirely to Go's `time` package. `NextTickAfter()` preserves the reference time's `time.Location` throughout all computations. This is acceptable because:

1. Spring-forward gaps: Go's `time.Date()` normalizes the nonexistent time to a valid wall-clock time. The scheduler's 60s max-sleep-cap ensures re-evaluation against wall clock.
2. Fall-back duplicates: Go's `time.Parse` returns the first occurrence. Consistent and deterministic.
3. Same behavior as standard crontab — this is not a defect, it's inherent to wall-clock scheduling.

**Recommendation**: For use cases where DST precision matters, users should define cron expressions targeting times outside the 1:00-3:00 AM DST transition window.

---

## 6. Package Placement

### Decision

| Component | Location | Rationale |
|-----------|----------|-----------|
| Cookie import logic | NEW `internal/cookies/` | Auth infrastructure, not core download engine. Platform-specific code. Independent from scheduling. |
| Scheduling fields | Extend `pkg/warplib/item.go` | Flat model matches existing GOB persistence. No new entity. Zero-value defaults are backward compatible. |
| Scheduler goroutine | NEW `internal/scheduler/` | Daemon-level component. Separate from download engine. Owns its own goroutine and heap. |
| CLI flags | Modify `cmd/download.go` | Extend existing flag set. |
| API handlers | Modify `internal/api/download.go` | Hook cookie import before Downloader creation. Hook schedule check before `d.Start()`. |
| Daemon startup | Modify `cmd/daemon_core.go` | Instantiate scheduler after Manager creation. |

### GOB Backward Compatibility

All new fields on `Item` have safe zero values:
- `ScheduledAt time.Time{}` → not scheduled
- `CronExpr ""` → one-shot
- `ScheduleState ""` → normal (not scheduled)
- `CookieSourcePath ""` → no cookies

**GOB behavior validated (CHK061)**: Go's `encoding/gob` leaves fields present in the destination struct but absent in the encoded data at their current value. When decoding into a freshly zero-initialized struct (`var x T; decode(&x)`), absent fields remain at their Go zero values. This is the documented behavior and is safe for all non-pointer field types. Existing WarpDL pattern confirmed at `pkg/warplib/item.go` lines 48-56 (Protocol and SSHKeyPath fields added without migration).

**Important nuance**: Pointer fields pointing to zero values are skipped by gob during encoding (golang/go#11119). This does NOT affect our new fields — all are value types (`time.Time`, `string`), not pointers.

### New Dependencies Summary

| Dependency | Purpose | License | Size Impact |
|-----------|---------|---------|------------|
| `modernc.org/sqlite` v1.46.1 | Read Firefox/Chrome SQLite cookie databases | BSD-3-Clause | +4.1 MB binary |
| `adhocore/gronx` | Parse cron expressions, compute next/prev occurrence | MIT | Negligible |
