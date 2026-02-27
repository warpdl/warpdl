# Pitfalls Research

**Domain:** Go download manager protocol expansion (FTP/SFTP/HTTP-redirect/JSON-RPC)
**Researched:** 2026-02-26
**Confidence:** HIGH (library docs + official Go issues + CVE records verified)

---

## Critical Pitfalls

### Pitfall 1: FTP Connection Is Not Goroutine-Safe — Parallel Segments Will Panic

**What goes wrong:**
`jlaffaye/ftp` `ServerConn` explicitly documents: "A single connection only supports one in-flight data connection. It is not safe to be called concurrently." Developers see that HTTP uses parallel goroutines and blindly apply the same pattern to FTP by launching multiple goroutines against the same connection, causing race conditions, corrupted responses, or panics.

**Why it happens:**
WarpDL's HTTP downloader (`dloader.go`) opens many goroutines per download with a shared HTTP client (which is safe because `http.Client` is goroutine-safe). The same code shape applied to FTP will not work — FTP is a stateful, sequential protocol with two separate control and data channels, both tracked on the same connection struct.

**How to avoid:**
FTP downloads must be single-stream. Do not attempt parallel segments. The `RetrFrom(path, offset)` method supports resume from an offset, which is the only "parallelism" available: one sequential download, resumable. If the project wants to allow FTP at all, the FTP downloader must implement the single-stream download path, bypassing the segment machinery entirely.

**Warning signs:**
- Any code that calls FTP methods from multiple goroutines sharing one `*ftp.ServerConn`
- Applying `maxConnections > 1` to an FTP download
- A `sync.WaitGroup` with multiple goroutines each invoking `Retr` or `RetrFrom` on the same client

**Phase to address:**
FTP protocol support implementation phase (before any code is written, enforce that the FTP downloader is a distinct type with `maxConnections` hardcoded to 1).

---

### Pitfall 2: CVE-2024-45336 — Authorization Header Re-Sent After Cross-Domain Redirect Chain

**What goes wrong:**
Go's `net/http` client, before versions 1.22.11 / 1.23.5 / 1.24.0, had a bug where a redirect chain (a.com → b.com/1 → b.com/2) would incorrectly re-attach the Authorization header on the second same-domain redirect (b.com/2), even though it had been stripped on the cross-domain hop. Any redirect implementation that relies on default `http.Client` behavior without pinning to a fixed Go version could silently send credentials to the wrong server.

**Why it happens:**
The fix for this CVE shipped in Go 1.22.11+ / 1.24+. If WarpDL's HTTP redirect support does not enforce the minimum patched Go version, or if it uses a custom `CheckRedirect` that inadvertently restores stripped headers, credentials will leak.

**How to avoid:**
1. Require Go >= 1.24 in `go.mod` (already at 1.24.9, so this is satisfied).
2. Do not override `CheckRedirect` with logic that manually copies request headers from the original request — this bypasses the standard library's stripping logic.
3. Add a regression test that follows a cross-domain redirect chain and verifies the Authorization header is absent on the cross-domain leg.

**Warning signs:**
- Custom `CheckRedirect` that passes `req.Header` from the initial request to subsequent requests
- Forwarding cookies or auth headers manually across all redirect hops unconditionally

**Phase to address:**
HTTP redirect support phase.

---

### Pitfall 3: JSON-RPC WebSocket Endpoint Vulnerable to CSRF Even on Localhost

**What goes wrong:**
The existing `web.go` WebSocket server already binds to `:<port>` with no authentication — any malicious webpage visited in the user's browser can open a WebSocket to `ws://localhost:<port>` and issue any RPC command. The JSON-RPC API, if it exposes download creation/deletion/file access, will have the same vulnerability. Localhost binding does NOT prevent browser-originated connections.

**Why it happens:**
This is a widely misunderstood security model. Developers assume localhost means "not accessible from the internet" and conflate network-layer isolation with browser security model isolation. Browsers freely allow pages to open WebSocket connections to localhost. Real-world exploits of this pattern exist (CryptoNote wallets, Claude Code CVE-2025-52882, Mopidy).

