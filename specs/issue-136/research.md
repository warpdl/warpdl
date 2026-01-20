---
spec: issue-136
phase: research
created: 2026-01-19
generated: auto
---

# Research: Batch URL Download from Input File

## Executive Summary

Feature is straightforward to implement. Queue manager (#142) already handles concurrent download limits with priority support. CLI patterns established. Main work: input file parsing + download loop with error aggregation.

## Codebase Analysis

### Existing Patterns

| Pattern | Location | Notes |
|---------|----------|-------|
| CLI flag definition | `cmd/download.go:22-56` | `dlFlags` slice, use `cli.StringFlag` |
| URL validation | `cmd/download.go:111-123` | Trim whitespace, check empty |
| Download invocation | `cmd/download.go:158-175` | `client.Download()` call pattern |
| Cookie parsing | `cmd/cookie_parser.go:16-34` | Line-by-line validation pattern |
| Queue management | `pkg/warplib/queue.go` | `QueueManager.Add()` with priority |
| Error handling | `cmd/common/` | `PrintRuntimeErr()`, `PrintErrWithCmdHelp()` |

### Key Integration Points

1. **Download command** (`cmd/download.go:111`)
   - Currently takes single URL from `ctx.Args().First()`
   - Need to support both: single URL arg AND input file flag

2. **Queue Manager** (`pkg/warplib/queue.go`)
   - Already integrated with daemon
   - `maxConcurrent` set via daemon `--max-concurrent` flag
   - Downloads auto-queue when limit reached

3. **Client interface** (`pkg/warpcli/methods.go:63-87`)
   - `client.Download(url, fileName, downloadDirectory, opts)` returns `*common.DownloadResponse`
   - Non-blocking when `--background` flag set

### Dependencies

| Dependency | Purpose | Status |
|------------|---------|--------|
| Queue Manager (#142) | Concurrent limit enforcement | Merged |
| `urfave/cli` | CLI framework | Existing |
| `pkg/warpcli` | Daemon communication | Existing |

### Constraints

- Must maintain backward compatibility (single URL arg still works)
- Input file flag and direct URL can coexist (per issue spec)
- Background mode (`-b`) applies to all downloads in batch
- Per-file options (P1) require extended format parsing

## Feasibility Assessment

| Aspect | Assessment | Notes |
|--------|------------|-------|
| Technical Viability | High | Straightforward file parsing + loop |
| Effort Estimate | S | MVP in ~4-6 tasks |
| Risk Level | Low | No architectural changes needed |

## Input File Format Analysis

### Simple Format (P0 - MVP)
```
# Comment line
https://example.com/file1.zip

https://example.com/file2.zip
# Empty lines ignored
```

Rules:
- One URL per line
- Lines starting with `#` are comments
- Empty/whitespace-only lines skipped
- Trailing whitespace trimmed

### Extended Format (P1 - Future)
```
https://example.com/file1.zip
  out=custom-name.zip
  dir=/custom/path
```

Rules:
- Option lines indented with 2+ spaces
- Options apply to preceding URL
- Supported options: `out`, `dir`, `header`

## Recommendations

1. Implement simple format first (P0)
2. Add `-i, --input-file` flag to download command
3. Allow mix of `-i` and direct URL args
4. Use existing `--background` mode for non-blocking batch
5. Print summary: "N succeeded, M failed"
