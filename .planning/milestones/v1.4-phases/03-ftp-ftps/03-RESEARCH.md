# Phase 3: FTP/FTPS - Research

**Researched:** 2026-02-27
**Domain:** FTP/FTPS protocol client implementation in Go, single-stream download with resume and explicit TLS
**Confidence:** HIGH

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| FTP-01 | User can download files from `ftp://` URLs | `jlaffaye/ftp` Dial+Login+Retr pattern; plug into SchemeRouter for "ftp" scheme |
| FTP-02 | Anonymous FTP login is used by default when no credentials in URL | `url.Parse().User` is nil for anonymous; Login("anonymous", "anonymous") as fallback |
| FTP-03 | User can authenticate with username/password via URL (`ftp://user:pass@host/path`) | `url.Parse().User.Username()` + `.Password()` extraction; must strip from stored URL |
| FTP-04 | FTP uses passive mode (EPSV/PASV) by default | `jlaffaye/ftp` defaults to EPSV; `DialWithDisabledEPSV(false)` is default (EPSV on) |
| FTP-05 | FTP downloads are single-stream (no parallel segments) | `Capabilities()` returns `{SupportsParallel: false, SupportsResume: true}`; single `io.Copy` loop |
| FTP-06 | User can resume interrupted FTP downloads via REST/RetrFrom offset | `ServerConn.RetrFrom(path, offset uint64)` performs REST before RETR; persist offset in Item.Parts |
| FTP-07 | User can download from FTPS servers with explicit TLS | `DialWithExplicitTLS(*tls.Config)` option; same downloader handles ftp:// and ftps:// |
| FTP-08 | File size is reported before download starts for progress tracking | `ServerConn.FileSize(path) (int64, error)` via SIZE command in Probe phase |
</phase_requirements>

## Summary

Phase 3 implements a single `ftpProtocolDownloader` that plugs into the `ProtocolDownloader` interface and `SchemeRouter` built in Phase 2. The implementation uses `github.com/jlaffaye/ftp` v0.2.0, a well-established Go FTP client (1.4k stars, 634+ dependents, RFC 959 compliant). Both `ftp://` and `ftps://` schemes map to the same downloader struct — FTPS is just `DialWithExplicitTLS` vs plain `Dial`.

The critical design constraint is that `ServerConn` is **not goroutine-safe** and allows only one in-flight data connection at a time. This makes single-stream mandatory for protocol reasons, not just by choice. The `DownloadCapabilities` struct correctly reflects `{SupportsParallel: false}`. Resume works via `ServerConn.RetrFrom(path, offset)` which issues REST before RETR — the FTP-level equivalent of HTTP Range requests.

The critical security concern documented in STATE.md is that FTP credentials in URLs (`ftp://user:pass@host/path`) must NOT be persisted in `Item.Url`. The current `Item.Url` field is stored verbatim in GOB to `~/.config/warpdl/userdata.warp`. The fix is to extract credentials before storing: persist `ftp://host/path` in `Item.Url` and never store the `user:pass` portion.

**Primary recommendation:** Implement `ftpProtocolDownloader` in `pkg/warplib/protocol_ftp.go`, register it for both "ftp" and "ftps" schemes in `SchemeRouter`, add `AddFTPDownload` (or generalize `AddDownload`) in `Manager`, and strip credentials from the stored URL. The API layer (`internal/api/download.go`) needs a new code path for FTP URLs.

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/jlaffaye/ftp` | v0.2.0 | FTP/FTPS client — Dial, Login, Retr, RetrFrom, FileSize, Quit | Pre-decided in STATE.md; mature library, TLS support, RetrFrom for resume, 634+ dependents |
| `crypto/tls` | stdlib | TLS config for FTPS explicit TLS | Already used throughout codebase for HTTPS |
| `net/url` | stdlib | Parse FTP URL to extract host:port, path, user credentials | Already used in protocol_router.go, dloader.go |
| `io` | stdlib | `io.Copy` loop for single-stream data transfer with progress | Already used in dloader.go part runner |
| `context` | stdlib | Context cancellation for Stop() propagation | Already used throughout warplib |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `net/textproto` | stdlib | `textproto.Error` type for FTP error code classification | FTP protocol errors come as `textproto.Error{Code: 530}` — use for IsTransient |
| `path` | stdlib | Extract file name from FTP URL path (`path.Base()`) | FTP URLs have Unix-style paths; `path.Base("/pub/file.iso")` = "file.iso" |
| `strconv` | stdlib | Port number parsing from URL | `url.Port()` returns string; may need `strconv.Atoi` for default port handling |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `jlaffaye/ftp` | `secsy/goftp` | goftp has connection pooling but that's overkill for single-stream. jlaffaye is simpler, already decided in STATE.md. |
| `jlaffaye/ftp` | `dutchcoders/goftp` | Less maintained. |
| Explicit TLS via `DialWithExplicitTLS` | Separate ftpsProtocolDownloader struct | Single struct is cleaner — TLS is a dial-time option, not a protocol-level distinction. Already decided in CONTEXT.md: "ftp and ftps share the same FTPDownloader". |

**Installation:**
```bash
cd /Users/divkix/GitHub/warpdl/.claude/worktrees/issues-fix && go get github.com/jlaffaye/ftp@v0.2.0
```

## Architecture Patterns

### Recommended Project Structure

New files in `pkg/warplib/`:

```
pkg/warplib/
├── protocol_ftp.go          # ftpProtocolDownloader struct + factory function
├── protocol_ftp_test.go     # Unit tests with mock FTP server
└── (no other new files)
```

Changes to existing files:
- `protocol_router.go` — Register "ftp" and "ftps" factories in `NewSchemeRouter` (or add `NewSchemeRouterWithFTP`)
- `manager.go` — Add `AddFTPDownload` method (or generalize `AddDownload` to accept `ProtocolDownloader`)
- `internal/api/download.go` — Detect FTP/FTPS URL scheme, call FTP code path

### Pattern 1: ftpProtocolDownloader Struct

**What:** Implements `ProtocolDownloader` for both `ftp://` and `ftps://`. Holds connection state, parsed URL fields, and a `*ftp.ServerConn` created during `Probe` or `Download`.

