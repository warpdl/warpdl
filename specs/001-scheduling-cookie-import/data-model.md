# Data Model: Download Scheduling & Browser Cookie Import

**Date**: 2026-02-28 | **Branch**: `001-scheduling-cookie-import`

## Entity Diagram

```
┌─────────────────────────────────────────────────────────────────────┐
│ Item (pkg/warplib/item.go) — EXTENDED                               │
├─────────────────────────────────────────────────────────────────────┤
│ EXISTING FIELDS (unchanged):                                        │
│   Hash            string        PK, content-addressable             │
│   Name            string        filename                            │
│   Url             string        download URL                        │
│   Headers         map[str]str   HTTP headers (incl Cookie header)   │
│   DateAdded       time.Time     creation timestamp                  │
│   TotalSize       int64         bytes                               │
│   Downloaded      int64         bytes completed                     │
│   DownloadLocation string       directory path                      │
│   AbsoluteLocation string       full file path                      │
│   ChildHash       string        parent download hash                │
│   Hidden          bool          internal item flag                  │
│   Children        []string      child download hashes               │
│   Parts           []*ItemPart   segment state                       │
│   Resumable       bool          supports HTTP range                 │
│   Protocol        Protocol      HTTP/FTP/SFTP enum                  │
│   SSHKeyPath      string        SFTP key path                       │
│                                                                     │
│ NEW FIELDS (scheduling):                                            │
│   ScheduledAt     time.Time     zero = not scheduled                │
│   CronExpr        string        empty = one-shot                    │
│   ScheduleState   ScheduleState enum (see below)                    │
│                                                                     │
│ NEW FIELDS (cookies):                                               │
│   CookieSourcePath string       path to cookie file or "auto"       │
│                                  empty = no cookies                  │
├─────────────────────────────────────────────────────────────────────┤
│ GOB Serialized: YES (all fields)                                    │
│ Backward Compatible: YES (zero values are safe defaults)            │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│ ScheduleState (pkg/warplib/item.go) — NEW enum type                 │
├─────────────────────────────────────────────────────────────────────┤
│   ""           default — item is not scheduled                      │
│   "scheduled"  waiting for trigger time                             │
│   "triggered"  trigger time reached, enqueued for download          │
│   "missed"     trigger time passed during daemon downtime           │
│   "cancelled"  user cancelled before trigger                        │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│ DownloadParams (common/types.go) — EXTENDED                         │
├─────────────────────────────────────────────────────────────────────┤
│ EXISTING FIELDS (unchanged):                                        │
│   Url             string                                            │
│   FileName        string                                            │
│   DownloadDir     string                                            │
│   Headers         map[string]string                                 │
│   Connections     int                                               │
│   ...other existing fields...                                       │
│                                                                     │
│ NEW FIELDS:                                                         │
│   StartAt         string        "YYYY-MM-DD HH:MM" or ""           │
│   StartIn         string        duration like "2h", "30m" or ""     │
│   Schedule        string        cron expression or ""               │
│   CookiesFrom     string        file path, "auto", or ""           │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│ Cookie (internal/cookies/types.go) — NEW type (in-memory only)      │
├─────────────────────────────────────────────────────────────────────┤
│   Name       string       cookie name                               │
│   Value      string       cookie value (SENSITIVE — never logged)   │
│   Domain     string       target domain                             │
│   Path       string       cookie path                               │
│   Expiry     time.Time    expiration time                           │
│   Secure     bool         HTTPS-only flag                           │
│   HttpOnly   bool         HttpOnly flag                             │
├─────────────────────────────────────────────────────────────────────┤
│ GOB Serialized: NO (in-memory only per FR-023)                      │
│ Logged: NEVER (names/domains only for debug per FR-020)             │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│ CookieSource (internal/cookies/types.go) — NEW type                 │
├─────────────────────────────────────────────────────────────────────┤
│   Path       string          file path to cookie store              │
│   Format     CookieFormat    detected format enum                   │
│   Browser    string          detected browser name                  │
├─────────────────────────────────────────────────────────────────────┤
│ CookieFormat enum:                                                  │
│   FormatUnknown  = 0                                                │
│   FormatFirefox  = 1   (moz_cookies SQLite)                         │
│   FormatChrome   = 2   (cookies SQLite, unencrypted only)           │
│   FormatNetscape = 3   (tab-separated text)                         │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│ ScheduleEvent (internal/scheduler/types.go) — NEW type              │
├─────────────────────────────────────────────────────────────────────┤
│   ItemHash    string       reference to Item                        │
│   TriggerAt   time.Time    when to fire                             │
│   CronExpr    string       for recurring: recompute next after fire │
├─────────────────────────────────────────────────────────────────────┤
│ Used internally by scheduler heap. Not persisted — rebuilt from     │
│ Item fields on daemon restart.                                      │
└─────────────────────────────────────────────────────────────────────┘
```

