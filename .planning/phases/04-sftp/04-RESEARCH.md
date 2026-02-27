# Phase 4: SFTP - Research

**Researched:** 2026-02-27
**Domain:** SFTP file download over SSH — host key verification (TOFU), password + private key auth, resume via Seek offset
**Confidence:** HIGH

## Summary

Phase 4 adds SFTP download support to WarpDL by implementing `sftpProtocolDownloader` that mirrors the `ftpProtocolDownloader` pattern from Phase 3. The standard Go stack is `github.com/pkg/sftp v1.13.10` (client) + `golang.org/x/crypto/ssh v0.48.0+` (SSH transport + knownhosts). Both are pre-selected in STATE.md as locked decisions.

The single non-trivial engineering problem is TOFU host key verification in a daemon context. The daemon is headless — it cannot interactively prompt the user. The resolution is to perform TOFU in the CLI layer, not in the daemon: before sending the download request to the daemon, the CLI resolves the host key, checks `~/.config/warpdl/known_hosts`, and either accepts (first use) or rejects (mismatch) the connection. If accepted on first use, the key is appended to the known_hosts file. The daemon receives only well-verified credentials.

Resume works identically to FTP Phase 3: `sftp.File.Seek(offset, io.SeekStart)` before reading, with offset derived from the destination file's on-disk size via `WarpStat`. No parallel segments — SFTP is single-stream per the requirements.

**Primary recommendation:** Use `github.com/pkg/sftp v1.13.10` + `golang.org/x/crypto/ssh v0.48.0` (patches GO-2025-4134 and GO-2025-4135). Wire TOFU into the CLI layer, not the daemon, and prohibit `ssh.InsecureIgnoreHostKey()` outside test files via a CI lint gate.

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|-----------------|
| SFTP-01 | User can download files from `sftp://` URLs | `sftp.NewClient(sshConn)` + `client.Open(path)` + `file.WriteTo(dest)` |
| SFTP-02 | User can authenticate with password via URL (`sftp://user:pass@host/path`) | `ssh.Password(pass)` in `ssh.ClientConfig.Auth` |
| SFTP-03 | User can authenticate with SSH private key (default `~/.ssh/id_rsa`, `~/.ssh/id_ed25519`) | `ssh.ParsePrivateKey(pemBytes)` → `ssh.PublicKeys(signer)` |
| SFTP-04 | User can specify custom SSH key via `--ssh-key` flag | Pass key path through `DownloadParams` or `DownloaderOpts.SSHKeyPath`; parse in downloader factory |
| SFTP-05 | SFTP downloads are single-stream | `SupportsParallel: false, MaxConnections: 1` in `Capabilities()` |
| SFTP-06 | User can resume interrupted SFTP downloads via Seek offset | `sftp.File.Seek(offset, io.SeekStart)` after `client.Open(path)` |
| SFTP-07 | Host key verification uses TOFU policy with `~/.config/warpdl/known_hosts` | `knownhosts.New(path)` + `KeyError.Want` length check + `knownhosts.Line()` to append on first use |
| SFTP-08 | Custom port support via URL (`sftp://user@host:2222/path`) | `url.Parse(rawURL).Host` already includes port; default to 22 if missing |
| SFTP-09 | File size reported before download starts | `sftpClient.Stat(remotePath).Size()` in `Probe()` |
</phase_requirements>

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/pkg/sftp` | v1.13.10 | SFTP client (file open, stat, seek, read, write-to) | Only maintained Go SFTP client; used by rclone, Gitea, etc. v2 is alpha (Dec 2025) — do not use |
| `golang.org/x/crypto/ssh` | v0.48.0 | SSH transport, auth methods, host key callback | Required by pkg/sftp; v0.48.0 is the current release (Feb 9, 2026) — patches GO-2025-4134 (GSSAPI unbounded mem) and GO-2025-4135 (agent OOB read) |
| `golang.org/x/crypto/ssh/knownhosts` | (same module) | TOFU known_hosts parsing + host key writing | Standard library for OpenSSH-compatible known_hosts; `KeyError.Want` length discriminates unknown vs. mismatch |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/fclairamb/ftpserverlib` | already in go.mod | FTP test server | Already used for FTP tests; not needed for SFTP |
| `github.com/spf13/afero` | already in go.mod | In-memory FS for tests | Already available; use for SFTP mock server's virtual filesystem |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `github.com/pkg/sftp v1.13.10` | `github.com/pkg/sftp/v2 v2.0.0-alpha` | v2 has context support + io/fs but is alpha (Dec 2025), API unstable, not suitable for production |
| `knownhosts.New()` + custom callback | `ssh.InsecureIgnoreHostKey()` | InsecureIgnoreHostKey is forbidden outside test files — CI gate must enforce this |
| Per-connection known_hosts | System `~/.ssh/known_hosts` | WarpDL uses its own `~/.config/warpdl/known_hosts` to avoid polluting system SSH state |

