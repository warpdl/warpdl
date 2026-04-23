// Package gdrive_test exercises the Google Drive warpdl plugin by
// loading main.js into an isolated goja runtime, stubbing out the
// global `request` function that the plugin depends on, and asserting
// `extract()` returns the right URL for every shape a naive user might
// paste.
package gdrive_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dop251/goja"
)

// stubResponse is the shape the plugin expects back from the injected
// `request` global: keys are the snake_case fields used in real
// request.js, and `headers` is a goja.Value that responds to .get(key).
type stubResponse struct {
	StatusCode    int
	Body          string
	ContentLength int64
	Headers       map[string]string // nil = "headers field absent"
}

// testRuntime builds a goja runtime that matches the globals the real
// engine provides: print, and a stub `request` function that hands back
// a response whose `.headers.get(name)` works (mimicking the Headers JS
// class the real engine installs).
func testRuntime(t *testing.T, handler func(req map[string]any) *stubResponse) *goja.Runtime {
	t.Helper()

	rt := goja.New()

	mustSet(t, rt, "print", func(call goja.FunctionCall) goja.Value {
		return goja.Undefined()
	})

	mustSet(t, rt, "request", func(call goja.FunctionCall) goja.Value {
		var req map[string]any
		if len(call.Arguments) > 0 {
			if err := rt.ExportTo(call.Arguments[0], &req); err != nil {
				t.Fatalf("request: bad argument: %v", err)
			}
		}
		resp := handler(req)
		if resp == nil {
			return goja.Null()
		}

		out := rt.NewObject()
		_ = out.Set("status_code", resp.StatusCode)
		_ = out.Set("body", resp.Body)
		_ = out.Set("content_length", resp.ContentLength)

		if resp.Headers != nil {
			// Install a `headers` value that exposes `.get(name)` and a
			// `has(name)` method, matching the JS Headers class the real
			// engine bootstraps.
			//
			// We do NOT attach a `.get` when `Headers` is nil - the
			// plugin's hasContentDisposition helper must cope with that
			// absence.
			headers := rt.NewObject()
			m := resp.Headers
			_ = headers.Set("get", func(call goja.FunctionCall) goja.Value {
				name := call.Argument(0).String()
				return rt.ToValue(m[name])
			})
			_ = headers.Set("has", func(call goja.FunctionCall) goja.Value {
				_, ok := m[call.Argument(0).String()]
				return rt.ToValue(ok)
			})
			_ = out.Set("headers", headers)
		}
		return out
	})

	// Load the plugin source.
	srcPath := pluginSourcePath(t, "main.js")
	src, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read main.js: %v", err)
	}
	if _, err := rt.RunString(string(src)); err != nil {
		t.Fatalf("run main.js: %v", err)
	}
	return rt
}

func pluginSourcePath(t *testing.T, name string) string {
	t.Helper()
	// The test is colocated with main.js.
	abs, err := filepath.Abs(name)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return abs
}

func mustSet(t *testing.T, rt *goja.Runtime, name string, v any) {
	t.Helper()
	if err := rt.Set(name, v); err != nil {
		t.Fatalf("set %s: %v", name, err)
	}
}

// runExtract invokes extract(url) on the runtime and returns the result
// as a string. Panics are converted to test failures.
func runExtract(t *testing.T, rt *goja.Runtime, url string) string {
	t.Helper()
	// Use a binding argument rather than string-interpolation to keep
	// the test immune to special characters in the URL.
	var fn func(string) string
	if err := rt.ExportTo(rt.Get("extract"), &fn); err != nil {
		t.Fatalf("export extract: %v", err)
	}
	return fn(url)
}

// runExtractFileId invokes the helper function directly.
func runExtractFileId(t *testing.T, rt *goja.Runtime, url string) string {
	t.Helper()
	var fn func(string) goja.Value
	if err := rt.ExportTo(rt.Get("extractFileId"), &fn); err != nil {
		t.Fatalf("export extractFileId: %v", err)
	}
	v := fn(url)
	if v == nil || goja.IsNull(v) || goja.IsUndefined(v) {
		return ""
	}
	return v.String()
}

