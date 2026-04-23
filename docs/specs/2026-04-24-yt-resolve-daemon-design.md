# Daemon-side URL Resolver — Design

**Date:** 2026-04-24
**Status:** Draft

## Goal

Add a daemon RPC that resolves a video page URL into a list of downloadable format entries (direct URLs, per-quality). The WarpDL web extension calls this RPC instead of doing in-extension signature decoding. Mirrors IDM's extension-to-native-binary delegation model.

## Scope

### In scope

- New JSON-RPC method `resolve.url` on the existing WebSocket RPC (same endpoint the web extension already talks to)
- Shell-out to `yt-dlp --dump-single-json` to get resolved format info
- Parse yt-dlp's JSON into a typed result with itag, quality, URL, size, codecs
- Optional cookie source forwarding (so authenticated videos work)
- Timeout + size caps to bound daemon resource use

### Out of scope

- Bundling yt-dlp inside the daemon binary
- In-Go signature decoding (we rely on yt-dlp)
- Post-download merging of video+audio streams (yt-dlp does this client-side; the daemon ships raw URLs and lets the web-extension / CLI download separately)
- Caching previously-resolved URLs across process restarts (in-memory only)

## Why yt-dlp

- 1000+ extractors for YouTube, Vimeo, TikTok, Twitter, Twitch, Reddit, etc.
- Signature decoder is interpreted via `jsinterp.py`, robust to base.js changes
- Cookie + OAuth support built in
- Actively maintained weekly by 200+ contributors
- Installed on most Linux systems; easy pip install on others