**Installation:**
```bash
go get github.com/pkg/sftp@v1.13.10
go get golang.org/x/crypto@v0.48.0
```

Note: `golang.org/x/crypto` is currently at v0.47.0 in go.mod (indirect via `golang.org/x/net`). Phase 4 makes it a direct dependency at v0.48.0.

## Architecture Patterns

### Recommended File Structure
```
pkg/warplib/
├── protocol_sftp.go          # sftpProtocolDownloader — mirrors protocol_ftp.go
├── protocol_sftp_test.go     # Mock SFTP server + unit tests
└── known_hosts.go            # TOFU helper functions (NewTOFUHostKeyCallback, AppendKnownHost)

cmd/
└── download.go               # Add --ssh-key flag to dlFlags
common/
└── types.go                  # Add SSHKeyPath field to DownloadParams
internal/api/
└── download.go               # Add "sftp" case to downloadHandler switch
pkg/warplib/
└── manager.go                # Add ProtoSFTP case to ResumeDownload protocol guard
```

### Pattern 1: TOFU Host Key Callback

The daemon is headless. TOFU cannot be done in the daemon because it requires user interaction on first connection. The architecture places TOFU in the **CLI layer** before the download request is sent to the daemon. However, since the CLI sends a download request via the socket and the actual SSH connection is made by the daemon, the TOFU must happen in the daemon's `sftpProtocolDownloader.connect()` — but we need user feedback routed back to the CLI.

**Resolution for daemon-headless TOFU:** The `sftpProtocolDownloader.Probe()` establishes the SSH connection (which triggers host key verification). If the host is unknown, the downloader auto-accepts the key (TOFU policy), appends it to `~/.config/warpdl/known_hosts`, and logs the fingerprint. If the key has changed (mismatch), it returns a `*DownloadError` (permanent) with a human-readable error message. The fingerprint display is done as a log message from the daemon — the CLI receives the error and displays it. This avoids interactive prompting while still implementing TOFU.

```go
// Source: golang.org/x/crypto/ssh/knownhosts
func newTOFUHostKeyCallback(knownHostsFile string) (ssh.HostKeyCallback, error) {
    callback, err := knownhosts.New(knownHostsFile)
    if err != nil && !os.IsNotExist(err) {
        return nil, err
    }
    // If file doesn't exist, callback == nil; handle below
    return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
        if callback != nil {
            err := callback(hostname, remote, key)
            if err == nil {
                return nil // Host known and key matches
            }
            var keyErr *knownhosts.KeyError
            if errors.As(err, &keyErr) {
                if len(keyErr.Want) > 0 {
                    // Key mismatch — potential MITM. Hard reject.
                    return fmt.Errorf("sftp: host key mismatch for %s — possible MITM attack; remove old key from %s to re-trust", hostname, knownHostsFile)
                }
                // len(keyErr.Want) == 0 → unknown host → fall through to TOFU accept
            } else {
                return err // Other error (revoked, etc.) — propagate
            }
        }
        // Unknown host: auto-accept + append to known_hosts
        return appendKnownHost(knownHostsFile, hostname, remote, key)
    }, nil
}

func appendKnownHost(path, hostname string, remote net.Addr, key ssh.PublicKey) error {
    f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
    if err != nil {
        return err
    }
    defer f.Close()
    normalized := knownhosts.Normalize(hostname)
    line := knownhosts.Line([]string{normalized}, key)
    _, err = fmt.Fprintln(f, line)
    return err
}
```