// ---------------------------------------------------------------------
// ID extraction — no network
// ---------------------------------------------------------------------

func TestExtractFileId_AllUrlShapes(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want string
	}{
		{
			"canonical share link",
			"https://drive.google.com/file/d/1AbCdEf123XYZ/view?usp=sharing",
			"1AbCdEf123XYZ",
		},
		{
			"open with id query param",
			"https://drive.google.com/open?id=AAAbbbCCCdddEEEfffGGG",
			"AAAbbbCCCdddEEEfffGGG",
		},
		{
			"uc export link",
			"https://drive.google.com/uc?export=download&id=XYZ-_abc123",
			"XYZ-_abc123",
		},
		{
			"doc edit URL",
			"https://docs.google.com/document/d/docID123_abc-def/edit",
			"docID123_abc-def",
		},
		{
			"spreadsheet URL",
			"https://docs.google.com/spreadsheets/d/sheetID99/edit#gid=0",
			"sheetID99",
		},
		{
			"presentation URL",
			"https://docs.google.com/presentation/d/slidesID77/edit",
			"slidesID77",
		},
		{
			"folder URL",
			"https://drive.google.com/drive/folders/folder-id-xyz_123",
			"folder-id-xyz_123",
		},
		{
			"file link with drive_link flag",
			"https://drive.google.com/file/d/abc123DEF/view?usp=drive_link",
			"abc123DEF",
		},
		{
			"file link with preview",
			"https://drive.google.com/file/d/abc_def-ghi/preview",
			"abc_def-ghi",
		},
	}

	rt := testRuntime(t, func(req map[string]any) *stubResponse {
		t.Fatalf("request should not be called for id extraction; got %v", req)
		return nil
	})

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := runExtractFileId(t, rt, tc.url)
			if got != tc.want {
				t.Fatalf("extractFileId(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}

func TestExtractFileId_NoMatch(t *testing.T) {
	rt := testRuntime(t, func(req map[string]any) *stubResponse {
		t.Fatalf("request should not be called")
		return nil
	})
	got := runExtractFileId(t, rt, "https://example.com/totally-unrelated")
	if got != "" {
		t.Fatalf("expected empty id for non-drive URL, got %q", got)
	}
}

func TestExtractFileId_EmptyAndWeirdInputs(t *testing.T) {
	rt := testRuntime(t, func(req map[string]any) *stubResponse { return nil })

	for _, url := range []string{
		"",
		"not-even-a-url",
		"https://drive.google.com/",
		"https://drive.google.com/file/d/",       // id missing
		"https://drive.google.com/file/d//view",   // empty id
	} {
		t.Run(url, func(t *testing.T) {
			got := runExtractFileId(t, rt, url)
			if got != "" {
				t.Fatalf("extractFileId(%q) should be empty, got %q", url, got)
			}
		})
	}
}

// ---------------------------------------------------------------------
// Native Google Docs / Sheets / Slides export URLs
// ---------------------------------------------------------------------

func TestExtract_DocsExportUrls(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{
			"https://docs.google.com/document/d/DID/edit",
			"https://docs.google.com/document/d/DID/export?format=pdf",
		},
		{
			"https://docs.google.com/spreadsheets/d/SID/edit#gid=0",
			"https://docs.google.com/spreadsheets/d/SID/export?format=xlsx",
		},
		{
			"https://docs.google.com/presentation/d/PID/edit",
			"https://docs.google.com/presentation/d/PID/export?format=pptx",
		},
	}

	rt := testRuntime(t, func(req map[string]any) *stubResponse {
		t.Fatalf("docs path must not hit network, request: %v", req)
		return nil
	})

	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			got := runExtract(t, rt, tc.url)
			if got != tc.want {
				t.Fatalf("extract(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}

// Folder URL: plugin must NOT hit the network and must return URL as-is
// (with a printed warning).
func TestExtract_FolderReturnsOriginalUrl(t *testing.T) {
	folder := "https://drive.google.com/drive/folders/folder-id-xyz_123"
	rt := testRuntime(t, func(req map[string]any) *stubResponse {
		t.Fatalf("folder path must not hit network, request: %v", req)
		return nil
	})
	got := runExtract(t, rt, folder)
	if got != folder {
		t.Fatalf("folder extract should return input URL, got %q", got)
	}
}

// URL that doesn't match any Drive pattern should be returned as-is
// without hitting the network.
func TestExtract_UnrelatedUrlPassesThrough(t *testing.T) {
	rt := testRuntime(t, func(req map[string]any) *stubResponse {
		t.Fatalf("extract should not request for unrelated URLs")
		return nil
	})
	in := "https://example.com/something.zip"
	got := runExtract(t, rt, in)
	if got != in {
		t.Fatalf("expected passthrough, got %q", got)
	}
}

// ---------------------------------------------------------------------
// Binary file flows (network path mocked)
// ---------------------------------------------------------------------

func TestExtract_SmallFileReturnsUcUrl(t *testing.T) {
	const fileID = "smallFileID123"
	want := "https://drive.google.com/uc?export=download&id=" + fileID + "&confirm=t"

	rt := testRuntime(t, func(req map[string]any) *stubResponse {
		if got := req["url"]; got != want {
			t.Fatalf("probe URL = %v, want %v", got, want)
		}
		return &stubResponse{
			StatusCode:    200,
			Body:          "binary-content-here",
			ContentLength: 12345,
			Headers: map[string]string{
				"Content-Disposition": "attachment; filename=\"file.bin\"",
			},
		}
	})

	in := "https://drive.google.com/file/d/" + fileID + "/view?usp=sharing"
	got := runExtract(t, rt, in)
	if got != want {
		t.Fatalf("extract(%q) = %q, want %q", in, got, want)
	}
}

func TestExtract_LargeFileParsesVirusWarningForm(t *testing.T) {
	const fileID = "largeFileID-abc_def"
	const uuid = "uuid-abc-123-XYZ"
	const at = "APZUnXXXX:9999999999"

	html := `<!DOCTYPE html>
<html><body>
<div>Google Drive can't scan this file for viruses.</div>
<form id="download-form" action="https://drive.usercontent.google.com/download" method="get">
<input type="hidden" name="id" value="` + fileID + `">
<input type="hidden" name="export" value="download">
<input type="hidden" name="authuser" value="0">
<input type="hidden" name="confirm" value="t">
<input type="hidden" name="uuid" value="` + uuid + `">
<input type="hidden" name="at" value="` + at + `">
<button type="submit">Download anyway</button>
</form>
</body></html>`

	rt := testRuntime(t, func(req map[string]any) *stubResponse {
		return &stubResponse{
			StatusCode:    200,
			Body:          html,
			ContentLength: int64(len(html)),
			Headers:       map[string]string{},
		}
	})

	in := "https://drive.google.com/file/d/" + fileID + "/view"
	got := runExtract(t, rt, in)

	// We don't assert on exact query-string ordering since Go's map
	// iteration order is randomised. Assert on required components.
	if !strings.HasPrefix(got, "https://drive.usercontent.google.com/download?") {
		t.Fatalf("expected drive.usercontent host, got %q", got)
	}
	for _, want := range []string{
		"id=" + fileID,
		"export=download",
		"authuser=0",
		"confirm=t",
		"uuid=" + uuid,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in final URL; got %q", want, got)
		}
	}
	// 'at' contains a colon which must be percent-encoded.
	if !strings.Contains(got, "at=APZUnXXXX%3A9999999999") {
		t.Fatalf("expected properly-encoded at=; got %q", got)
	}
}

// Single-quoted attribute variant (Google has rotated markup before).
func TestExtract_LargeFileParsesSingleQuotedForm(t *testing.T) {
	const fileID = "FID123"

	html := `<!doctype html><html><body>
<form id='download-form' method='get' action='https://drive.usercontent.google.com/download'>
<input type='hidden' name='id' value='` + fileID + `'/>
<input type='hidden' name='export' value='download'/>
<input type='hidden' name='confirm' value='t'/>
<input type='hidden' name='uuid' value='uuid-single-quoted'/>
</form></body></html>`

	rt := testRuntime(t, func(req map[string]any) *stubResponse {
		return &stubResponse{
			StatusCode:    200,
			Body:          html,
			ContentLength: int64(len(html)),
			Headers:       map[string]string{},
		}
	})

	got := runExtract(t, rt, "https://drive.google.com/file/d/"+fileID+"/view")
	if !strings.Contains(got, "uuid=uuid-single-quoted") {
		t.Fatalf("single-quoted attributes not parsed; got %q", got)
	}
}

// No form at all in the body: fall back to the uc URL.
func TestExtract_HtmlWithoutFormFallsBack(t *testing.T) {
	const fileID = "WeirdID"
	want := "https://drive.google.com/uc?export=download&id=" + fileID + "&confirm=t"

	rt := testRuntime(t, func(req map[string]any) *stubResponse {
		return &stubResponse{
			StatusCode:    200,
			Body:          "<!doctype html><html><body>something else</body></html>",
			ContentLength: 50,
			Headers:       map[string]string{},
		}
	})

	got := runExtract(t, rt, "https://drive.google.com/file/d/"+fileID+"/view")
	if got != want {
		t.Fatalf("extract fallback = %q, want %q", got, want)
	}
}

// 4xx from Google: surface the uc URL so warpdl's downloader produces
// the user-visible error.
func TestExtract_HttpErrorFallsBack(t *testing.T) {
	const fileID = "PrivateFileID"
	want := "https://drive.google.com/uc?export=download&id=" + fileID + "&confirm=t"

	rt := testRuntime(t, func(req map[string]any) *stubResponse {
		return &stubResponse{
			StatusCode:    404,
			Body:          "Not Found",
			ContentLength: 9,
			Headers:       map[string]string{},
		}
	})

	got := runExtract(t, rt, "https://drive.google.com/file/d/"+fileID+"/view")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Network failure: request returns nil, plugin must not panic and must
// return the uc URL.
func TestExtract_RequestReturnsNilDoesNotCrash(t *testing.T) {
	const fileID = "NetErrFileID"
	want := "https://drive.google.com/uc?export=download&id=" + fileID + "&confirm=t"

	rt := testRuntime(t, func(req map[string]any) *stubResponse {
		return nil
	})

	got := runExtract(t, rt, "https://drive.google.com/file/d/"+fileID+"/view")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// Response missing the headers field entirely: plugin must not panic.
func TestExtract_ResponseWithoutHeaders(t *testing.T) {
	const fileID = "NoHeaderFileID"

	rt := testRuntime(t, func(req map[string]any) *stubResponse {
		return &stubResponse{
			StatusCode:    200,
			Body:          "some body",
			ContentLength: 9,
			// Headers deliberately nil — plugin must cope.
		}
	})

	got := runExtract(t, rt, "https://drive.google.com/file/d/"+fileID+"/view")
	if !strings.Contains(got, fileID) {
		t.Fatalf("expected result to still reference fileID, got %q", got)
	}
}

// Ensure the plugin's network request sets the User-Agent. Google
// sometimes returns different markup for empty UAs, so this is
// important.
func TestExtract_SendsUserAgent(t *testing.T) {
	const fileID = "UA_TEST_ID"

	var capturedUA string
	rt := testRuntime(t, func(req map[string]any) *stubResponse {
		if headers, ok := req["headers"].(map[string]any); ok {
			if ua, ok := headers["User-Agent"].(string); ok {
				capturedUA = ua
			}
		}
		return &stubResponse{
			StatusCode:    200,
			Body:          "x",
			ContentLength: 1,
			Headers:       map[string]string{
				"Content-Disposition": "attachment; filename=x",
			},
		}
	})

	_ = runExtract(t, rt, "https://drive.google.com/file/d/"+fileID+"/view")

	if capturedUA == "" {
		t.Fatalf("plugin did not send a User-Agent header")
	}
}

// Extremely long HTML with many nested attributes: the plugin must still
// find the form and not take exponential time or allocate wildly.
func TestExtract_LongHtmlWithMisleadingContent(t *testing.T) {
	const fileID = "ID_long_html"

	var sb strings.Builder
	sb.WriteString("<!doctype html><html><body>\n")
	// Lots of unrelated <input> elements before the real form.
	for i := 0; i < 500; i++ {
		sb.WriteString(`<input type="text" name="nope" value="noise">`)
	}
	// A misleading form that is NOT id="download-form".
	sb.WriteString(`<form id="other-form" action="https://fake.example.com/"><input name="id" value="WRONG"></form>`)
	// The real form.
	sb.WriteString(`<form id="download-form" action="https://drive.usercontent.google.com/download" method="get">`)
	sb.WriteString(`<input type="hidden" name="id" value="` + fileID + `">`)
	sb.WriteString(`<input type="hidden" name="export" value="download">`)
	sb.WriteString(`<input type="hidden" name="confirm" value="t">`)
	sb.WriteString(`<input type="hidden" name="uuid" value="U">`)
	sb.WriteString(`</form>`)
	sb.WriteString("</body></html>")

	rt := testRuntime(t, func(req map[string]any) *stubResponse {
		return &stubResponse{
			StatusCode:    200,
			Body:          sb.String(),
			ContentLength: int64(sb.Len()),
			Headers:       map[string]string{},
		}
	})

	got := runExtract(t, rt, "https://drive.google.com/file/d/"+fileID+"/view")
	if !strings.HasPrefix(got, "https://drive.usercontent.google.com/download?") {
		t.Fatalf("expected usercontent host, got %q", got)
	}
	if strings.Contains(got, "id=WRONG") {
		t.Fatalf("plugin picked up the misleading earlier form: %q", got)
	}
	if !strings.Contains(got, "id="+fileID) {
		t.Fatalf("real fileID missing from final URL: %q", got)
	}
}

// Manifest sanity: makes sure manifest.json stays in sync with the
// plugin's intended match set. This also protects against typos that
// would prevent the engine from picking up the plugin at all.
func TestManifestMatchesDriveUrls(t *testing.T) {
	data, err := os.ReadFile(pluginSourcePath(t, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if !strings.Contains(string(data), `"entrypoint": "main.js"`) {
		t.Fatalf("manifest should declare main.js entrypoint; got:\n%s", data)
	}
	if !strings.Contains(string(data), "drive|docs") {
		t.Fatalf("manifest should match drive/docs URLs; got:\n%s", data)
	}
}

// ---------------------------------------------------------------------
// v2 — OAuth / private-file path
// ---------------------------------------------------------------------

// runExtractValue is a variant of runExtract that returns the raw
// goja.Value so the caller can distinguish a plain string from an
// {url, headers} object.
func runExtractValue(t *testing.T, rt *goja.Runtime, url string) goja.Value {
	t.Helper()
	var fn func(string) goja.Value
	if err := rt.ExportTo(rt.Get("extract"), &fn); err != nil {
		t.Fatalf("export extract: %v", err)
	}
	return fn(url)
}

// TestExtract_PrivateFile_UsesDriveApi covers the central v2 flow: when
// the public uc probe comes back 403 (private file), and the host engine
// provides an OAuth token, the plugin returns the authenticated Drive
// API v3 media URL with a Bearer header.
func TestExtract_PrivateFile_UsesDriveApi(t *testing.T) {
	const fileID = "PRIVATE_FILE_ID"
	const token = "TESTTOK"

	var tokenCalls int
	var capturedOpts map[string]any

	rt := testRuntime(t, func(req map[string]any) *stubResponse {
		// Probe returns a classic "private file" 403.
		return &stubResponse{
			StatusCode:    403,
			Body:          "Forbidden",
			ContentLength: 9,
			Headers:       map[string]string{},
		}
	})

	mustSet(t, rt, "getAccessToken", func(call goja.FunctionCall) goja.Value {
		tokenCalls++
		if len(call.Arguments) > 0 {
			_ = rt.ExportTo(call.Arguments[0], &capturedOpts)
		}
		return rt.ToValue(token)
	})

	v := runExtractValue(t, rt, "https://drive.google.com/file/d/"+fileID+"/view")
	m, ok := v.Export().(map[string]any)
	if !ok {
		t.Fatalf("private-file extract must return an object, got %T: %v",
			v.Export(), v.Export())
	}

	gotURL, _ := m["url"].(string)
	wantURL := "https://www.googleapis.com/drive/v3/files/" + fileID + "?alt=media"
	if gotURL != wantURL {
		t.Fatalf("url = %q, want %q", gotURL, wantURL)
	}

	headers, ok := m["headers"].(map[string]any)
	if !ok {
		t.Fatalf("expected headers object, got %T: %v", m["headers"], m["headers"])
	}
	if auth, _ := headers["Authorization"].(string); auth != "Bearer "+token {
		t.Fatalf("Authorization = %q, want %q", auth, "Bearer "+token)
	}

	if tokenCalls != 1 {
		t.Fatalf("expected exactly one getAccessToken call, got %d", tokenCalls)
	}
	// The plugin must request the manifest scope explicitly so the host
	// engine can reject anything out of bounds.
	scopes, _ := capturedOpts["scopes"].([]any)
	if len(scopes) != 1 {
		t.Fatalf("expected exactly one scope requested, got %v", scopes)
	}
	if s, _ := scopes[0].(string); s != "https://www.googleapis.com/auth/drive.readonly" {
		t.Fatalf("scope = %q, want drive.readonly", s)
	}
}

// TestExtract_PrivateFile_OnProbe401 covers the sibling case where the
// host returns 401 (missing auth) rather than 403 (forbidden). Behavior
// must be identical.
func TestExtract_PrivateFile_OnProbe401(t *testing.T) {
	const fileID = "PRIVATE_401"
	const token = "TOK401"

	rt := testRuntime(t, func(req map[string]any) *stubResponse {
		return &stubResponse{
			StatusCode:    401,
			Body:          "Unauthorized",
			ContentLength: 12,
			Headers:       map[string]string{},
		}
	})
	mustSet(t, rt, "getAccessToken", func(call goja.FunctionCall) goja.Value {
		return rt.ToValue(token)
	})

	v := runExtractValue(t, rt, "https://drive.google.com/file/d/"+fileID+"/view")
	m, ok := v.Export().(map[string]any)
	if !ok {
		t.Fatalf("401 branch must return object, got %T", v.Export())
	}
	if gotURL, _ := m["url"].(string); !strings.Contains(gotURL, fileID) {
		t.Fatalf("expected Drive API URL with %s, got %q", fileID, gotURL)
	}
}

// TestExtract_PrivateFile_NoTokenReturnsOriginal: if the host engine
// cannot produce a token (no `getAccessToken` binding is registered at
// all, e.g. tokens==nil path), the plugin must not panic and must
// return the original URL so warpdl surfaces the original 401/403 to
// the user.
func TestExtract_PrivateFile_NoTokenReturnsOriginal(t *testing.T) {
	const fileID = "NOAUTH_ID"
	in := "https://drive.google.com/file/d/" + fileID + "/view"

	rt := testRuntime(t, func(req map[string]any) *stubResponse {
		return &stubResponse{
			StatusCode:    403,
			Body:          "Forbidden",
			ContentLength: 9,
			Headers:       map[string]string{},
		}
	})
	// Deliberately do NOT install getAccessToken — the plugin must
	// tolerate its absence.

	got := runExtract(t, rt, in)
	if got != in {
		t.Fatalf("no-token private file should return original URL, got %q", got)
	}
}

// TestExtract_PrivateFile_TokenBindingThrowsFallsBack: if the host
// engine's getAccessToken binding throws (e.g. scope not declared in
// manifest, auth cancelled, auth timeout), the plugin must catch the
// exception and fall back to the original URL rather than propagate
// the throw out of extract().
func TestExtract_PrivateFile_TokenBindingThrowsFallsBack(t *testing.T) {
	const fileID = "SCOPE_DENIED"
	in := "https://drive.google.com/file/d/" + fileID + "/view"

	rt := testRuntime(t, func(req map[string]any) *stubResponse {
		return &stubResponse{
			StatusCode:    403,
			Body:          "Forbidden",
			ContentLength: 9,
			Headers:       map[string]string{},
		}
	})
	// Stub that mimics what the real engine binding does when a plugin
	// asks for a scope not declared in the manifest: throw. The real
	// binding uses panic(rt.NewGoError(...)) so that's what we do too.
	mustSet(t, rt, "getAccessToken", func(call goja.FunctionCall) goja.Value {
		panic(rt.NewGoError(errors.New("scope \"drive.full\" not declared in manifest")))
	})

	got := runExtract(t, rt, in)
	if got != in {
		t.Fatalf("expected fallback to original URL, got %q", got)
	}
}

// TestExtract_PrivateFile_TokenBindingReturnsEmpty: an empty token is
// treated the same as no token (fall back to original URL).
func TestExtract_PrivateFile_TokenBindingReturnsEmpty(t *testing.T) {
	const fileID = "EMPTY_TOK"
	in := "https://drive.google.com/file/d/" + fileID + "/view"

	rt := testRuntime(t, func(req map[string]any) *stubResponse {
		return &stubResponse{
			StatusCode:    403,
			Body:          "Forbidden",
			ContentLength: 9,
			Headers:       map[string]string{},
		}
	})
	mustSet(t, rt, "getAccessToken", func(call goja.FunctionCall) goja.Value {
		return rt.ToValue("")
	})

	got := runExtract(t, rt, in)
	if got != in {
		t.Fatalf("empty token should fall back to original URL, got %q", got)
	}
}

// TestExtract_PublicFile_DoesNotCallGetAccessToken makes sure the OAuth
// path is dormant for public files: a 200 with Content-Disposition must
// resolve purely through the v1 uc URL flow and never reach getAccessToken.
func TestExtract_PublicFile_DoesNotCallGetAccessToken(t *testing.T) {
	const fileID = "PUBLIC_ID"
	rt := testRuntime(t, func(req map[string]any) *stubResponse {
		return &stubResponse{
			StatusCode:    200,
			Body:          "x",
			ContentLength: 1,
			Headers: map[string]string{
				"Content-Disposition": "attachment; filename=\"public.bin\"",
			},
		}
	})
	mustSet(t, rt, "getAccessToken", func(call goja.FunctionCall) goja.Value {
		t.Fatalf("public-file path must not consult getAccessToken")
		return goja.Undefined()
	})

	got := runExtract(t, rt, "https://drive.google.com/file/d/"+fileID+"/view")
	want := "https://drive.google.com/uc?export=download&id=" + fileID + "&confirm=t"
	if got != want {
		t.Fatalf("extract = %q, want %q", got, want)
	}
}

// TestExtract_HttpError_404_StillFallsBack: regression guard. Earlier
// code surfaced all 4xx the same way; after v2 only 401/403 take the
// OAuth branch, so 404 must still fall back to the uc URL untouched.
func TestExtract_HttpError_404_StillFallsBack(t *testing.T) {
	const fileID = "NOTFOUND"
	want := "https://drive.google.com/uc?export=download&id=" + fileID + "&confirm=t"

	rt := testRuntime(t, func(req map[string]any) *stubResponse {
		return &stubResponse{
			StatusCode:    404,
			Body:          "Not Found",
			ContentLength: 9,
			Headers:       map[string]string{},
		}
	})
	mustSet(t, rt, "getAccessToken", func(call goja.FunctionCall) goja.Value {
		t.Fatalf("404 path must not consult getAccessToken")
		return goja.Undefined()
	})

	got := runExtract(t, rt, "https://drive.google.com/file/d/"+fileID+"/view")
	if got != want {
		t.Fatalf("extract = %q, want %q", got, want)
	}
}

// TestManifestV2Shape verifies the manifest declares the v2 auth block
// exactly as the engine's OpenModule + NormalizeOAuth2Config validator
// expects. Any drift here (missing scope, http:// URL, unknown pkce
// method) would fail `warp ext install` silently — the integration test
// catches the common cases but this test documents the exact shape.
func TestManifestV2Shape(t *testing.T) {
	data, err := os.ReadFile(pluginSourcePath(t, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}

	var m struct {
		Name       string   `json:"name"`
		Version    string   `json:"version"`
		Entrypoint string   `json:"entrypoint"`
		Matches    []string `json:"matches"`
		Auth       *struct {
			Type            string            `json:"type"`
			ClientID        string            `json:"client_id"`
			ClientSecret    string            `json:"client_secret,omitempty"`
			Scopes          []string          `json:"scopes"`
			AuthorizeURL    string            `json:"authorize_url"`
			TokenURL        string            `json:"token_url"`
			DeviceURL       string            `json:"device_url,omitempty"`
			RevokeURL       string            `json:"revoke_url,omitempty"`
			PKCE            string            `json:"pkce,omitempty"`
			ExtraAuthParams map[string]string `json:"extra_auth_params,omitempty"`
		} `json:"auth,omitempty"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}

	if m.Version != "2.0.0" {
		t.Fatalf("version = %q, want 2.0.0", m.Version)
	}
	if m.Auth == nil {
		t.Fatal("auth block missing")
	}
	if m.Auth.Type != "oauth2" {
		t.Fatalf("auth.type = %q, want oauth2", m.Auth.Type)
	}
	if m.Auth.ClientSecret != "" {
		t.Fatalf("auth.client_secret must NOT be set for PKCE public clients; got %q",
			m.Auth.ClientSecret)
	}
	if m.Auth.ClientID == "" {
		t.Fatal("auth.client_id must be set")
	}
	wantScope := "https://www.googleapis.com/auth/drive.readonly"
	if len(m.Auth.Scopes) == 0 {
		t.Fatal("auth.scopes must be non-empty")
	}
	found := false
	for _, s := range m.Auth.Scopes {
		if s == wantScope {
			found = true
		}
	}
	if !found {
		t.Fatalf("auth.scopes must include %q; got %v", wantScope, m.Auth.Scopes)
	}
	for _, u := range []string{m.Auth.AuthorizeURL, m.Auth.TokenURL} {
		if !strings.HasPrefix(u, "https://") {
			t.Fatalf("auth URL %q must be https://", u)
		}
	}
	if m.Auth.PKCE != "" && m.Auth.PKCE != "S256" && m.Auth.PKCE != "plain" {
		t.Fatalf("auth.pkce = %q, want S256 or plain", m.Auth.PKCE)
	}
}