## Relationships

```
Item (1) ──────── (0..1) ScheduleEvent    [scheduler heap, in-memory]
Item (1) ──────── (0..N) Cookie           [imported set, in-memory]
Item.CookieSourcePath ──> CookieSource    [resolved at import time]
```

## State Transitions

### Schedule State Machine

```
                 ┌──────────────┐
    CLI submit   │              │
   ──────────>   │  "scheduled" │
                 │              │
                 └──────┬───────┘
                        │
            ┌───────────┼───────────┐
            │           │           │
     trigger time    daemon was   user runs
       reached       down when    warpdl stop
            │        time hit        │
            v           │            v
     ┌──────────┐      │     ┌────────────┐
     │"triggered"│      │     │ "cancelled" │
     └──────┬───┘      │     └────────────┘
            │           v           (terminal)
            │    ┌──────────┐
            │    │ "missed"  │──> enqueue immediately
            │    └──────┬───┘    on daemon restart
            │           │
            v           v
     ┌──────────┐  ┌──────────┐
     │"triggered"│  │"triggered"│
     └──────┬───┘  └──────┬───┘
            │              │
            v              v
       [normal download flow — queue handles from here]
            │
            v
     ┌──────────────┐
     │  If recurring │──> compute next CronExpr occurrence
     │  (CronExpr)   │   set ScheduledAt = next, state = "scheduled"
     └──────────────┘
```

### State Transition Rules (CHK068)

| From | To | Trigger | Notes |
|------|----|---------|-------|
| `""` | `"scheduled"` | CLI submits `--start-at`, `--start-in`, or `--schedule` | Initial state |
| `"scheduled"` | `"triggered"` | Trigger time reached (daemon running) | Enqueued for download |
| `"scheduled"` | `"missed"` | Trigger time passed during daemon downtime | Detected on daemon restart |
| `"scheduled"` | `"cancelled"` | User runs `warpdl stop <id>` | Terminal state |
| `"missed"` | `"triggered"` | Daemon restart detects missed schedule | Enqueued immediately |
| `"triggered"` | `"scheduled"` | Recurring (CronExpr != ""): download completes or fails | Next cron occurrence computed |
| `"triggered"` | `""` | One-shot: download completes or fails | Returns to normal item |
| `"cancelled"` | *(none)* | Terminal state | No transitions out |

**Key constraint (CHK068)**: "missed" → "cancelled" is NOT a valid transition. Missed downloads are enqueued immediately on daemon restart — they transition to "triggered" before a user could cancel. If the user runs `warpdl stop` after the download starts, the stop targets the active download (normal stop flow), not the schedule state.

### Cookie Import Flow