**Warning signs of getting this wrong:**
- Using `ssh.InsecureIgnoreHostKey()` in non-test code — CI gate must reject this
- Mutating the known_hosts file from multiple goroutines — use a file lock or serialize via a mutex
- Not normalizing the hostname before writing (port 22 must be omitted, non-22 must be bracketed)

### Pattern 2: SFTPDownloader struct layout

Mirrors `ftpProtocolDownloader` exactly:

```go
type sftpProtocolDownloader struct {
    rawURL     string
    opts       *DownloaderOpts
    host       string   // host:port
    remotePath string   // file path on server
    user       string   // from URL userinfo — NOT persisted
    password   string   // from URL userinfo — NOT persisted
    sshKeyPath string   // from opts.SSHKeyPath — NOT persisted
    fileName   string
    fileSize   int64    // set during Probe
    hash       string
    dlDir      string
    savePath   string
    probed     bool
    stopped    int32    // atomic
    cleanURL   string   // URL with credentials stripped — safe to persist
    ctx        context.Context
    cancel     context.CancelFunc
}
```

### Pattern 3: SSH Auth method selection

```go
// Source: golang.org/x/crypto/ssh docs
func buildAuthMethods(password, sshKeyPath string) ([]ssh.AuthMethod, error) {
    if password != "" {
        return []ssh.AuthMethod{ssh.Password(password)}, nil
    }
    // No password — try private key
    keyPaths := resolveSSHKeyPaths(sshKeyPath)
    for _, kp := range keyPaths {
        pemBytes, err := os.ReadFile(kp)
        if err != nil {
            continue // Key file not found — try next
        }
        signer, err := ssh.ParsePrivateKey(pemBytes)
        if err != nil {
            return nil, fmt.Errorf("sftp: failed to parse SSH key %s: %w", kp, err)
        }
        return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
    }
    return nil, fmt.Errorf("sftp: no authentication method available (no password, no readable SSH key)")
}

func resolveSSHKeyPaths(explicitPath string) []string {
    if explicitPath != "" {
        return []string{explicitPath}
    }
    home, _ := os.UserHomeDir()
    return []string{
        filepath.Join(home, ".ssh", "id_ed25519"), // Prefer modern key
        filepath.Join(home, ".ssh", "id_rsa"),
    }
}
```

### Pattern 4: Download with Resume

```go
// Source: pkg.go.dev/github.com/pkg/sftp
func (d *sftpProtocolDownloader) download(ctx context.Context, handlers *Handlers, startOffset int64) error {
    sshConn, sftpClient, err := d.connect(ctx)
    if err != nil { return err }
    defer sshConn.Close()
    defer sftpClient.Close()

    remoteFile, err := sftpClient.Open(d.remotePath)
    if err != nil { return classifySFTPError("sftp", "download:open", err) }
    defer remoteFile.Close()

    if startOffset > 0 {
        if _, err := remoteFile.Seek(startOffset, io.SeekStart); err != nil {
            return NewPermanentError("sftp", "download:seek", err)
        }
    }

    flags := os.O_RDWR | os.O_CREATE
    if startOffset == 0 { flags |= os.O_TRUNC }
    localFile, err := WarpOpenFile(d.savePath, flags, DefaultFileMode)
    if err != nil { return NewPermanentError("sftp", "download:create", err) }
    defer localFile.Close()

    if startOffset > 0 {
        if _, err := localFile.Seek(startOffset, io.SeekStart); err != nil {
            return NewPermanentError("sftp", "download:localseek", err)
        }
    }

    pw := &sftpProgressWriter{handlers: handlers, hash: d.hash}
    _, err = io.Copy(io.MultiWriter(localFile, pw), remoteFile)
    return classifySFTPError("sftp", "download:copy", err)
}
```

### Pattern 5: In-Process SFTP Test Server

Use `pkg/sftp`'s own server-side implementation (`sftp.NewRequestServer` or `sftp.NewServer`) over an in-process SSH connection. This avoids external dependencies.