The alternative (port yt-dlp's extractor framework to Go) is months of work and ongoing catch-up. Shelling out is 100 lines and always works as long as yt-dlp itself works.

## RPC contract

### Method: `resolve.url`

**Params** (`ResolveURLParams`):

```go
type ResolveURLParams struct {
    URL             string   `json:"url"`                       // page URL to resolve
    CookiesFrom     string   `json:"cookiesFrom,omitempty"`     // "firefox", "chrome", "edge", "brave", or ""
    CookiesFromFile string   `json:"cookiesFromFile,omitempty"` // path to cookies.txt
    Timeout         int      `json:"timeout,omitempty"`         // per-call timeout in seconds (default 30, max 120)
}
```

**Result** (`ResolveURLResult`):

```go
type ResolveURLResult struct {
    Title    string            `json:"title"`    // video title
    Author   string            `json:"author,omitempty"`
    Duration int               `json:"duration,omitempty"` // seconds
    Formats  []ResolvedFormat  `json:"formats"`
}

type ResolvedFormat struct {
    FormatID     string `json:"formatId"`    // yt-dlp format_id (e.g. "137", "251"); matches YouTube itag where applicable
    URL          string `json:"url"`         // direct, signed download URL
    Ext          string `json:"ext"`         // "mp4", "webm", "m4a"
    MimeType     string `json:"mimeType,omitempty"`
    Quality      string `json:"quality,omitempty"`     // "1080p", "720p", "medium", "best"
    FileSize     int64  `json:"fileSize,omitempty"`    // bytes; 0 if unknown
    HasVideo     bool   `json:"hasVideo"`
    HasAudio     bool   `json:"hasAudio"`
    VideoCodec   string `json:"videoCodec,omitempty"`
    AudioCodec   string `json:"audioCodec,omitempty"`
    Height       int    `json:"height,omitempty"`      // pixel height for video
    Width        int    `json:"width,omitempty"`
    Fps          int    `json:"fps,omitempty"`
    AudioBitrate int    `json:"audioBitrate,omitempty"` // kbps
}
```

**Errors** (JSON-RPC):

- `-32602 invalid params` — missing/malformed URL
- `-32001 resolver_unavailable` — yt-dlp binary not found on $PATH
- `-32002 resolver_timeout` — yt-dlp ran longer than timeout
- `-32003 resolver_failed` — yt-dlp exited non-zero (message includes stderr tail)
- `-32004 resolver_unsupported` — URL doesn't match any yt-dlp extractor

## Implementation

### File structure

```
common/const.go                           +UPDATE_RESOLVE_URL
common/types.go                           +ResolveURLParams, ResolveURLResult, ResolvedFormat
internal/server/rpc_methods.go            +"resolve.url" registration
internal/server/rpc_resolve.go   (new)    resolveURL handler + yt-dlp shell-out + JSON parsing
internal/server/rpc_resolve_test.go (new) unit tests (stub yt-dlp with sh -c echo)
```

We register on the JSON-RPC server (`internal/server/rpc_methods.go`), not the old Unix-socket `internal/api` — the web extension already talks to the JSON-RPC WebSocket, and that's the right place for new external RPCs.

### yt-dlp invocation

```
yt-dlp \
    --no-warnings \
    --skip-download \
    --dump-single-json \
    [--cookies-from-browser firefox] \
    [--cookies /path/to/cookies.txt] \
    <url>
```

- `--dump-single-json` prints one JSON object per video (vs `--dump-json` which can emit multiple for playlists)
- `--no-warnings` keeps stderr clean
- `--skip-download` skips actual file transfer (we only want metadata + URLs)

### Parsing yt-dlp output

yt-dlp's JSON has many fields; we care about:
- Top-level: `title`, `uploader`/`channel`, `duration`
- `formats[]` array, for each format:
  - `format_id` (string, e.g. "137")
  - `url` (decoded, ready-to-download)
  - `ext` ("mp4", "webm", "m4a")
  - `vcodec`, `acodec` ("none" if absent)
  - `filesize` or `filesize_approx`
  - `height`, `width`, `fps`
  - `abr` (audio bitrate, kbps)
  - `format_note` (e.g. "1080p60")

We filter out entries with `protocol == "m3u8"` or `"dash"` (HLS/DASH manifests, not directly downloadable as a single file). This is consistent with our web-extension scope that excludes streaming manifests.

### Resource limits

- Default timeout 30s, max 120s (configurable per-call via `timeout` param)
- Kill the process via `exec.CommandContext` on context cancel
- Limit stdout size to 32 MB (oversized output → error)
- No shell; direct `exec.Command`

### Binary discovery

```go
ytdlpPath := "yt-dlp"
if p, err := exec.LookPath(ytdlpPath); err == nil {
    ytdlpPath = p
}
// if LookPath fails at invocation time, return resolver_unavailable
```

Future: allow daemon config to specify a custom path (`YTDLP_BINARY` env var or config field).

## Testing

- Unit tests stub the `exec.LookPath` + `exec.CommandContext` pair via a hook variable (injectable for tests)
- Test cases:
  - Success: fixture yt-dlp JSON parsed into expected Result
  - Invalid URL format → invalid_params
  - Binary not found → resolver_unavailable
  - Non-zero exit → resolver_failed with stderr tail
  - Timeout → resolver_timeout
  - Playlist (multiple objects) → first video only
  - No formats → empty Formats array
- Coverage target: ≥80% (repo standard)

## Rollout

1. Add types to `common/`
2. Add handler with tests (unit, table-driven, stubbed exec)
3. Register in `methods()` map
4. Write a small CLI test client or curl-equivalent for smoke testing
5. Ship; web-extension integration follows in a separate branch

## Trade-offs & risks

| Risk | Mitigation |
|---|---|
| yt-dlp not installed on user's machine | Clear error code + user-facing message pointing to install docs |
| yt-dlp version too old; YouTube blocks with captcha | Daemon returns `resolver_failed` with stderr; user updates yt-dlp |
| yt-dlp behavior changes between versions | Parser tolerates unknown/missing fields; only consumes documented subset |
| Authenticated videos need cookies | Optional `cookiesFrom`/`cookiesFromFile` params; passes straight through |
| Multiple concurrent resolve calls explode memory | Each call has 32 MB stdout cap + timeout; processes are short-lived |
| yt-dlp binary lookup race / path injection | Use `exec.LookPath` result once per call; no shell interpolation |

## Alternatives considered

- **Go-native yt-dlp port (kkdai/youtube, lrstanley/go-ytdlp)**: Go libraries exist but lag yt-dlp proper by weeks/months on breaking changes. Shell-out stays current for free.
- **In-daemon Python with embedded yt-dlp library**: adds Python runtime dependency; larger install; worse isolation than subprocess.
- **WebSocket streaming of results**: not needed — resolution is a single-shot request/response in <10 s typical case.
