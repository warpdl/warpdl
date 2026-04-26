# Native YouTube Resolver + ffmpeg Mux — Design

**Date:** 2026-04-27
**Status:** Draft
**Supersedes:** `2026-04-24-yt-resolve-daemon-design.md`

## Goal

Replace the yt-dlp shell-out with the pure-Go `github.com/kkdai/youtube/v2` library and add a `youtube.download` RPC that handles both progressive (single-stream) and adaptive (separate video+audio + mux) YouTube downloads using ffmpeg. The web-extension UX matches IDM: the in-player overlay lists all available qualities; the user picks one; the daemon downloads (and muxes if needed) to disk.

## Why kkdai/youtube/v2

| Property | yt-dlp shell-out | kkdai/youtube/v2 |
|---|---|---|
| Runtime dep | yt-dlp (Python) | none — single Go binary |
| Sites | 1000+ | YouTube only (acceptable; YouTube is the dominant case) |
| Latency | ~1–2 s startup per call | ~200–500 ms per call |
| Ship-ability | requires user install | bundled into daemon |
| Maintenance | yt-dlp tracks YouTube | kkdai upstream tracks YouTube; we pin a known-good version |
| Mux | yt-dlp does it via ffmpeg | we drive ffmpeg directly |

We accept the YouTube-only narrowing. Generic `<video src>` detection in the extension still works for other sites (direct URL → `download.add`).

## Format taxonomy

YouTube serves three classes of streams:

- **Progressive** — itags `18` (360p), `22` (720p): video + audio in a single mp4. No mux.
- **Adaptive video-only** — itags `137` (1080p mp4), `248` (1080p webm), `271` (1440p), `313` (2160p), and so on. Codecs `avc1`, `vp9`, `av01`. Need mux.
- **Adaptive audio-only** — itags `140` (128 kbps m4a), `251` (160 kbps opus), `139` (48 kbps m4a). Either standalone or as the audio leg of a mux.

The web-extension overlay groups them as **Combined** / **Video only** / **Audio only**. For Video-only entries (1080p+), the extension pairs them with the best matching audio internally and displays "1080p (mux)" — the user clicks once and the daemon handles the rest.

## RPC contract

### `resolve.url` (replaces yt-dlp version)