```go
// Source: github.com/pkg/sftp/server_integration_test.go pattern
func startMockSFTPServer(t *testing.T) (addr string, hostKey ssh.PublicKey, cleanup func()) {
    t.Helper()

    // Generate host key
    hostPrivKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
    hostSigner, _ := ssh.NewSignerFromKey(hostPrivKey)

    config := &ssh.ServerConfig{
        PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
            if c.User() == "testuser" && string(pass) == "testpass" {
                return &ssh.Permissions{}, nil
            }
            return nil, fmt.Errorf("password rejected")
        },
    }
    config.AddHostKey(hostSigner)

    listener, _ := net.Listen("tcp", "127.0.0.1:0")
    go acceptSFTPConnections(listener, config)

    return listener.Addr().String(), hostSigner.PublicKey(), func() { listener.Close() }
}
```

### Anti-Patterns to Avoid

- **Using `ssh.InsecureIgnoreHostKey()` in non-test code:** Silently accepts MITM. CI gate must scan for this.
- **Storing credentials in GOB:** SFTP password and SSH key path must NOT be persisted in `userdata.warp`. Only `cleanURL` (credential-stripped) is persisted — same as FTP.
- **Connecting from both `Probe()` and `Download()` separately:** Each connect() call checks host key — the TOFU file append must be idempotent and the file write must be concurrency-safe.
- **Assuming `~/.ssh/known_hosts` for TOFU storage:** Use `~/.config/warpdl/known_hosts` to isolate WarpDL's trust store from system SSH.
- **Blindly accepting passphrase-protected keys:** `ssh.ParsePrivateKey` returns `*ssh.PassphraseMissingError` for protected keys. Phase 4 does NOT support passphrase prompting (deferred to v2). Return a clear error.
- **Not normalizing port in known_hosts entry:** Port 22 is implicit (written as `hostname`), non-22 ports require `[hostname]:port` format — `knownhosts.Normalize()` handles this correctly.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| SSH known_hosts parsing | Custom parser | `knownhosts.New()` | OpenSSH format has hashed entries, wildcards, @cert-authority — subtle rules |
| known_hosts line format | `fmt.Sprintf` | `knownhosts.Line()` | Handles key serialization, address normalization, hashing |
| Address normalization | `strings.Replace` | `knownhosts.Normalize()` | IPv6 brackets, port-22 omission, colon-in-hostname rules |
| SSH private key parsing | PEM decode by hand | `ssh.ParsePrivateKey()` | Handles RSA, ECDSA, Ed25519, OpenSSH private key format (v2) |
| SFTP protocol | Custom implementation | `github.com/pkg/sftp` | SSH File Transfer Protocol is complex binary protocol with flow control |
| Key mismatch detection | String comparison | `KeyError.Want` length check | `len(Want) == 0` = unknown host; `len(Want) > 0` = mismatch. Single-field check, easy to get wrong |

**Key insight:** The `knownhosts` package is deceptively simple to misuse. The most common mistake is checking `err != nil` without inspecting `*KeyError` — you'll reject ALL unknown hosts instead of implementing TOFU.

## Common Pitfalls

### Pitfall 1: TOFU Callback Closure Over Nil `callback`
**What goes wrong:** If `~/.config/warpdl/known_hosts` does not yet exist, `knownhosts.New()` returns an error. Callers who discard this error and call the nil callback will panic.
**Why it happens:** First run with no known_hosts file — a common case.
**How to avoid:** Check `os.IsNotExist(err)` separately; if file doesn't exist, skip the `callback(...)` call and go straight to TOFU append.
**Warning signs:** Nil pointer dereference in HostKeyCallback.

### Pitfall 2: known_hosts File Concurrent Write
**What goes wrong:** If two simultaneous SFTP downloads connect to new hosts, both append to known_hosts concurrently, potentially corrupting the file.
**Why it happens:** SFTP downloads run in goroutines; file append is not atomic.
**How to avoid:** Wrap the known_hosts file write with a package-level `sync.Mutex`. The mutex only contends during first-use events (rare in practice, but must be safe).
**Warning signs:** Duplicate or truncated lines in known_hosts after concurrent downloads.