```
     --cookies-from <path|auto>
              │
              v
     ┌────────────────┐
     │ Resolve source  │──> auto: scan known paths in priority order
     │                 │    path: use as-is
     └────────┬───────┘
              │
              v
     ┌────────────────┐
     │ Detect format   │──> SQLite magic bytes → check table names
     │                 │    Text header → Netscape format
     └────────┬───────┘
              │
              v
     ┌────────────────┐
     │ Copy to temp    │──> copy .sqlite + -wal + -shm to temp dir
     │ (lock safety)   │    open with ?immutable=1
     └────────┬───────┘
              │
              v
     ┌────────────────┐
     │ Parse & filter  │──> SELECT by domain (incl. subdomains)
     │                 │    skip expired cookies
     └────────┬───────┘
              │
              v
     ┌────────────────┐
     │ Build Cookie    │──> "Cookie: name1=val1; name2=val2"
     │ header string   │    merge into Item.Headers
     └────────┬───────┘
              │
              v
     ┌────────────────┐
     │ Persist source  │──> Item.CookieSourcePath = path
     │ path only       │    cookie VALUES stay in-memory only
     └────────────────┘
```

## Validation Rules

### Scheduling Flags (CLI)

| Rule | Enforcement |
|------|-------------|
| `--start-at` and `--start-in` are mutually exclusive | CLI rejects with error (FR-003) |
| `--start-at` format must be `YYYY-MM-DD HH:MM` | `time.Parse("2006-01-02 15:04", value)` — reject on error (SC-007) |
| `--start-in` format must be Go duration (`2h`, `30m`, `1h30m`) | `time.ParseDuration(value)` — reject on error |
| `--schedule` must be valid 5-field cron | `gronx.IsValid(value)` — reject on error |
| `--start-at` in the past triggers warning | Warn user, prompt or start immediately (FR-007) |
| `--schedule` combined with `--start-at` or `--start-in` | Allowed — first occurrence can be delayed |

### Cookie Import

| Rule | Enforcement |
|------|-------------|
| Cookie source file must exist and be readable | `os.Stat()` + `os.Open()` — clear error on failure |
| Cookie source must be a file, not a directory | `os.Stat().IsDir()` — reject with `"error: {path} is a directory"` (CHK012) |
| SQLite databases must not be corrupt | Catch driver errors on open/query — report clearly (SC-007) |
| Zero-byte or truncated files are rejected | `os.Stat().Size() == 0` or SQLite driver error — report `"empty or corrupted"` (CHK049) |
| Domain filtering includes subdomains | Match exact `example.com`, dot-prefix `.example.com`, and wildcard `%.example.com` (CHK016, CHK025) |
| Expired cookies are excluded | Compare `expiry` against current Unix time (FR-018) |
| Cookie values are NEVER logged at ANY level | Log `name` and `domain` at DEBUG level only (FR-020, CHK033, CHK039) |
| Cookie header in Item.Headers is redacted in logs | If header key is `Cookie` or `Set-Cookie`, log `"[REDACTED]"` (CHK034) |
| Chrome encrypted cookies are skipped | Check `value != ""` — skip rows with only `encrypted_value` (FR-014) |
| Malformed Netscape lines are skipped | Skip with warning, continue processing (edge case spec) |
| CRLF line endings handled | `bufio.Scanner` with default `ScanLines` handles both `\n` and `\r\n` (CHK060) |
| Temp file cleanup guaranteed | `defer os.RemoveAll(tempDir)` immediately after creation — runs on all paths (CHK037) |
| Symlinks followed | Standard `os.Open` behavior, no restriction (CHK036, CHK058) |
| Unsupported schema version | Catch "no such table" or "no such column" — report `"unsupported cookie database schema"` (CHK055) |

## Persistence Summary

| Data | Persisted? | Storage | Notes |
|------|-----------|---------|-------|
| Item scheduling fields | YES | GOB (`userdata.warp`) | ScheduledAt, CronExpr, ScheduleState, CookieSourcePath |
| Cookie values | NO | In-memory only | Re-imported from source on resume/retry/recurring (FR-023) |
| Scheduler heap | NO | In-memory only | Rebuilt from Item fields on daemon restart |
| Cookie source path | YES | GOB (via Item field) | For re-import on resume/retry (FR-024) |
