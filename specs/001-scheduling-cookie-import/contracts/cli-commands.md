# CLI Command Contracts: Download Scheduling & Browser Cookie Import

**Date**: 2026-02-28 | **Branch**: `001-scheduling-cookie-import`

## Overview

WarpDL exposes its features through the `warpdl` CLI (urfave/cli v1). All new flags extend the existing `download` command. The `list` and `stop` commands gain awareness of scheduled items.

---

## Command: `warpdl download` (Extended)

### New Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--start-at` | `string` | `""` | Absolute datetime in `YYYY-MM-DD HH:MM` format (local timezone). Mutually exclusive with `--start-in`. |
| `--start-in` | `string` | `""` | Relative duration using Go `time.ParseDuration` syntax: `h` (hours), `m` (minutes), `s` (seconds), compounds like `1h30m`. Days not supported — use `24h`. `0s`/`0m` = immediate start. Resolved to absolute time at submission. Mutually exclusive with `--start-at`. |
| `--schedule` | `string` | `""` | 5-field cron expression for recurring downloads (e.g., `"0 2 * * *"` = daily 2 AM). |
| `--cookies-from` | `string` | `""` | Path to cookie file or `auto` for auto-detection. Formats: Firefox SQLite, Chrome SQLite (unencrypted), Netscape text. Auto-detection priority: Firefox, LibreWolf, Chrome, Chromium, Edge, Brave. |

### Flag Interactions

```
--start-at + --start-in           → ERROR: "flags --start-at and --start-in are mutually exclusive"
--start-at + --schedule           → OK: first run at --start-at, then recurring per cron
--start-in + --schedule           → OK: first run after delay, then recurring per cron
--cookies-from + any scheduling   → OK: cookies re-imported on each trigger
--cookies-from + no scheduling    → OK: cookies imported once for immediate download
```

### Request Schema (DownloadParams JSON over socket)

```json
{
  "url": "https://example.com/file.zip",
  "fileName": "file.zip",
  "downloadDir": "/home/user/Downloads",
  "headers": {},
  "connections": 8,
  "startAt": "2026-03-01 02:00",
  "startIn": "",
  "schedule": "",
  "cookiesFrom": "/path/to/cookies.sqlite"
}
```

### Response Schema

**Immediate download (no scheduling)**:
```json
{
  "hash": "abc123...",
  "status": "downloading",
  "message": "Download started"
}
```

**Scheduled download**:
```json
{
  "hash": "abc123...",
  "status": "scheduled",
  "scheduledAt": "2026-03-01 02:00",
  "message": "Download scheduled for 2026-03-01 02:00"
}
```

**Cookie import feedback** (displayed to user, not in JSON response):
```
Imported 12 cookies for example.com from Firefox (/path/to/cookies.sqlite)
```

### Error Responses

| Condition | Error Message |
|-----------|---------------|
| Both `--start-at` and `--start-in` specified | `error: flags --start-at and --start-in are mutually exclusive` |
| Invalid `--start-at` format | `error: invalid --start-at format, expected YYYY-MM-DD HH:MM (e.g., 2026-03-01 02:00)` |
| Invalid `--start-in` format | `error: invalid --start-in duration, expected format like 2h, 30m, or 1h30m (days not supported — use 24h)` |
| Invalid `--schedule` cron expression | `error: invalid cron expression "{expr}", expected 5-field format (minute hour day-of-month month day-of-week)` |
| Cron expression with no occurrence in next year | `warning: cron expression "{expr}" has no occurrence in the next year` |
| `--start-at` time is in the past | `warning: scheduled time is in the past, starting download immediately` |
| `--start-in 0s` or `--start-in 0m` | No warning — treated as immediate download (valid input) |
| Cookie file not found | `error: cookie file not found: {path}` |
| Cookie path is a directory | `error: {path} is a directory, expected a cookie file path or "auto"` |
| Cookie file is empty or truncated | `error: cookie file at {path} is empty or corrupted` |
| Cookie file format unrecognized | `error: unrecognized cookie file format at {path} (expected Firefox SQLite, Chrome SQLite, or Netscape text)` |
| Unsupported cookie database schema | `error: unsupported cookie database schema at {path} — expected Firefox moz_cookies or Chrome cookies table` |
| Cookie database locked and copy fails | `error: cookie database is locked (browser may be running), try closing the browser or exporting cookies to Netscape format` |
| Cookie database copy fails (disk full) | `error: failed to copy cookie database: {err}` |
| No cookies found for domain | `warning: no cookies found for {domain} in {path}, proceeding without cookies` |
| Chrome cookies are all encrypted | `warning: all cookies for {domain} in Chrome are encrypted, export to Netscape format using a browser extension (e.g., cookies.txt)` |
| `--cookies-from auto` finds no browser | `error: no supported browser cookie store found, supported browsers: Firefox, LibreWolf, Chrome, Chromium, Edge, Brave` |