**When to use:** Created by `ftpDownloaderFactory` for any URL with scheme "ftp" or "ftps".

**Example:**
```go
// Source: jlaffaye/ftp v0.2.0 API + ProtocolDownloader interface from Phase 2
// Compile-time interface check
var _ ProtocolDownloader = (*ftpProtocolDownloader)(nil)

type ftpProtocolDownloader struct {
    rawURL   string
    opts     *DownloaderOpts
    // parsed URL fields (set in constructor)
    host     string    // host:port, default port 21
    path     string    // file path on server
    user     string    // extracted from URL userinfo (not persisted)
    password string    // extracted from URL userinfo (not persisted)
    useTLS   bool      // true for ftps://
    // set during Probe
    conn     *ftp.ServerConn
    fileSize int64
    fileName string
    hash     string
    probed   bool
    // stop control
    stopped  int32     // atomic
    // download location
    dlDir    string
    savePath string
}

func newFTPProtocolDownloader(rawURL string, opts *DownloaderOpts) (ProtocolDownloader, error) {
    u, err := url.Parse(rawURL)
    if err != nil {
        return nil, fmt.Errorf("ftp: parse url: %w", err)
    }
    d := &ftpProtocolDownloader{
        rawURL:  rawURL,
        opts:    opts,
        path:    u.Path,
        useTLS:  strings.ToLower(u.Scheme) == "ftps",
    }
    // Extract host:port (default port 21 if not specified)
    host := u.Hostname()
    port := u.Port()
    if port == "" {
        port = "21"
    }
    d.host = net.JoinHostPort(host, port)
    // Extract credentials from URL userinfo — NOT stored to disk
    if u.User != nil {
        d.user = u.User.Username()
        d.password, _ = u.User.Password()
    }
    // Default anonymous if no credentials
    if d.user == "" {
        d.user = "anonymous"
        d.password = "anonymous"
    }
    // Derive file name from path
    d.fileName = path.Base(u.Path)
    if d.fileName == "." || d.fileName == "/" || d.fileName == "" {
        return nil, fmt.Errorf("ftp: cannot determine filename from URL path %q", u.Path)
    }
    // Set download directory
    if opts != nil && opts.DownloadDirectory != "" {
        d.dlDir = opts.DownloadDirectory
    } else {
        d.dlDir = DefaultDownloadDir // same default as HTTP downloader
    }
    return d, nil
}
```

### Pattern 2: Probe Method — Connect, Authenticate, FileSize, Disconnect

**What:** Opens a connection, authenticates, calls `FileSize`, then **closes the connection**. The download connection is opened fresh in `Download`/`Resume`. This is the cleanest approach because jlaffaye/ftp connections are not goroutine-safe and holding a connection open between Probe and Download introduces timeout risk.

**When to use:** Called by Manager before `AddFTPDownload` to populate `ProbeResult`.

**Example:**
```go
// Source: jlaffaye/ftp v0.2.0 Dial/Login/FileSize/Quit pattern
func (d *ftpProtocolDownloader) Probe(ctx context.Context) (ProbeResult, error) {
    conn, err := d.connect(ctx)
    if err != nil {
        return ProbeResult{}, NewPermanentError("ftp", "connect", err)
    }
    defer conn.Quit()

    size, err := conn.FileSize(d.path)
    if err != nil {
        // SIZE command failure — try to continue without size? No: FTP-08 requires size.
        return ProbeResult{}, NewPermanentError("ftp", "probe:size", err)
    }
    d.fileSize = size
    d.probed = true

    return ProbeResult{
        FileName:      d.fileName,
        ContentLength: size,
        Resumable:     true, // FTP always supports resume via RetrFrom
        Checksums:     nil,  // FTP has no checksum headers
    }, nil
}

func (d *ftpProtocolDownloader) connect(ctx context.Context) (*ftp.ServerConn, error) {
    opts := []ftp.DialOption{
        ftp.DialWithContext(ctx),
        ftp.DialWithTimeout(30 * time.Second),
    }
    if d.useTLS {
        opts = append(opts, ftp.DialWithExplicitTLS(&tls.Config{
            ServerName: d.host[:strings.LastIndex(d.host, ":")], // hostname only
        }))
    }
    conn, err := ftp.Dial(d.host, opts...)
    if err != nil {
        return nil, err
    }
    if err := conn.Login(d.user, d.password); err != nil {
        conn.Quit()
        return nil, err
    }
    return conn, nil
}
```

**Key insight:** Connect-Probe-Disconnect-Reconnect-Download is standard for FTP. Holding open is risky (server idle timeout = 300s typical). The two-dial approach is correct.

### Pattern 3: Download Method — Single-Stream io.Copy with Progress