### Pitfall 3: Credentials in GOB Persistence
**What goes wrong:** If `rawURL` (containing `user:pass@host`) is stored as `item.Url` in GOB, credentials are written to `~/.config/warpdl/userdata.warp` in plaintext.
**Why it happens:** Developer copies FTP pattern but forgets `StripURLCredentials()`.
**How to avoid:** Always store `cleanURL` (result of `StripURLCredentials(rawURL)`) in the Item, not `rawURL`. Add a GOB round-trip test that verifies the stored URL has no userinfo.
**Warning signs:** Test `TestSFTPCredentialSecurityGOBRoundTrip` fails (per Phase 3 pattern).

### Pitfall 4: Port 22 in known_hosts Entry
**What goes wrong:** `knownhosts.Line([]string{"host:22"}, key)` writes `[host]:22` to known_hosts. Standard OpenSSH writes `host` (port 22 is implicit). Subsequent verification by system SSH fails because the format doesn't match.
**Why it happens:** Developer constructs the address manually without normalizing.
**How to avoid:** Use `knownhosts.Normalize(hostname)` before passing to `knownhosts.Line()`. `Normalize("host:22")` returns `"host"`, `Normalize("host:2222")` returns `"[host]:2222"`.
**Warning signs:** SSH system client can't verify the same host after WarpDL first-connects.

### Pitfall 5: InsecureIgnoreHostKey in Non-Test Code
**What goes wrong:** Developer uses `ssh.InsecureIgnoreHostKey()` as a quick fix for test reliability, then merges to main. All SFTP connections silently skip host verification.
**Why it happens:** TOFU test setup is more complex than `InsecureIgnoreHostKey()`.
**How to avoid:** CI lint gate: `grep -r "InsecureIgnoreHostKey" --include="*.go" | grep -v "_test.go" && exit 1`.
**Warning signs:** Test passes but the grep gate fails in CI.

### Pitfall 6: `ssh.PassphraseMissingError` Panic
**What goes wrong:** `ssh.ParsePrivateKey(pemBytes)` returns `*ssh.PassphraseMissingError` for passphrase-protected keys. If caller only checks `err != nil` and panics on `signer == nil`.
**Why it happens:** passphrase-protected keys are common but Phase 4 doesn't support them.
**How to avoid:** After `ParsePrivateKey`, check if error is `*ssh.PassphraseMissingError` and return a clear user-facing error: "SFTP key is passphrase-protected; passphrase keys are not yet supported (use an unprotected key)."
**Warning signs:** Panic in `ssh.PublicKeys(nil)` or confusing "nil pointer" error.

### Pitfall 7: ResumeDownload Protocol Guard Missing ProtoSFTP
**What goes wrong:** `manager.ResumeDownload()` switch statement has no case for `ProtoSFTP`, falls through to `default:` which returns "resume not supported for protocol sftp".
**Why it happens:** Developer adds SFTP downloader but forgets to update the protocol guard in manager.go.
**How to avoid:** Add `case ProtoSFTP:` to the switch alongside `case ProtoFTP, ProtoFTPS:`. The SFTP resume logic mirrors FTP: stat the destination file, seek, read.
**Warning signs:** `warpdl resume <sftp-hash>` returns "resume not supported for protocol sftp".

## Code Examples

Verified patterns from official sources:

### TOFU HostKeyCallback
```go
// Source: golang.org/x/crypto/ssh/knownhosts (pkg.go.dev/golang.org/x/crypto/ssh/knownhosts)
func newTOFUCallback(knownHostsFile string) (ssh.HostKeyCallback, error) {
    // Ensure directory exists
    if err := os.MkdirAll(filepath.Dir(knownHostsFile), 0700); err != nil {
        return nil, err
    }

    // Try to load existing known hosts (file may not exist yet)
    var cb ssh.HostKeyCallback
    if _, err := os.Stat(knownHostsFile); err == nil {
        var loadErr error
        cb, loadErr = knownhosts.New(knownHostsFile)
        if loadErr != nil {
            return nil, loadErr
        }
    }

    return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
        if cb != nil {
            err := cb(hostname, remote, key)
            if err == nil {
                return nil
            }
            var keyErr *knownhosts.KeyError
            if errors.As(err, &keyErr) {
                if len(keyErr.Want) > 0 {
                    // Mismatch — hard reject with actionable message
                    fp := ssh.FingerprintSHA256(key)
                    return fmt.Errorf(
                        "sftp: WARNING: host key changed for %s (got %s)\n"+
                        "If this is expected, remove the old entry from %s",
                        hostname, fp, knownHostsFile,
                    )
                }
                // Want is empty → unknown host → TOFU accept below
            } else {
                return err
            }
        }
        // First use: accept + persist
        knownHostsMu.Lock()
        defer knownHostsMu.Unlock()
        f, err := os.OpenFile(knownHostsFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
        if err != nil {
            return fmt.Errorf("sftp: failed to write known_hosts: %w", err)
        }
        defer f.Close()
        normalized := knownhosts.Normalize(hostname)
        line := knownhosts.Line([]string{normalized}, key)
        _, err = fmt.Fprintln(f, line)
        return err
    }, nil
}

// Package-level mutex for known_hosts writes (TOFU is rare, lock overhead is negligible)
var knownHostsMu sync.Mutex
```