**Params** unchanged:
```go
type ResolveURLParams struct {
    URL     string `json:"url"`
    Timeout int    `json:"timeout,omitempty"` // seconds, default 30, max 120
}
```
(`cookiesFrom` / `cookiesFromFile` — out of scope for v1; can be added later via kkdai's `HTTPClient` jar.)

**Result** (`ResolveURLResult`) — new field `videoId`:
```go
type ResolveURLResult struct {
    VideoID  string           `json:"videoId"`         // kkdai Video.ID — needed by youtube.download
    Title    string           `json:"title"`
    Author   string           `json:"author,omitempty"`
    Duration int              `json:"duration,omitempty"`
    Formats  []ResolvedFormat `json:"formats"`
}
```

`ResolvedFormat.url` is **empty** in the v1 response — the URL is decoded lazily by `youtube.download` to avoid N round-trips during resolve. The extension uses `formatId` (= itag) as the identifier.

**Error codes** — same as v0 except meanings updated:

| Code | Symbol | Cause |
|---|---|---|
| -32101 | `resolver_failed` | kkdai returned an error (private, region-locked, deleted, network, …) |
| -32102 | `resolver_timeout` | resolve took longer than `timeout` seconds |
| -32104 | `resolver_unsupported` | URL does not parse as a YouTube watch URL |
| -32602 | `invalid_params` | missing / malformed URL |

### `youtube.download` (new)

**Params**:
```go
type YouTubeDownloadParams struct {
    VideoID         string `json:"videoId"`          // required
    VideoFormatID   string `json:"videoFormatId"`    // required — itag of video stream (or progressive)
    AudioFormatID   string `json:"audioFormatId,omitempty"` // optional — itag of audio stream; if set, mux is performed
    Dir             string `json:"dir,omitempty"`    // optional output dir (defaults to daemon config)
    FileName        string `json:"fileName,omitempty"` // optional — extension auto-derived from container
    Connections     int32  `json:"connections,omitempty"` // optional, default 24
}
```

**Result**:
```go
type YouTubeDownloadResult struct {
    GID      string `json:"gid"`            // primary GID for status / progress tracking
    Muxed    bool   `json:"muxed"`          // true iff AudioFormatID was provided
    FileName string `json:"fileName"`       // final filename (post-mux for adaptive)
}
```

**Behavior**:

- **Progressive case** (`audioFormatId` empty):
  1. Resolve `Video` via kkdai by `VideoID`.
  2. Look up the video format by itag.
  3. Get the decoded URL via `GetStreamURLContext`.
  4. Hand off to `warplib.NewDownloader` + `manager.AddDownload` — the existing infrastructure already does parallel-segment downloads, progress notifications, retries.
  5. Return the warplib GID.

- **Adaptive case** (`audioFormatId` set):
  1. Resolve `Video`. Look up both formats by itag.
  2. Verify the video format is video-only and the audio format is audio-only (mismatch → `invalid_params`).
  3. Generate a synthetic parent GID (random hex 16). Allocate `<dir>/<base>.tmp/` with `video.<ext>` and `audio.<ext>`.
  4. Spawn a goroutine:
     - Download video stream via warplib.NewDownloader to `video.<ext>`.
     - Download audio stream via warplib.NewDownloader to `audio.<ext>` (in parallel with video).
     - Wait for both `download.complete` (or any `download.error`).
     - On error — broadcast `download.error` for the parent GID, cleanup tmp dir.
     - On success — exec `ffmpeg -y -i video -i audio -c copy -movflags +faststart final.<container>` into `<dir>/<base>.<container>`.
     - On ffmpeg success — broadcast `download.complete` for parent GID, cleanup tmp dir.
     - On ffmpeg failure — broadcast `download.error` for parent GID with stderr tail.
  5. Return parent GID immediately (handler does not block on the download).

**Error codes**:

| Code | Symbol | Cause |
|---|---|---|
| -32105 | `muxer_unavailable` | ffmpeg not on `$PATH` (only when `audioFormatId` is set) |
| -32106 | `format_not_found` | itag not present in video.Formats |
| -32107 | `format_mismatch` | video itag has audio, or audio itag has video |
| -32101 | `resolver_failed` | kkdai error fetching video |
| -32602 | `invalid_params` | missing videoId / videoFormatId |

### Progress notifications

Existing notifications work unchanged for progressive downloads. For adaptive:
- `download.started` for the parent GID, with the *combined* expected size (sum of video + audio content lengths).
- `download.progress` aggregated — the goroutine sums per-stream progress under the parent GID. (Implementation detail: we expose `Handlers` to per-leg downloads that funnel into a single counter.)
- `download.complete` once mux finishes.
- `download.error` for any failure; tmp dir is cleaned up.

## ffmpeg invocation

```
ffmpeg -y \
       -i <tmp>/video.<ext> \
       -i <tmp>/audio.<ext> \
       -c:v copy -c:a copy \
       -movflags +faststart \
       <dir>/<base>.<container>
```

- `-c copy` — no re-encode (fast, lossless container remux).
- `-movflags +faststart` — moves moov atom to front for streaming-friendly mp4.
- Output container: mp4 if video is `avc1`/`av01`, webm if video is `vp9` and audio is `opus`. Picked by sniffing video MIME.

Detection:
```go
ffmpegPath, err := exec.LookPath("ffmpeg")
if err != nil { return ErrMuxerUnavailable }
```
Re-checked at handler invocation, not startup, so users can install ffmpeg without restarting the daemon.

## Files

```
common/types.go                            UPDATE   YouTubeDownloadParams, YouTubeDownloadResult, ResolveURLResult.VideoID
internal/server/rpc_methods.go             UPDATE   register "youtube.download"
internal/server/rpc_resolve.go             REWRITE  kkdai-backed (was yt-dlp shell-out)
internal/server/rpc_resolve_test.go        REWRITE  mock kkdai HTTPClient
internal/server/rpc_youtube_download.go    NEW      orchestrator + mux pipeline
internal/server/rpc_youtube_download_test.go NEW    table-driven, mocked manager
internal/server/ffmpeg.go                  NEW      Discovery + Mux helper
internal/server/ffmpeg_test.go             NEW
go.mod                                     UPDATE   + github.com/kkdai/youtube/v2
docs/specs/2026-04-24-yt-resolve-daemon-design.md   ARCHIVE (this doc supersedes)
```

## Test plan

- Unit:
  - resolve.url: mock kkdai HTTP responses (video info JSON), assert format mapping.
  - format mapping: progressive itag → both flags true; video-only → hasVideo only; audio-only → hasAudio only; codec parse from MimeType.
  - youtube.download (progressive): manager.AddDownload called with decoded URL; returns warplib GID.
  - youtube.download (adaptive): both downloads kicked off, mux invoked, parent GID returned.
  - youtube.download (ffmpeg missing): -32105 muxer_unavailable.
  - youtube.download (format_not_found, format_mismatch): -32106 / -32107.
  - ffmpeg helper: command construction, exit-code mapping.
- Integration: optional in-process daemon + real YouTube URL (gated behind a build tag, not in CI).

## Trade-offs

| Risk | Mitigation |
|---|---|
| kkdai lags YouTube changes | pin tested version; bump on incidents |
| ffmpeg not installed on user machine | clear error code + UI message; progressive ≤720p still works |
| Tmp dir cleanup race on cancel | defer cleanup; remove on goroutine exit regardless of outcome |
| Adaptive download cannot be paused/resumed atomically | per-leg pause works; mux step is non-resumable but cheap |
| Mux container choice wrong | sniff MIME at the top; fall back to mkv if conflicting codecs |

## Out of scope (deferred)

- Cookie pass-through (private videos). Add later via `kkdai.Client.HTTPClient` with a cookie jar.
- DASH/HLS manifest extraction for live streams.
- Generic non-YouTube sites — generic detector remains as today (direct `<video>` URL → `download.add`).
- Re-encode (`-c:v libx264 ...`). For now, container remux only.
