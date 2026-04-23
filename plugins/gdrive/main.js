// Google Drive plugin for warpdl.
//
// Resolves Google Drive share URLs (file, Google Docs, folders) into a
// direct download URL that warpdl can stream. Handles both the small-
// file path (served directly via `uc?export=download`) and the large-
// file path where Google interposes an HTML virus-scan warning form
// that the user is expected to re-submit.
//
// The plugin exposes a single entry point, `extract(url)`, as required
// by warpdl's extension engine.

// URL patterns used to pull the Google Drive file / doc / folder id.
// Ordered from most-specific (share URLs) to least-specific (?id=...).
var ID_PATTERNS = [
  /\/file\/d\/([a-zA-Z0-9_-]+)/,
  /\/document\/d\/([a-zA-Z0-9_-]+)/,
  /\/spreadsheets\/d\/([a-zA-Z0-9_-]+)/,
  /\/presentation\/d\/([a-zA-Z0-9_-]+)/,
  /\/drive\/folders\/([a-zA-Z0-9_-]+)/,
  /[?&]id=([a-zA-Z0-9_-]+)/,
];

// A realistic User-Agent. Google occasionally returns different pages
// for empty / Go-default User-Agents, which breaks the HTML parsing.
var UA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 " +
  "(KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36 warpdl/1.0";

// Cap on how much of the HTML warning page we'll scan for a form.
// The page is small (~5 KB); 64 KB is a very generous upper bound
// and also below the 1 MB cap enforced by the engine's request().
var HTML_SCAN_LIMIT = 64 * 1024;

// -- Pure helpers (no network, easy to test) -------------------------

function extractFileId(url) {
  if (!url || typeof url !== "string") return null;
  for (var i = 0; i < ID_PATTERNS.length; i++) {
    var m = url.match(ID_PATTERNS[i]);
    if (m && m[1]) return m[1];
  }
  return null;
}