**How to avoid:**
1. Require a bearer token on every JSON-RPC request — embed it in the Authorization header for HTTP and as first param (`"token:<secret>"`) for WebSocket, matching the aria2 pattern.
2. Generate a random token at daemon startup; store it in a lock file or return it on daemon start output so the CLI can read it.
3. For the WebSocket endpoint, verify the `Origin` header on upgrade to reject browser-originated connections from non-localhost origins.
4. Never expose the JSON-RPC endpoint on `0.0.0.0` by default; bind to `127.0.0.1` explicitly.

**Warning signs:**
- WebSocket handler that never checks `Authorization` or a `token` parameter
- `ListenAndServe` with `":"` prefix instead of `"127.0.0.1:"`
- No token generation at daemon startup

**Phase to address:**
JSON-RPC API implementation phase — security must be designed in, not bolted on.

---

### Pitfall 4: SFTP `ssh.InsecureIgnoreHostKey()` Shipped in Production

**What goes wrong:**
The `golang.org/x/crypto/ssh` package requires the `HostKeyCallback` field in `ClientConfig` to be set. A survey of real-world Go SSH clients found almost no projects actually implemented proper host key verification — most used `ssh.InsecureIgnoreHostKey()`. This bypasses all MITM protection and allows any server to impersonate the target.

**Why it happens:**
The Go SSH package returns an error if `HostKeyCallback` is nil (it was changed from "accept all" default to "error" in a security fix). Developers set `InsecureIgnoreHostKey()` to make the error go away, then forget to replace it with real verification.

**How to avoid:**
Use `golang.org/x/crypto/ssh/knownhosts` to build a callback from the system's `~/.ssh/known_hosts` file, or require users to pass a known host key fingerprint. Do not use `InsecureIgnoreHostKey()` in any code path except tests (and even there, comment it clearly). WarpDL should either: (a) verify from `~/.ssh/known_hosts` by default, or (b) expose an `--sftp-insecure` flag that is explicitly opt-in with a warning.

**Warning signs:**
- Any occurrence of `ssh.InsecureIgnoreHostKey()` outside `_test.go` files
- `ClientConfig{HostKeyCallback: nil}` in non-test code
- No `knownhosts` import anywhere in the SFTP implementation

**Phase to address:**
SFTP protocol support implementation phase.

---

### Pitfall 5: FTP/SFTP Item Persisted With Protocol-Specific Fields That Break GOB Deserialization

**What goes wrong:**
WarpDL persists `ManagerData` (including all `Item` structs) as GOB-encoded bytes. The `Item` struct currently has `Parts map[int64]*ItemPart` representing byte-range offsets — a concept that is meaningless for FTP/SFTP single-stream downloads. If a new field is added to `Item` (e.g., `Protocol`, `FTPCredentials`, `SFTPKeyPath`) that changes how deserialization must work, existing users' persisted state from before the upgrade will decode incorrectly or fail silently.

**Why it happens:**
GOB is additive for new fields (unknown fields ignored) but changing field types or removing fields breaks old serialized files. The real risk is that adding a non-zero default field whose zero value has the wrong semantic meaning will make old `Item` records appear to be FTP items or have empty protocol fields.

**How to avoid:**
1. New fields added to `Item` must have zero values that mean "HTTP" or "undefined" (i.e., default to existing behavior).
2. If a `Protocol` enum is added, value `0` must map to HTTP, not FTP or empty.
3. Write a migration test: encode an `Item` with the old schema, decode with the new schema, verify all fields have correct defaults.
4. Never change the type of an existing field — add a new field instead and migrate.

**Warning signs:**
- A `Protocol` field with iota starting at `FTP = iota` (FTP = 0) rather than `HTTP = iota`
- Removing or renaming existing `Item` fields during protocol integration

**Phase to address:**
Any phase that modifies `pkg/warplib/item.go` or `manager.go`.

---

### Pitfall 6: FTPS TLS Certificate Verification Disabled for "Compatibility"