**What:** Opens connection, sets binary mode, calls `Retr` or `RetrFrom`, streams via `io.Copy` with a progress-tracking reader wrapper, calls completion handler.

**When to use:** Fresh download (no existing parts). Resume uses `RetrFrom`.

**Example:**
```go
// Source: jlaffaye/ftp Retr + io.Copy pattern with progress reader
func (d *ftpProtocolDownloader) Download(ctx context.Context, handlers *Handlers) error {
    if !d.probed {
        return ErrProbeRequired
    }
    conn, err := d.connect(ctx)
    if err != nil {
        return NewTransientError("ftp", "connect", err)
    }
    defer conn.Quit()

    // Set binary transfer mode (FTP-05: single stream, correct mode)
    if err := conn.Type(ftp.TransferTypeBinary); err != nil {
        return NewPermanentError("ftp", "type:binary", err)
    }

    // Open destination file
    f, err := os.OpenFile(d.savePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
    if err != nil {
        return NewPermanentError("ftp", "open:dest", err)
    }
    defer f.Close()

    resp, err := conn.Retr(d.path)
    if err != nil {
        return classifyFTPError("ftp", "retr", err)
    }
    defer resp.Close()

    // Progress-tracking copy
    hash := d.hash
    handlers.SpawnPartHandler(hash, 0, d.fileSize-1)
    _, err = io.Copy(io.MultiWriter(f, &ftpProgressWriter{
        handlers: handlers,
        hash:     hash,
    }), resp)
    if err != nil {
        return classifyFTPError("ftp", "copy", err)
    }

    handlers.DownloadCompleteHandler(MAIN_HASH, d.fileSize)
    return nil
}

// ftpProgressWriter calls DownloadProgressHandler on each Write
type ftpProgressWriter struct {
    handlers *Handlers
    hash     string
}

func (w *ftpProgressWriter) Write(p []byte) (int, error) {
    n := len(p)
    w.handlers.DownloadProgressHandler(w.hash, n)
    return n, nil
}
```

### Pattern 4: Resume Method — RetrFrom Offset

**What:** For FTP resume, `parts` will contain a single entry (FTP is single-stream). Extract the start offset from the uncompiled part, call `RetrFrom(path, offset)`, stream from there.

**When to use:** Called by Manager's `ResumeDownload` path when `item.Protocol == ProtoFTP`.

**Example:**
```go
// Source: jlaffaye/ftp RetrFrom + offset calculation
func (d *ftpProtocolDownloader) Resume(ctx context.Context, parts map[int64]*ItemPart, handlers *Handlers) error {
    if !d.probed {
        return ErrProbeRequired
    }
    // FTP is single-stream: find the single uncompiled part
    var startOffset int64
    for ioff, part := range parts {
        if !part.Compiled {
            startOffset = ioff
            break
        }
    }

    conn, err := d.connect(ctx)
    if err != nil {
        return NewTransientError("ftp", "connect", err)
    }
    defer conn.Quit()

    if err := conn.Type(ftp.TransferTypeBinary); err != nil {
        return NewPermanentError("ftp", "type:binary", err)
    }

    // Open file for append at offset
    f, err := os.OpenFile(d.savePath, os.O_WRONLY, 0644)
    if err != nil {
        return NewPermanentError("ftp", "open:dest", err)
    }
    defer f.Close()
    if _, err := f.Seek(startOffset, io.SeekStart); err != nil {
        return NewPermanentError("ftp", "seek", err)
    }

    // RetrFrom issues REST <offset> before RETR
    resp, err := conn.RetrFrom(d.path, uint64(startOffset))
    if err != nil {
        return classifyFTPError("ftp", "retrfrom", err)
    }
    defer resp.Close()

    hash := d.hash
    _, err = io.Copy(io.MultiWriter(f, &ftpProgressWriter{
        handlers: handlers,
        hash:     hash,
    }), resp)
    if err != nil {
        return classifyFTPError("ftp", "copy", err)
    }

    handlers.DownloadCompleteHandler(MAIN_HASH, d.fileSize)
    return nil
}
```

### Pattern 5: FTP Error Classification (textproto.Error)