function classifyUrl(url) {
  if (/\/drive\/folders\//.test(url)) return "folder";
  if (/\/document\/d\//.test(url)) return "doc";
  if (/\/spreadsheets\/d\//.test(url)) return "sheet";
  if (/\/presentation\/d\//.test(url)) return "slides";
  return "file";
}

function docExportUrl(kind, fileId) {
  switch (kind) {
    case "doc":
      return "https://docs.google.com/document/d/" + fileId +
        "/export?format=pdf";
    case "sheet":
      return "https://docs.google.com/spreadsheets/d/" + fileId +
        "/export?format=xlsx";
    case "slides":
      return "https://docs.google.com/presentation/d/" + fileId +
        "/export?format=pptx";
    default:
      return null;
  }
}

function ucDownloadUrl(fileId) {
  return "https://drive.google.com/uc?export=download&id=" + fileId +
    "&confirm=t";
}

// looksLikeHtml heuristically decides whether a response body is the
// virus-scan warning page (as opposed to binary file content that
// happened to be served via the same URL).
function looksLikeHtml(body) {
  if (!body) return false;
  var head = String(body).slice(0, 1024).toLowerCase();
  return head.indexOf("<!doctype html") >= 0 ||
    head.indexOf("<html") >= 0;
}

// parseForm extracts the download-form's action and all hidden input
// key/value pairs from the virus-warning HTML. Returns null if the
// form is not present.
function parseForm(html) {
  if (!html) return null;

  // Cap the slice we look at so a pathological response can't wedge
  // the regex engine.
  var slice = String(html).slice(0, HTML_SCAN_LIMIT);

  var openTag = slice.match(
    /<form\b[^>]*\bid=["']download-form["'][^>]*>/i
  );
  if (!openTag) return null;

  var actionMatch = openTag[0].match(/\baction=["']([^"']+)["']/i);
  var action = actionMatch ? actionMatch[1] :
    "https://drive.usercontent.google.com/download";

  var bodyStart = slice.indexOf(openTag[0]) + openTag[0].length;
  var bodyEnd = slice.indexOf("</form>", bodyStart);
  if (bodyEnd < 0) bodyEnd = slice.length;
  var body = slice.slice(bodyStart, bodyEnd);

  var params = {};
  var inputRe = /<input\b[^>]*>/gi;
  var m;
  while ((m = inputRe.exec(body)) !== null) {
    var raw = m[0];
    var nameMatch = raw.match(/\bname=["']([^"']+)["']/i);
    if (!nameMatch) continue;
    var valueMatch = raw.match(/\bvalue=["']([^"']*)["']/i);
    params[nameMatch[1]] = valueMatch ? valueMatch[1] : "";
  }

  return { action: action, params: params };
}

function buildQuery(base, params) {
  var keys = [];
  for (var k in params) {
    if (!Object.prototype.hasOwnProperty.call(params, k)) continue;
    keys.push(
      encodeURIComponent(k) + "=" + encodeURIComponent(params[k])
    );
  }
  if (keys.length === 0) return base;
  var sep = base.indexOf("?") >= 0 ? "&" : "?";
  return base + sep + keys.join("&");
}

// -- Network helpers -------------------------------------------------

function probe(url) {
  // `request` is injected by the extension runtime. In tests we can
  // replace it with a stub.
  try {
    return request({
      method: "GET",
      url: url,
      headers: { "User-Agent": UA },
      body: "",
    });
  } catch (err) {
    print("[gdrive] probe failed:", err);
    return null;
  }
}

// hasContentDisposition guards against responses that lack a headers
// object (e.g. when the plugin is unit-tested with a minimal stub).
function hasContentDisposition(resp) {
  if (!resp || !resp.headers) return false;
  try {
    var v = resp.headers.get("Content-Disposition");
    return !!v;
  } catch (e) {
    return false;
  }
}

// driveApiDownloadUrl is the authenticated Drive API v3 URL that streams
// the raw file bytes when accompanied by a bearer token.
function driveApiDownloadUrl(fileId) {
  return "https://www.googleapis.com/drive/v3/files/" + fileId +
    "?alt=media";
}

// tryGetAccessToken asks the host engine for a short-lived access token,
// capturing the manifest scope explicitly. Returns the token string on
// success or null when no interactive channel / stored credential is
// available (so the caller can fall back gracefully). This helper is
// also the seam tests mock by installing a stub `getAccessToken` symbol.
function tryGetAccessToken() {
  if (typeof getAccessToken !== "function") return null;
  try {
    var tok = getAccessToken({
      scopes: ["https://www.googleapis.com/auth/drive.readonly"],
    });
    return tok || null;
  } catch (e) {
    print("[gdrive] authentication required: " + e);
    return null;
  }
}

// -- Entry point ------------------------------------------------------

function extract(url) {
  var fileId = extractFileId(url);
  if (!fileId) {
    print("[gdrive] could not find file ID in URL:", url);
    return url;
  }

  var kind = classifyUrl(url);

  if (kind === "folder") {
    print("[gdrive] folder downloads are not supported; " +
      "please pick individual files from the folder");
    return url;
  }

  var docUrl = docExportUrl(kind, fileId);
  if (docUrl) return docUrl;

  var tryUrl = ucDownloadUrl(fileId);
  var resp = probe(tryUrl);

  // Network failure: fall back to the best guess and let warpdl
  // surface the error to the user.
  if (!resp) return tryUrl;

  // Private-file path: Google returns 401/403 on the public uc endpoint
  // for files whose ACL is not "anyone with the link". When warpdl has
  // an OAuth token for this plugin we can fetch the bytes through the
  // Drive API v3 media endpoint instead.
  if (resp.status_code === 401 || resp.status_code === 403) {
    var tok = tryGetAccessToken();
    if (tok) {
      return {
        url: driveApiDownloadUrl(fileId),
        headers: { "Authorization": "Bearer " + tok },
      };
    }
    // No token available — surface the original URL so warpdl's
    // downloader returns the 401/403 and the CLI can prompt for login.
    print("[gdrive] private file but no OAuth token; returning original URL");
    return url;
  }

  // Non-2xx: surface the user-facing URL; warpdl will show the
  // HTTP error.
  if (resp.status_code && resp.status_code >= 400) {
    print("[gdrive] probe returned HTTP", resp.status_code);
    return tryUrl;
  }

  // Small file path: Content-Disposition means Google is streaming
  // the file already, so the tryUrl is the right final URL.
  if (hasContentDisposition(resp)) return tryUrl;

  // Large file path: expect an HTML warning page with a form we can
  // re-submit to get the real download.
  if (looksLikeHtml(resp.body)) {
    var form = parseForm(resp.body);
    if (form) return buildQuery(form.action, form.params);
    print("[gdrive] could not find download form on warning page");
  }

  // Last-ditch fallback: hand the uc URL to warpdl.
  return tryUrl;
}