**What goes wrong:**
FTPS servers (FTP over TLS) commonly use self-signed certificates, especially on internal NAS devices and legacy servers. Developers set `InsecureSkipVerify: true` in the `tls.Config` to avoid dealing with certificate errors, shipping a man-in-the-middle vulnerability to every user who downloads over FTPS.

**Why it happens:**
Self-signed FTPS certificates fail TLS verification by default. The path of least resistance is to skip verification. Real-world examples: filestash project had this exact bug reported and fixed.

**How to avoid:**
1. Default to full TLS verification for FTPS.
2. Add an `--ftps-insecure` flag that explicitly opts in to skip verification with a printed warning.
3. Provide documentation on how to add a custom CA certificate instead of disabling verification.
4. Never let `InsecureSkipVerify: true` appear without a user-visible warning.

**Warning signs:**
- `tls.Config{InsecureSkipVerify: true}` in non-test FTP code with no user-facing warning
- A single TLS config object reused across FTP and SFTP that has verification disabled

**Phase to address:**
FTP protocol support implementation phase.

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Copy the HTTP `Downloader` struct and swap the `http.Client` for FTP/SFTP client | Fast implementation | FTP/SFTP downloaders inherit maxParts/maxConn logic that will panic or corrupt when > 1 | Never — create a distinct single-stream downloader type |
| Use `ssh.InsecureIgnoreHostKey()` for SFTP | Avoids known_hosts setup complexity | MITM-able in production | Test code only, never production |
| Reuse existing WebSocket endpoint from `web.go` for JSON-RPC without adding auth | Fastest path to JSON-RPC | Every user with any browser tab is a potential attacker | Never |
| Skip FTPS and implement FTP-only (unencrypted) | Simpler TLS handling | Users' FTP credentials sent in plaintext | Never — always support FTPS, plain FTP is opt-in |
| Hard-code `maxConnections = 1` for FTP without abstraction | Works now | If the protocol abstraction adds proper interface later, workaround gets lost | Acceptable in MVP if documented with a TODO and enforced via a named constant `ftpMaxConnections = 1` |

---

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| `jlaffaye/ftp` RetrFrom | Not closing the `*ftp.Response` — leaks the FTP data connection, causing the next call to hang | Always `defer r.Close()` immediately after `Retr`/`RetrFrom` |
| `pkg/sftp` file download | Using `WriteTo()` on servers that delete-on-stat (some queue-mapped mainframe servers) | Use `sftp.UseConcurrentReads(false)` and read to EOF instead |
| Go `net/http` redirect | Setting `CheckRedirect` to `nil` makes the client follow up to 10 redirects silently; setting it to a custom func and then manually propagating all headers re-introduces the CVE-2024-45336 pattern | Use default `CheckRedirect` (nil = 10 redirects) and let the standard library handle header stripping; only customize if you need to stop at a specific status code |
| aria2-style token auth in JSON-RPC | Putting the token in the JSON body only (not checked for WebSocket connections without a body) | Check the token on every connection upgrade (WebSocket `Sec-WebSocket-Protocol` or first message) AND on every HTTP POST `Authorization` header |
| FTP passive mode behind NAT | Client receives the server's internal/private IP in PASV response; connection to that IP fails | The `jlaffaye/ftp` library handles EPSV/PASV automatically; just ensure the server is properly configured; this is a server admin issue, not a client code issue — but document the failure mode |

---

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Creating a new FTP connection for every download operation (CWD, SIZE, RETR all separate connections) | Extreme connection overhead on FTP servers that limit concurrent connections | Reuse a single `*ftp.ServerConn` per download for all operations (SIZE, then RETR) | Every download |
| SFTP MaxPacket too small | Very slow SFTP downloads (many small round-trips) | Set `sftp.MaxPacket(32768)` (default) or higher; 262144 is a common high-performance value | Downloads > 1MB |
| JSON-RPC over WebSocket broadcasting all progress events to all connected clients without filtering | Client A sees progress for client B's downloads | Filter progress broadcasts by download hash or subscription — only send events to clients that have subscribed to that download | > 2 simultaneous WebSocket clients |
| Progress handler firing for every byte read (calling `pool.Broadcast` per-byte) | High CPU, locks contend on Pool | Throttle progress updates to 1Hz or per-chunk as already done in HTTP downloader | Any download |