**What:** FTP protocol errors from `jlaffaye/ftp` are `*textproto.Error` with a numeric `Code` field. RFC 959 defines:
- **4xx codes**: Transient — may be retried (421 Service Not Available, 425 Can't Open Data Connection, 426 Transfer Aborted)
- **5xx codes**: Permanent — no retry (530 Not Logged In/Auth Failure, 550 File Not Available, 553 File Name Not Allowed)

**When to use:** Wrap all `jlaffaye/ftp` errors in `classifyFTPError` before returning from Probe/Download/Resume.

**Example:**
```go
// Source: RFC 959 section 4.2, net/textproto package (stdlib)
func classifyFTPError(proto, op string, err error) *DownloadError {
    if err == nil {
        return nil
    }
    // Check for textproto.Error (FTP protocol error codes)
    var tpErr *textproto.Error
    if errors.As(err, &tpErr) {
        // 4xx = transient, 5xx = permanent (RFC 959 §4.2)
        isTransient := tpErr.Code >= 400 && tpErr.Code < 500
        if isTransient {
            return NewTransientError(proto, op, err)
        }
        return NewPermanentError(proto, op, err)
    }
    // Network errors (connection refused, timeout) = transient
    var netErr net.Error
    if errors.As(err, &netErr) {
        return NewTransientError(proto, op, err)
    }
    // Default: transient (prefer retry over giving up on unexpected errors)
    return NewTransientError(proto, op, err)
}
```

### Pattern 6: Credential Stripping from Stored URL — SECURITY CRITICAL

**What:** `Item.Url` is GOB-persisted to `~/.config/warpdl/userdata.warp`. If `ftp://user:pass@host/file` is stored verbatim, credentials leak to disk. Strip credentials before storage.

**When to use:** In `AddFTPDownload` (or wherever the Item.Url is set for FTP downloads).

**Example:**
```go
// Source: net/url stdlib — Redacted() was added in Go 1.21
// Use url.Parse().Redacted() to get the URL with password replaced by "xxxxx"
// OR construct the URL without userinfo:

func stripFTPCredentials(rawURL string) (string, error) {
    u, err := url.Parse(rawURL)
    if err != nil {
        return "", err
    }
    // Remove userinfo (user:pass) before storing — never persist credentials
    u.User = nil
    return u.String(), nil
}

// In AddFTPDownload:
storedURL := stripFTPCredentials(rawURL) // "ftp://host/path" — no credentials
item.Url = storedURL
// d.user, d.password are held in ftpProtocolDownloader memory only
// They are NOT stored in item.Url
```

**WARNING:** `url.URL.Redacted()` replaces the password with "xxxxx" — NOT what we want. We want user:pass REMOVED entirely. Use `u.User = nil` then `u.String()`.

### Pattern 7: SchemeRouter Registration

**What:** Register "ftp" and "ftps" factories in `NewSchemeRouter`. Per the Phase 2 decision, `ftp` and `ftps` share the same factory.

**When to use:** At startup, when SchemeRouter is constructed.

**Example:**
```go
// Source: protocol_router.go NewSchemeRouter — add ftp/ftps alongside http/https
func NewSchemeRouter(client *http.Client) *SchemeRouter {
    // ... existing http/https registration ...
    ftpFactory := func(rawURL string, opts *DownloaderOpts) (ProtocolDownloader, error) {
        return newFTPProtocolDownloader(rawURL, opts)
    }
    r.routes["ftp"] = ftpFactory
    r.routes["ftps"] = ftpFactory
    return r
}
```

### Pattern 8: Manager.AddFTPDownload — New API Needed

**What:** `Manager.AddDownload(d *Downloader, opts)` is HTTP-specific — it takes a `*Downloader` directly. FTP needs a parallel method that accepts a `ProtocolDownloader` already initialized.

The Phase 2 summary (02-01) noted: "Phase 3 will add a `AddProtocolDownload` or generalize when FTP needs it."

**Recommendation:** Add `AddProtocolDownload(pd ProtocolDownloader, probeResult ProbeResult, proto Protocol, opts *AddDownloadOpts)`. This avoids touching the existing `AddDownload` signature (no HTTP regression risk).

```go
// New method in manager.go for non-HTTP protocol downloaders
func (m *Manager) AddProtocolDownload(pd ProtocolDownloader, probe ProbeResult, proto Protocol, opts *AddDownloadOpts) (string, error) {
    if opts == nil {
        opts = &AddDownloadOpts{}
    }
    hash := pd.GetHash() // must be set by factory
    item, err := newItem(
        m.mu,
        probe.FileName,
        pd.GetSavePath(),   // credential-stripped URL must be set inside pd
        pd.GetDownloadDirectory(),
        hash,
        ContentLength(probe.ContentLength),
        probe.Resumable,
        &itemOpts{...},
    )
    item.Protocol = proto  // ProtoFTP or ProtoFTPS
    item.Url = pd.GetCleanURL() // URL without credentials — need new interface method OR pass as param
    item.setDAlloc(pd)
    // NOTE: patchHandlers needs refactor — it currently takes *Downloader directly
    // For Phase 3: FTP handlers are passed into Download/Resume directly, no patchHandlers needed
    m.UpdateItem(item)
    return hash, nil
}
```

**Alternative:** Pass `cleanURL` (credential-stripped) and `rawURL` (with credentials) separately to the factory, with the factory storing only `cleanURL` as the retrievable URL via `GetCleanURL()`. This keeps credential stripping inside the FTP factory where it belongs.

### Pattern 9: Handler Integration for FTP (No patchHandlers)

**What:** `patchHandlers` in `manager.go` directly accesses `d.handlers` on a concrete `*Downloader`. FTP doesn't use `*Downloader`, so `patchHandlers` cannot be reused. For Phase 3, the handlers are pre-built by `internal/api/download.go` and passed into `Download(ctx, handlers)` directly.

The wrapping that `patchHandlers` does (updating `item.Downloaded`, marking parts compiled, notifying queue on complete) must happen either:
1. Inside the FTP downloader itself (tight coupling — bad)
2. In a new `patchFTPHandlers(pd *ftpProtocolDownloader, item *Item) *Handlers` that builds a patched `*Handlers` the caller passes into `pd.Download(ctx, patchedHandlers)` — correct approach

**Recommendation:** Create `patchProtocolHandlers(item *Item, handlers *Handlers) *Handlers` that is protocol-agnostic and returns a new `*Handlers` with wrapped callbacks. Call this from `AddProtocolDownload` and the API layer.

### Anti-Patterns to Avoid

- **Reusing `patchHandlers(d *Downloader, item *Item)`:** It accesses `d.handlers` directly (unexported field on `*Downloader`). Cannot be used with FTP. Don't add workarounds — create a clean handler-patching function that takes `*Handlers` and `*Item` instead.
- **Holding FTP connection open between Probe and Download:** Server idle timeout (typically 60-300s). Open fresh in each method.
- **Using active mode (PORT command):** Disabled by design (per REQUIREMENTS.md Out of Scope). Passive mode (EPSV/PASV) only. `jlaffaye/ftp` defaults to EPSV — do not override.
- **Storing `ftp://user:pass@host/path` in `Item.Url`:** Credentials persist to disk. Strip before storage. This is a security requirement from STATE.md.
- **Parallel segments for FTP:** Protocol limitation. `ServerConn` allows only one in-flight data connection. `DownloadCapabilities.SupportsParallel = false` must be set and enforced.
- **Using `ftp.DialWithTLS` for FTPS explicit:** `DialWithTLS` is for **implicit** TLS (port 990). `DialWithExplicitTLS` is for **explicit** TLS (STARTTLS on port 21). FTP-07 requires explicit TLS. These are different; mixing them causes connection failures.
- **Ignoring `conn.Type(ftp.TransferTypeBinary)`:** Default mode may be ASCII which corrupts binary files. Always set binary mode before `Retr`/`RetrFrom`.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| FTP protocol state machine | Custom RETR/REST implementation | `jlaffaye/ftp` ServerConn | RFC 959 compliance, passive mode negotiation, textproto handling |
| FTPS explicit TLS upgrade | Custom STARTTLS/AUTH TLS sequence | `ftp.DialWithExplicitTLS` | TLS handshake + PBSZ/PROT command sequence is error-prone to implement manually |
| FTP passive mode negotiation | Custom EPSV/PASV parsing | Built into `jlaffaye/ftp` | EPSV vs PASV fallback, NAT traversal, IPv4/IPv6 |
| Progress tracking | Custom byte counter | `ftpProgressWriter` (4 lines) wrapping `io.Copy` | Standard io.Writer wrapper pattern already used in dloader.go |
| URL credential extraction | Custom string parsing | `url.Parse().User.Username()` + `.Password()` | net/url handles percent-encoding, edge cases |
| FTP error classification | String matching on error messages | `errors.As(err, &textproto.Error)` + code ranges | Protocol-level codes are authoritative; string matching is fragile |

**Key insight:** `jlaffaye/ftp` handles the entire FTP protocol layer. The FTP downloader adapter is thin — it connects, authenticates, calls one or two library methods, and streams bytes. Keep it under 300 LOC.

## Common Pitfalls

### Pitfall 1: Credentials Persisted to Disk

**What goes wrong:** `Item.Url` is GOB-encoded to `~/.config/warpdl/userdata.warp`. If the raw FTP URL with embedded `user:pass` is stored, credentials persist to disk in plaintext.

**Why it happens:** The current HTTP flow stores `d.url` (the raw URL) directly. HTTP URLs rarely contain credentials. FTP URLs routinely do.

**How to avoid:** Strip credentials in the factory before setting any persistent state. Store `ftp://host/path` in `Item.Url`, keep `user` and `password` only in `ftpProtocolDownloader` memory. Add a test that verifies `item.Url` does not contain `@` or password-like content after `AddProtocolDownload`.

**Warning signs:** Any `Item.Url` containing `@` in the FTP case is a bug.

### Pitfall 2: Wrong TLS Option for FTPS

**What goes wrong:** `DialWithTLS` (implicit, port 990) used instead of `DialWithExplicitTLS` (explicit/STARTTLS, port 21). Most public FTPS servers use explicit TLS on port 21. Using implicit TLS against an explicit server hangs or gets rejected.

**Why it happens:** Confusing "implicit" and "explicit" TLS — both options exist in `jlaffaye/ftp` and look similar.

**How to avoid:** For `ftps://` scheme, always use `DialWithExplicitTLS`. Document this choice in code comments. Test with a test server that requires explicit TLS.

**Warning signs:** FTPS connections that hang on Dial or fail with unexpected connection reset.

### Pitfall 3: ASCII Mode Corrupting Binary Files

**What goes wrong:** FTP defaults to ASCII transfer mode on some servers. In ASCII mode, line endings are translated (`\n` → `\r\n`). Binary files (ISO, ZIP, tar.gz) become corrupted.

**Why it happens:** FTP was designed for text files and ASCII is the RFC 959 default transfer type.

**How to avoid:** Always call `conn.Type(ftp.TransferTypeBinary)` immediately after Login and before any Retr/RetrFrom call. Add a test that downloads a binary file and verifies its hash.

**Warning signs:** Downloaded files have wrong checksums or sizes.

### Pitfall 4: FTP ServerConn Held Open Between Probe and Download

**What goes wrong:** If `Probe` creates a `*ftp.ServerConn` and stores it for `Download` to reuse, the connection may timeout during the interval between user confirming download and actual transfer start. Server-side idle timeout is typically 60-300 seconds.

**Why it happens:** Attempting to reuse the connection for performance.

**How to avoid:** Open a new connection in each of `Probe`, `Download`, and `Resume`. The cost is one extra TCP+TLS handshake. This is unavoidable for correctness.

**Warning signs:** "421 Service not available" or "530 Please login" errors when Download is called after Probe.

### Pitfall 5: RetrFrom Offset Is uint64, Not int64

**What goes wrong:** `ServerConn.RetrFrom(path string, offset uint64)` takes `uint64`. Internal offsets in `Item.Parts` use `int64`. Implicit conversion of a negative `int64` to `uint64` would wrap around to a huge number.

**Why it happens:** Type mismatch between Go's FTP library API (uint64) and warplib's internal offset type (int64).

**How to avoid:** Guard: if `startOffset < 0`, return an error. Use `uint64(startOffset)` after the guard check.

**Warning signs:** Resume starts from a random huge offset instead of the correct byte position.

### Pitfall 6: Empty Path or Root Path in FTP URL

**What goes wrong:** `ftp://host/` or `ftp://host` has `path = "/"` or `path = ""`. `path.Base("/")` returns `"/"`, not a usable filename.

**Why it happens:** User provides a URL without a specific file path (e.g., a directory listing URL).

**How to avoid:** In the factory constructor, check `path.Base(u.Path)` and reject paths that resolve to `"."`, `"/"`, or `""` with a clear error: `"ftp URL must point to a specific file, not a directory"`.

### Pitfall 7: patchHandlers Cannot Be Reused for FTP

**What goes wrong:** `patchHandlers(d *Downloader, item *Item)` takes a concrete `*Downloader` and mutates `d.handlers` directly. FTP doesn't use `*Downloader`, so patchHandlers cannot wrap FTP progress events.

**Why it happens:** patchHandlers is coupled to the concrete HTTP downloader implementation.

**How to avoid:** Create `buildPatchedHandlers(item *Item, m *Manager, base *Handlers) *Handlers` — a pure function that returns a new `*Handlers` with item-update wrapping. Call it for both HTTP (via AddDownload) and FTP (via AddProtocolDownload). The existing `patchHandlers` can be refactored to use this internally without breaking HTTP behavior.

### Pitfall 8: MAIN_HASH Assumption in DownloadCompleteHandler

**What goes wrong:** `patchHandlers` in `manager.go` fires `DownloadCompleteHandler` only when `hash == MAIN_HASH`. FTP uses a single hash for the entire file. If FTP does not use `MAIN_HASH` as the identifier, the completion handler never fires, leaving the item stuck as "downloading".

**Why it happens:** The MAIN_HASH mechanism is HTTP-specific, used to signal the aggregated download is complete (vs individual segments).

**How to avoid:** FTP downloader must call `handlers.DownloadCompleteHandler(MAIN_HASH, totalBytesRead)` — using `MAIN_HASH` as the hash value, not any FTP-specific hash. This is the signal the manager watches. Document this clearly in the FTP downloader.

## Code Examples

Verified patterns from official sources:

### jlaffaye/ftp Dial + Login + FileSize + Retr (from pkg.go.dev)

```go
// Source: https://pkg.go.dev/github.com/jlaffaye/ftp v0.2.0
// Basic connection and download
c, err := ftp.Dial("ftp.example.org:21",
    ftp.DialWithTimeout(5*time.Second),
    ftp.DialWithContext(ctx))
if err != nil {
    return err
}
defer c.Quit()

if err = c.Login("user", "password"); err != nil {
    return err
}

// Get file size before transfer (FTP-08)
size, err := c.FileSize("/path/to/file.iso")
if err != nil {
    return err  // SIZE command not supported or file not found
}

// Set binary transfer mode (prevents corruption)
if err := c.Type(ftp.TransferTypeBinary); err != nil {
    return err
}

// Download fresh (from beginning)
r, err := c.Retr("/path/to/file.iso")
if err != nil {
    return err
}
defer r.Close()
_, err = io.Copy(dest, r)
```

### jlaffaye/ftp RetrFrom for Resume (from pkg.go.dev)

```go
// Source: https://pkg.go.dev/github.com/jlaffaye/ftp v0.2.0 — RetrFrom
// Note: offset is uint64, not int64 — guard against negative startOffset first
if startOffset < 0 {
    return fmt.Errorf("invalid resume offset: %d", startOffset)
}
r, err := c.RetrFrom("/path/to/file.iso", uint64(startOffset))
if err != nil {
    return err
}
defer r.Close()

// Seek destination file to resume point
if _, err := destFile.Seek(startOffset, io.SeekStart); err != nil {
    return err
}
_, err = io.Copy(destFile, r)
```

### FTPS Explicit TLS (from pkg.go.dev)

```go
// Source: https://pkg.go.dev/github.com/jlaffaye/ftp v0.2.0 — DialWithExplicitTLS
// Explicit TLS (STARTTLS on port 21) — used for ftps:// scheme
c, err := ftp.Dial("ftp.example.org:21",
    ftp.DialWithExplicitTLS(&tls.Config{
        ServerName: "ftp.example.org",  // must match certificate
    }))
// NOT DialWithTLS (that's implicit TLS on port 990)
```

### textproto.Error FTP Code Classification (stdlib)

```go
// Source: Go stdlib net/textproto — Error struct
// FTP protocol errors come as *textproto.Error
var tpErr *textproto.Error
if errors.As(err, &tpErr) {
    // RFC 959: 4xx = transient, 5xx = permanent
    // 421 = service unavailable (transient), 530 = auth failure (permanent)
    isTransient := tpErr.Code >= 400 && tpErr.Code < 500
    return isTransient
}
```

### Credential Stripping (stdlib)

```go
// Source: Go stdlib net/url
// Strip credentials from URL before storing in Item.Url
func stripURLCredentials(rawURL string) (string, error) {
    u, err := url.Parse(rawURL)
    if err != nil {
        return "", err
    }
    u.User = nil  // Remove userinfo entirely — u.Redacted() keeps it with "xxxxx"
    return u.String(), nil
}
// "ftp://user:pass@host:21/path/file.iso" → "ftp://host:21/path/file.iso"
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| HTTP-only `*Downloader` hard-coded | `ProtocolDownloader` interface + SchemeRouter | Phase 2 | FTP adapter just needs to implement the interface |
| `Item.dAlloc` as `*Downloader` | `Item.dAlloc` as `ProtocolDownloader` | Phase 2 | FTP downloader plugs in directly |
| `AddDownload(d *Downloader)` only | Phase 3 adds `AddProtocolDownload(pd ProtocolDownloader, ...)` | Phase 3 | Manager supports both HTTP and FTP paths |
| No protocol routing | `SchemeRouter.Register("ftp", factory)` | Phase 3 | URL scheme dispatches to FTP |

**Deprecated/outdated:**
- `jlaffaye/ftp v0.1.x`: v0.2.0 is current. v0.2.0 adds `DialWithExplicitTLS` and context support. Use v0.2.0.
- `DialTimeout()`: Deprecated in jlaffaye/ftp — use `Dial` with `DialWithTimeout` option instead.
- `Connect()`: Deprecated alias for `Dial` in jlaffaye/ftp.

## Open Questions

1. **patchHandlers refactor scope**
   - What we know: `patchHandlers(d *Downloader, item *Item)` is used in both `AddDownload` and `ResumeDownload` for HTTP. FTP cannot use it.
   - What's unclear: Whether to refactor `patchHandlers` into a protocol-agnostic `buildPatchedHandlers` or create a separate FTP-specific version.
   - Recommendation: Create `buildPatchedHandlers(item *Item, m *Manager, base *Handlers) *Handlers` as a pure function. Both HTTP and FTP call it. Existing `patchHandlers` becomes a thin wrapper calling this + setting `d.handlers`. Zero regression on HTTP.

2. **Manager.AddFTPDownload vs AddProtocolDownload**
   - What we know: `AddDownload(d *Downloader)` is HTTP-specific and is called from `internal/api/download.go`. The Phase 2 summary noted FTP will need a new method or generalization.
   - What's unclear: Whether to add `AddProtocolDownload(ProtocolDownloader, ProbeResult, Protocol, *AddDownloadOpts)` or refactor `AddDownload` to accept `ProtocolDownloader` directly.
   - Recommendation: Add `AddProtocolDownload` as a new method. `AddDownload` stays untouched (no HTTP regression). Both HTTP and FTP addpaths converge at item creation + `setDAlloc`.

3. **FTP hash generation**
   - What we know: HTTP downloader generates a hash via `crypto/rand` in `NewDownloader`. FTP downloader must also have a unique hash for the item.
   - What's unclear: Should the hash be generated inside `newFTPProtocolDownloader` or passed in by the caller?
   - Recommendation: Generate the hash inside `newFTPProtocolDownloader` using the same `crypto/rand` hex approach as the HTTP downloader. The hash is a download session identifier — the FTP URL (with credentials stripped) is not a reliable hash source.

4. **FTPS TLS certificate verification**
   - What we know: `DialWithExplicitTLS(&tls.Config{})` uses Go's default TLS verification (hostname + cert chain).
   - What's unclear: Should an `--insecure` flag bypass TLS verification for self-signed certs? Phase 3 scope doesn't mention this.
   - Recommendation: Use default TLS verification (no `InsecureSkipVerify`). If users need self-signed cert support, that's a Phase 3+ enhancement. Keep it out of scope for now.

5. **FTP test server approach**
   - What we know: `jlaffaye/ftp` uses an internal mock FTP server in its own tests. `fclairamb/ftpserverlib` provides a full in-memory FTP server (backed by afero). Both require additional dependencies.
   - What's unclear: Which approach to use for Phase 3 tests. Using an in-process mock server (via `net.Listener` + FTP protocol simulation) avoids new dependencies but is high-effort. Using `fclairamb/ftpserverlib` adds a test dependency.
   - Recommendation: Use `fclairamb/ftpserverlib` with `afero.NewMemMapFs()` as a test-only dependency. It provides a realistic FTP server without network or disk I/O. Confine it to `_test.go` files so it's not part of the main binary.

## Validation Architecture

Note: `workflow.nyquist_validation` is not present in `.planning/config.json` — this section is included based on project testing requirements (80% coverage minimum, TDD+Trophy strict red-green-refactor).

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` |
| Config file | none (uses `go test`) |
| Quick run command | `go test -run TestFTP ./pkg/warplib/` |
| Full suite command | `go test -race -short ./pkg/warplib/` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| FTP-01 | `ftpProtocolDownloader` factory creates downloader for `ftp://host/file.iso` URL | unit | `go test -run TestFTPFactory ./pkg/warplib/` | ❌ Wave 0 |
| FTP-01 | Scheme router dispatches `ftp://` to FTP factory, `https://` still dispatches to HTTP | unit | `go test -run TestFTPSchemeRouting ./pkg/warplib/` | ❌ Wave 0 |
| FTP-01 | Full download completes against mock FTP server — file written to disk | integration | `go test -run TestFTPDownloadIntegration ./pkg/warplib/` | ❌ Wave 0 |
| FTP-02 | No credentials in URL → Login("anonymous", "anonymous") called on mock server | unit | `go test -run TestFTPAnonymousLogin ./pkg/warplib/` | ❌ Wave 0 |
| FTP-03 | `ftp://user:pass@host/file` extracts user/password and calls Login with them | unit | `go test -run TestFTPCredentialAuth ./pkg/warplib/` | ❌ Wave 0 |
| FTP-03 | After `AddProtocolDownload`, `item.Url` does not contain `@` or password | unit | `go test -run TestFTPCredentialNotPersisted ./pkg/warplib/` | ❌ Wave 0 |
| FTP-04 | `Capabilities()` returns `{SupportsParallel: false, SupportsResume: true}` | unit | `go test -run TestFTPCapabilities ./pkg/warplib/` | ❌ Wave 0 |
| FTP-05 | Download uses single stream (single part entry in Item.Parts) | unit | `go test -run TestFTPSingleStream ./pkg/warplib/` | ❌ Wave 0 |
| FTP-06 | Resume calls `RetrFrom(path, uint64(offset))` with correct byte offset | unit | `go test -run TestFTPResume ./pkg/warplib/` | ❌ Wave 0 |
| FTP-07 | `ftps://` URL uses `DialWithExplicitTLS` (not `DialWithTLS`) | unit | `go test -run TestFTPSExplicitTLS ./pkg/warplib/` | ❌ Wave 0 |
| FTP-08 | `Probe` returns `ContentLength == server file size` from SIZE command | unit | `go test -run TestFTPProbeFileSize ./pkg/warplib/` | ❌ Wave 0 |
| FTP-08 | `DownloadProgressHandler` called during transfer; total matches expected size | integration | `go test -run TestFTPProgressTracking ./pkg/warplib/` | ❌ Wave 0 |

### Sampling Rate

- **Per task commit:** `go test -run TestFTP ./pkg/warplib/`
- **Per wave merge:** `go test -race -short ./pkg/warplib/`
- **Phase gate:** Full suite green before verify-work — coverage must remain ≥80%

### Wave 0 Gaps

- [ ] `pkg/warplib/protocol_ftp.go` — ftpProtocolDownloader implementation
- [ ] `pkg/warplib/protocol_ftp_test.go` — all FTP tests using mock server
- [ ] Test dependency: `github.com/fclairamb/ftpserverlib` (test-only, `go get ... -t`)
- [ ] `go.mod` / `go.sum` updated with `github.com/jlaffaye/ftp@v0.2.0` and test dependencies

## Sources

### Primary (HIGH confidence)

- `https://pkg.go.dev/github.com/jlaffaye/ftp` — Full API reference: `Dial`, `DialWithExplicitTLS`, `DialWithTLS`, `Login`, `Retr`, `RetrFrom`, `FileSize`, `Type`, `TransferTypeBinary`, `ServerConn` concurrency note ("not safe for concurrent calls")
- `https://github.com/jlaffaye/ftp` — README, module version v0.2.0 confirmation
- Go stdlib `net/textproto` — `textproto.Error{Code int, Msg string}` type used for FTP protocol errors
- Go stdlib `net/url` — `url.Parse()`, `u.User`, `u.User.Username()`, `u.User.Password()`, `u.User = nil` for credential stripping
- `/Users/divkix/GitHub/warpdl/.claude/worktrees/issues-fix/pkg/warplib/protocol.go` — `ProtocolDownloader` interface (15 methods), `DownloadCapabilities`, `ProbeResult`, `DownloadError`, `NewTransientError`, `NewPermanentError` — Phase 2 output, directly inspected
- `/Users/divkix/GitHub/warpdl/.claude/worktrees/issues-fix/pkg/warplib/protocol_router.go` — `SchemeRouter.Register` method — Phase 2 output, directly inspected
- `/Users/divkix/GitHub/warpdl/.claude/worktrees/issues-fix/pkg/warplib/manager.go` — `AddDownload` signature, `ResumeDownload` signature, `patchHandlers` implementation — directly inspected
- `/Users/divkix/GitHub/warpdl/.claude/worktrees/issues-fix/pkg/warplib/item.go` — `Item.Url` field is GOB-persisted — directly inspected — confirms credential-in-URL is a leak vector
- RFC 959 — FTP return code classification: 4xx transient, 5xx permanent

### Secondary (MEDIUM confidence)

- `https://pkg.go.dev/github.com/fclairamb/ftpserverlib` — ftpserverlib API for test server (MainDriver, afero.Fs backend) — verified from official pkg.go.dev
- STATE.md decision: "Use `github.com/jlaffaye/ftp` v0.2.0 for FTP (mature, TLS support, RetrFrom resume)" — confirmed against actual library API
- `https://github.com/jlaffaye/ftp/blob/master/client_test.go` — confirmed mock FTP server pattern used in library's own tests

### Tertiary (LOW confidence)

- Phase 2 decision to NOT implement `patchHandlers` for non-HTTP (from 02-01-SUMMARY.md "patchHandlers stays concrete *Downloader") — will require resolution in Phase 3

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — jlaffaye/ftp v0.2.0 API verified from pkg.go.dev; pre-decided in STATE.md
- Architecture: HIGH — Phase 2 interface directly inspected; FTP adapter patterns derived from verified API
- Pitfalls: HIGH — credential leakage verified from Item.Url GOB persistence; TLS options verified from API; patchHandlers coupling verified from source
- Open questions: MEDIUM — patchHandlers refactor approach and AddProtocolDownload signature need design decisions during planning

**Research date:** 2026-02-27
**Valid until:** 2026-04-30 (jlaffaye/ftp v0.2.0 is stable; no fast-moving dependencies)