### Probe (stat file size via SFTP)
```go
// Source: pkg.go.dev/github.com/pkg/sftp
func (d *sftpProtocolDownloader) Probe(ctx context.Context) (ProbeResult, error) {
    sshConn, sftpClient, err := d.connect(ctx)
    if err != nil {
        return ProbeResult{}, err
    }
    defer sshConn.Close()
    defer sftpClient.Close()

    info, err := sftpClient.Stat(d.remotePath)
    if err != nil {
        return ProbeResult{}, classifySFTPError("sftp", "probe:stat", err)
    }

    d.fileSize = info.Size()
    d.probed = true
    return ProbeResult{
        FileName:      d.fileName,
        ContentLength: d.fileSize,
        Resumable:     true,
    }, nil
}
```

### Private key auth with passphrase detection
```go
// Source: golang.org/x/crypto/ssh (pkg.go.dev/golang.org/x/crypto/ssh)
pemBytes, err := os.ReadFile(keyPath)
if err != nil {
    return nil, fmt.Errorf("sftp: cannot read SSH key %s: %w", keyPath, err)
}
signer, err := ssh.ParsePrivateKey(pemBytes)
if err != nil {
    var passErr *ssh.PassphraseMissingError
    if errors.As(err, &passErr) {
        return nil, fmt.Errorf("sftp: SSH key %s is passphrase-protected; use an unprotected key or specify --ssh-key", keyPath)
    }
    return nil, fmt.Errorf("sftp: failed to parse SSH key %s: %w", keyPath, err)
}
```

