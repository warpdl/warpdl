# Quickstart: Download Scheduling & Browser Cookie Import

**Date**: 2026-02-28 | **Branch**: `001-scheduling-cookie-import`

## Prerequisites

- WarpDL daemon running (`warpdl daemon` or system service)
- Go 1.24.9+ for development

## Schedule a Download for Later

```bash
# Schedule for a specific time (local timezone)
warpdl download --start-at "2026-03-01 02:00" https://example.com/large-file.zip

# Schedule relative to now
warpdl download --start-in 2h https://example.com/large-file.zip

# Schedule recurring (cron: daily at 2 AM)
warpdl download --schedule "0 2 * * *" https://example.com/daily-backup.tar.gz
```

## Import Browser Cookies for Authenticated Downloads

```bash
# Auto-detect browser cookies (tries Firefox, LibreWolf, Chrome, etc.)
warpdl download --cookies-from auto https://protected.example.com/file.zip

# Specify Firefox cookie store explicitly
warpdl download --cookies-from ~/.mozilla/firefox/abc123.default/cookies.sqlite https://protected.example.com/file.zip

# Use a Netscape-format cookie export file
warpdl download --cookies-from cookies.txt https://protected.example.com/file.zip
```

## Combine Scheduling + Cookies

```bash
# Schedule an authenticated download for tonight
warpdl download \
  --start-at "2026-03-01 02:00" \
  --cookies-from auto \
  https://protected.example.com/nightly-export.zip
```

## Check Scheduled Downloads

```bash
warpdl list
# Shows scheduled time, status, and countdown for pending downloads
```

## Cancel a Scheduled Download

```bash
warpdl stop <download-id>
# Cancels the schedule — download will not start
```

## Development Quick Commands

```bash
# Run all tests
go test ./...

# Run scheduling tests
go test ./internal/scheduler/...

# Run cookie import tests
go test ./internal/cookies/...

# Run with race detection
go test -race -short ./...

# Build
make build
```