---

## Command: `warpdl list` (Extended Display)

### Output Format (Scheduled Items)

```
ID       Status      Name                 Size     Progress   Scheduled                                      Cookies
abc123   scheduled   file.zip             1.2 GB   0%         2026-03-01 02:00                               —
def456   scheduled   backup.tar.gz        500 MB   0%         (recurring: 0 2 * * *, next: 2026-03-02 02:00) Firefox
ghi789   downloading report.pdf           50 MB    45%        —                                              cookies.txt
jkl012   missed      dataset.csv          2.1 GB   0%         was 2026-02-27 03:00 (starting now)            —
```

**New columns/fields**:
- `Status`: Now includes `scheduled`, `missed`, `cancelled` in addition to existing statuses
- `Scheduled`: Shows scheduled time for one-shot, cron expression + next time for recurring, `—` for unscheduled
- `Cookies`: Shows browser name (e.g., `Firefox`, `Chrome`) when cookies are from auto-detect or SQLite, shows filename (e.g., `cookies.txt`) for Netscape files, `—` when no cookies configured. Full cookie source path is only visible in debug mode (CHK003, CHK038)

---

## Command: `warpdl stop <id>` (Extended Behavior)

### Scheduled Item Cancellation

When `warpdl stop` targets a scheduled (not-yet-started) download:

```
$ warpdl stop abc123
Cancelled scheduled download "file.zip" (was scheduled for 2026-03-01 02:00)
```

- Sets `ScheduleState = "cancelled"`
- Removes from scheduler heap
- Item remains in `warpdl list` with `cancelled` status
- For recurring schedules: cancels ALL future occurrences (the schedule itself, not just the next run)

### Recurring Download Behavior

**On recurring trigger failure** (CHK005): The next cron occurrence still triggers regardless of whether the previous execution succeeded or failed. Each cron occurrence is independent. The failed download retains its failure status; the new occurrence creates a fresh download attempt with a new timestamp suffix.

**On daemon restart with missed recurring schedule**: The daemon detects that the schedule was missed, enqueues the missed download immediately, AND computes the next cron occurrence to re-add to the scheduler heap. Both happen on restart.

---

## Wire Protocol Changes

All new fields are added to existing `DownloadParams` JSON structure sent over the daemon socket. No new message types or update types are needed.

### Existing Protocol (4-byte length prefix + JSON payload)

```
[4 bytes: payload length][JSON payload]
```

New fields (`startAt`, `startIn`, `schedule`, `cookiesFrom`) are simply additional keys in the JSON payload. The daemon ignores unknown keys, so this is backward-compatible with older CLI clients talking to a newer daemon (they just won't send scheduling fields).

### Update Types (common/types.go)

No new `UpdateType` constants needed. Scheduled downloads use the existing download flow once triggered. The `warpdl list` command already reads items from the manager — it just needs to display the new fields.