### SchemeRouter registration
```go
// In NewSchemeRouter or daemon init (mirrors FTP registration pattern)
sftpFactory := func(rawURL string, opts *DownloaderOpts) (ProtocolDownloader, error) {
    return newSFTPProtocolDownloader(rawURL, opts)
}
r.routes["sftp"] = sftpFactory
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| System SSH binary via `exec.Command` | In-process `golang.org/x/crypto/ssh` | Go 1.3 era | No subprocess, full control over host key callback |
| `ssh.InsecureIgnoreHostKey()` for quick dev | `knownhosts.New()` + `*KeyError` inspection | golang.org/x/crypto v0.1.0+ | Proper TOFU without accepting MITM |
| `sftp v1` (`RealPath` old signature) | `sftp v1.13.5+` (signature change in v1.13.5 with backward compat shim) | v1.13.5, Oct 2024 | Must use v1.13.5+ to avoid breakage |
| `golang.org/x/crypto v0.44.0` | `v0.45.0+` (patches GO-2025-4134, GO-2025-4135) | Nov–Dec 2025 | GSSAPI DoS and agent OOB read fixed |

**Deprecated/outdated:**
- `github.com/pkg/sftp/v2`: Alpha as of Dec 2025. API unstable. Do NOT use until stable release.
- `ssh.InsecureIgnoreHostKey()`: Forbidden in non-test code for this project. CI gate enforces.
- `x/crypto < v0.45.0`: Two known CVEs. Must use v0.45.0+; current version in go.mod is v0.47.0 (indirect), bump to v0.48.0 as direct dep.

## Open Questions

1. **`--sftp-insecure` flag (bypass host key check)**
   - What we know: ROADMAP plan 04-01 mentions `--sftp-insecure` flag
   - What's unclear: Should this flag be in the CLI (warpcli) or daemon-side via DownloadParams?
   - Recommendation: Add to `DownloadParams.SFTPInsecure bool` field. In `newSFTPProtocolDownloader`, if `opts.SFTPInsecure == true`, use a permissive callback (accept any key but still log the fingerprint). This allows testing against servers with self-signed host keys without editing known_hosts.

2. **Re-reading known_hosts on each connect (or cache it)**
   - What we know: `knownhosts.New()` reads the file at call time; subsequent appends are not reflected in the returned callback
   - What's unclear: If multiple SFTP downloads connect to new hosts, the callback created at factory time won't see keys added by later connections
   - Recommendation: Recreate the callback on each `connect()` call (not at factory time). The file I/O overhead is negligible vs. the network connection.

3. **`--ssh-key` flag threading through common.DownloadParams**
   - What we know: `DownloadParams` currently has no SSHKeyPath field
   - What's unclear: Should the SSH key path be sent to the daemon, or should the daemon resolve the default key independently?
   - Recommendation: Add `SSHKeyPath string` to `DownloadParams`. This lets the user specify `--ssh-key /path/to/key` from the CLI. The daemon receives the path string; the actual key file is read by the daemon process. The key path is NOT persisted in GOB (only the cleanURL is stored in Item).

## Sources

### Primary (HIGH confidence)
- `golang.org/x/crypto/ssh/knownhosts` — [pkg.go.dev/golang.org/x/crypto/ssh/knownhosts](https://pkg.go.dev/golang.org/x/crypto/ssh/knownhosts) — KeyError, New(), Line(), Normalize() APIs verified
- `golang.org/x/crypto/ssh` — [pkg.go.dev/golang.org/x/crypto/ssh](https://pkg.go.dev/golang.org/x/crypto/ssh) — ClientConfig, AuthMethod, Password(), PublicKeys(), ParsePrivateKey() verified
- `github.com/pkg/sftp v1.13.10` — [pkg.go.dev/github.com/pkg/sftp](https://pkg.go.dev/github.com/pkg/sftp) — File.Seek(), Client.Open(), Client.Stat(), NewClient() verified
- `github.com/pkg/sftp/v2 v2.0.0-alpha` — [pkg.go.dev/github.com/pkg/sftp/v2](https://pkg.go.dev/github.com/pkg/sftp/v2) — Confirmed alpha/unstable as of Dec 2025
- GO-2025-4134 — [pkg.go.dev/vuln/GO-2025-4134](https://pkg.go.dev/vuln/GO-2025-4134) — Minimum patched version is v0.45.0
- GO-2025-4135 — [pkg.go.dev/vuln/GO-2025-4135](https://pkg.go.dev/vuln/GO-2025-4135) — Same patched version requirement

### Secondary (MEDIUM confidence)
- In-process SFTP test server pattern — [github.com/pkg/sftp/blob/master/server_integration_test.go](https://github.com/pkg/sftp/blob/master/server_integration_test.go) — Verified test pattern uses `ssh.NewServerConn` + `sftp.NewServer`
- `knownhosts.Normalize()` behavior for port 22 — [pkg.go.dev/golang.org/x/crypto/ssh/knownhosts](https://pkg.go.dev/golang.org/x/crypto/ssh/knownhosts) — Verified in docs

### Tertiary (LOW confidence)
- TOFU headless daemon pattern (accept-on-first-use in daemon, not CLI) — derived from analysis of the existing daemon architecture; no single authoritative source for this design choice

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — verified against pkg.go.dev official docs, both libraries are established
- Architecture: HIGH — directly derived from existing Phase 3 (FTP) patterns which are already in the codebase; mirrors exactly
- Pitfalls: MEDIUM-HIGH — credential/TOFU pitfalls are verified against official docs; concurrent file write is reasoning from known concurrent Go patterns
- TOFU headless architecture: MEDIUM — architectural inference from daemon design; the design is sound but no prior art in this specific codebase

**Research date:** 2026-02-27
**Valid until:** 2026-03-27 (stable libraries; golang.org/x/crypto moves fast on CVEs — check for new patches before implementation)
