# warpdl · Google Drive plugin

Resolves Google Drive share links into direct download URLs so that
`warp download <drive-link>` just works. Handles the small-file fast
path and the large-file "virus scan warning" bypass automatically.

## Install

From the root of the warpdl checkout:

```bash
warp ext install plugins/gdrive
```

Verify it's loaded:

```bash
warp ext list
```

## Use

Give warp any of the following URL shapes:

```bash
# canonical share link
warp download "https://drive.google.com/file/d/FILE_ID/view?usp=sharing"

# "open" shortlink
warp download "https://drive.google.com/open?id=FILE_ID"

# already-direct uc link
warp download "https://drive.google.com/uc?export=download&id=FILE_ID"

# Google Docs / Sheets / Slides (exported to PDF / XLSX / PPTX)
warp download "https://docs.google.com/document/d/DOC_ID/edit"
warp download "https://docs.google.com/spreadsheets/d/SHEET_ID/edit"
warp download "https://docs.google.com/presentation/d/SLIDES_ID/edit"
```

The plugin itself just resolves the URL; warpdl then downloads the file
using its normal multi-part / resumable engine.

## What's supported

| URL kind | Result |
|---|---|
| `drive.google.com/file/d/<id>/...` | direct binary download |
| `drive.google.com/open?id=<id>` | direct binary download |
| `drive.google.com/uc?...id=<id>` | direct binary download |
| `docs.google.com/document/d/<id>/...` | PDF export |
| `docs.google.com/spreadsheets/d/<id>/...` | XLSX export |
| `docs.google.com/presentation/d/<id>/...` | PPTX export |
| `drive.google.com/drive/folders/<id>` | not supported; pick individual files |

## Limitations

- **Folders aren't enumerated.** One URL → one file. Drive folder
  downloads would require the engine to accept a list of URLs from a
  single plugin call; until that hook exists, pass individual files.
- **Daily quota.** Google's unauthenticated download endpoint is
  quota-limited (the usual "Too many users have viewed or downloaded
  this file recently" page). If you hit it, warpdl will surface an
  HTTP error and you'll need to wait or sign in via a browser, or use
  the OAuth flow described below (authenticated requests have a much
  higher per-user quota).

## How it works

1. Parse the file / doc / folder ID out of the URL using a short list
   of regex patterns.
2. For native Google Docs, build an `/export?format=...` URL and return
   it — no network roundtrip needed.
3. For binary files, probe `https://drive.google.com/uc?export=download&id=<ID>&confirm=t`.
   - If the response has a `Content-Disposition` header, Google is
     streaming the file directly — return that URL.
   - If the probe returns **401 or 403** the file is private. If an
     OAuth token is cached (see below) the plugin returns the Drive
     API v3 media URL (`https://www.googleapis.com/drive/v3/files/<ID>?alt=media`)
     plus a `Authorization: Bearer <token>` header that warpdl attaches
     to the download request. If no token is available, the original
     URL is returned so warpdl surfaces a clear "please log in" error.
   - Otherwise the response is the virus-scan warning page. The plugin
     finds the hidden `<form id="download-form">`, extracts its action
     and all hidden inputs (id, export, confirm, uuid, at), and builds
     a `drive.usercontent.google.com/download?...` URL that the
     warpdl engine can then fetch.
4. Any network or parsing failure falls back to the probe URL so that
   warpdl still gets a reasonable URL to try (and whatever error
   Google returns is surfaced to the user).

## OAuth Setup (v2)

Private Google Drive files require OAuth. Since v2 the plugin declares
an `auth` block in its manifest pointing at Google's OAuth 2.0 endpoints
with PKCE (public-client flow — no client secret needed).

To authenticate:

1. Install the plugin:

   ```bash
   warp ext install plugins/gdrive
   ```

2. Log in:

   ```bash
   warp auth login gdrive
   ```

   This opens your browser, asks for Drive read-only access, and stores
   the resulting access + refresh tokens in warpdl's encrypted
   credential store. The same command also triggers automatically the
   first time you try to download a private file if no token is cached.

3. Download:

   ```bash
   warp download "https://drive.google.com/file/d/<PRIVATE_ID>/view"
   ```

   warpdl resolves the URL through the plugin, which returns the Drive
   API v3 media URL plus a bearer token. warpdl's engine strips that
   bearer on any cross-origin redirect so the token is only sent to
   `googleapis.com`.

### Headless machines

If you don't have a browser on the host (SSH, server, container), use
the device-code flow:

```bash
warp auth login gdrive --device
```

It prints a short code and a URL you enter from another device (phone,
laptop). warp then polls Google until the device is approved.

### Managing credentials

```bash
warp auth list                  # show which plugins have tokens
warp auth status gdrive         # show scopes, expiry, account
warp auth logout gdrive         # revoke + delete the stored credential
```

### Scopes

The plugin requests a single scope: `drive.readonly`. Google's consent
screen will say "See and download all your Google Drive files". The
plugin cannot ask for any scope not declared in the manifest — the
warpdl engine enforces that at the binding level.

### Client ID

This plugin ships with a **placeholder** `client_id`. To install the
plugin in production you (the plugin author / distribution) should:

1. Register a new OAuth client in Google Cloud → APIs & Services →
   Credentials, type **Desktop app**.
2. Copy the generated client ID into `manifest.json`'s
   `auth.client_id` field. No client secret is needed (PKCE public
   client).
3. If you plan to distribute widely, complete Google's OAuth
   verification so users don't see the "unverified app" warning.

Never commit a Google client secret here — the engine will reject any
manifest with `client_secret` set.

## Testing

The Go tests mock out the injected `request` function (and
`getAccessToken` for the private-file path) and exercise every URL
shape, the virus-warning form parser (double- and single-quoted markup,
misleading earlier forms, long HTML), HTTP errors, network failure,
missing headers, User-Agent plumbing, and all four v2 private-file
branches (token returned, no binding registered, binding throws, empty
token). There is also a manifest sanity test that pins the auth-block
shape so drift fails loudly.

```bash
go test ./plugins/gdrive/... -count=1 -v
```

All assertions are deterministic; the tests don't touch the network.