---

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| JSON-RPC endpoint without token auth | Any malicious webpage (via CSRF/WebSocket) can create, stop, or inspect downloads | Token required on all requests; generate at daemon start; verify on every connection |
| FTP credentials passed in URL (`ftp://user:pass@host`) without scrubbing from logs | Credentials visible in debug logs, persisted in download history | Extract credentials from URL before storing in `Item.Url`; store in `credman` separately; log sanitized URL |
| SFTP `InsecureIgnoreHostKey` in production | Attacker can MITM the SSH connection and steal transferred files | Use `knownhosts.New(path)` callback; require explicit `--sftp-insecure` to disable |
| Binding JSON-RPC to `0.0.0.0` | Remote attackers on the same network can access the API | Default to `127.0.0.1` binding; require explicit `--rpc-listen-all` flag with documentation warning |
| Not stripping FTP/SFTP password from `Item.Url` before GOB persistence | Passwords stored in plaintext in `~/.config/warpdl/userdata.warp` | Strip credentials from URL before creating `Item`; store in credman if persistence needed |

---

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| FTP download shows "0 segments" in progress UI because single-stream bypasses segment machinery | User thinks download is broken or not started | Show a single progress bar for FTP/SFTP with total bytes/speed; no segment count |
| SFTP host key mismatch error with no explanation | User doesn't know how to fix it; gives up or uses `--insecure` | Print the expected vs. received fingerprint; print the command to add the key to known_hosts |
| JSON-RPC token not shown to user after daemon start | Third-party integrations can't connect; user doesn't know the token | Print the token on `warpdl daemon` start or write it to a well-known file (e.g., `~/.config/warpdl/rpc.token`) |
| FTP download claimed "complete" but file is truncated (connection dropped mid-stream) | Corrupted file silently saved | Validate downloaded byte count against SIZE command result after download; error if mismatch |

---

## "Looks Done But Isn't" Checklist

- [ ] **FTP resume:** `RetrFrom(path, offset)` used, but the FTP server must also support `REST` command — verify with `ServerConn.IsRetrFrom()` or check server features before assuming resume works
- [ ] **SFTP progress:** `pkg/sftp` does not natively emit progress callbacks — must wrap the `io.Reader` with a custom counting reader that calls the `DownloadProgressHandler`
- [ ] **HTTP redirects:** Default client follows up to 10 redirects, but the final effective URL is not reflected back in the `Item.Url` — the stored URL may not be the actual download URL
- [ ] **JSON-RPC WebSocket:** WebSocket is established but never sends a ping/pong keepalive — long-idle connections silently drop; client must reconnect or server must send periodic pings
- [ ] **FTP FTPS fallback:** Code connects FTP plain-text as fallback when FTPS fails — silently downgrades security; require explicit user opt-in for plain FTP
- [ ] **Token auth on WebSocket:** HTTP POST path checks Authorization header, but WebSocket path does not check the token (two code paths that must both be secured)
- [ ] **SFTP known_hosts:** Code reads `~/.ssh/known_hosts` but doesn't handle the case where the file doesn't exist (first-time users); must gracefully offer to add the key with user confirmation, not just error out

---

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| FTP concurrency panic in production | HIGH | Hotfix: enforce `maxConnections = 1` for FTP; rebuild and redistribute binary; no data migration needed |
| CVE-2024-45336 exposure (Go version not pinned) | MEDIUM | Update `go.mod` minimum to 1.24+; rebuild; test redirect chains with auth headers |
| JSON-RPC without auth discovered post-release | HIGH | Issue security advisory; add mandatory token in patch release; document upgrade path for existing integrators |
| GOB deserialization failure after Item struct change | HIGH | Write migration: read old format, transform, write new format; provide `warpdl migrate` command; test on real userdata.warp file |
| SFTP with InsecureIgnoreHostKey released | MEDIUM | Patch to use knownhosts; make SFTP connections fail for unrecognized hosts by default; provide migration docs |

---

## Pitfall-to-Phase Mapping

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| FTP concurrency (pitfall 1) | FTP implementation phase | Test: assert `maxConnections == 1` for any FTP downloader via unit test; race detector test with fake FTP server |
| CVE-2024-45336 header leakage (pitfall 2) | HTTP redirect implementation phase | Test: redirect chain a.com → b.com/1 → b.com/2 with Authorization header; assert header absent at b.com legs |
| JSON-RPC CSRF/WebSocket auth (pitfall 3) | JSON-RPC API implementation phase | Test: unauthenticated WebSocket connection is rejected with 401; CSRF simulation test with browser Origin header |
| SFTP InsecureIgnoreHostKey (pitfall 4) | SFTP implementation phase | Code review gate: grep for `InsecureIgnoreHostKey` in non-test files fails CI |
| GOB backward compatibility (pitfall 5) | Any phase touching Item/Manager structs | Test: decode a fixture file created with old schema using new code; verify defaults |
| FTPS certificate skip (pitfall 6) | FTP implementation phase | Code review gate: grep for `InsecureSkipVerify: true` in non-test files fails CI |
| FTP credentials in URL (security) | FTP implementation phase | Test: stored Item.Url for ftp://user:pass@host contains no password |
| JSON-RPC token not generated (UX) | JSON-RPC API implementation phase | Integration test: start daemon, read token from output, make authenticated RPC call |

---

## Sources

- [jlaffaye/ftp pkg.go.dev — "not safe to be called concurrently"](https://pkg.go.dev/github.com/jlaffaye/ftp) — HIGH confidence (official docs)
- [pkg/sftp pkg.go.dev — concurrent reads, UseConcurrentReads, AWS compatibility](https://pkg.go.dev/github.com/pkg/sftp) — HIGH confidence (official docs)
- [CVE-2024-45336 — Go net/http Authorization header re-sent after cross-domain redirect chain](https://www.cvedetails.com/cve/CVE-2024-45336/) — HIGH confidence (CVE record)
- [golang/go issue #70530 — Sensitive headers incorrectly sent after cross-domain redirect](https://github.com/golang/go/issues/70530) — HIGH confidence (official Go issue tracker)
- [golang.org/x/crypto/ssh/knownhosts — HostKeyCallback documentation](https://pkg.go.dev/golang.org/x/crypto/ssh/knownhosts) — HIGH confidence (official docs)
- [x/crypto/ssh issue #19767 — make ClientConfig HostKeyCallback non-permissive](https://github.com/golang/go/issues/19767) — HIGH confidence (official Go issue)
- [Go SSH with Host Key Verification — survey finding almost no projects verify host keys](https://skarlso.github.io/2019/02/17/go-ssh-with-host-key-verification/) — MEDIUM confidence (community article)
- [CVE-2025-52882 — WebSocket auth bypass on localhost JSON-RPC](https://securitylabs.datadoghq.com/articles/claude-mcp-cve-2025-52882/) — HIGH confidence (Datadog Security Labs)
- [Mopidy issue #1659 — localhost RPC not protected against CSRF](https://github.com/mopidy/mopidy/issues/1659) — MEDIUM confidence (community report)
- [Unauthenticated JSON-RPC API cryptonote takeover](https://www.ayrx.me/cryptonote-unauthenticated-json-rpc/) — MEDIUM confidence (security research)
- [aria2 JSON-RPC token auth spec](https://aria2.github.io/manual/en/html/aria2c.html) — HIGH confidence (official aria2 docs)
- [filestash FTPS InsecureSkipVerify bug](https://github.com/mickael-kerjean/filestash/issues/710) — MEDIUM confidence (real-world bug report)
- [encoding/gob — field compatibility rules](https://pkg.go.dev/encoding/gob) — HIGH confidence (official docs)
- [Datadog TLS skip verify Go security rule](https://docs.datadoghq.com/security/code_security/static_analysis/static_analysis_rules/go-security/tls-skip-verify/) — HIGH confidence (official Datadog docs)

---
*Pitfalls research for: WarpDL protocol expansion (FTP/SFTP/HTTP-redirects/JSON-RPC)*
*Researched: 2026-02-26*
