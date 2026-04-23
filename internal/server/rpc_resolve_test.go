package server

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/creachadair/jrpc2"
	"github.com/warpdl/warpdl/common"
)

// Minimal yt-dlp-shaped JSON fixture covering the fields we consume.
const sampleYtdlpJSON = `{
  "title": "Test Video",
  "uploader": "Test Channel",
  "channel": "Test Channel",
  "duration": 125.4,
  "formats": [
    {
      "format_id": "18",
      "url": "https://example.com/video18.mp4?sig=abc",
      "ext": "mp4",
      "protocol": "https",
      "vcodec": "avc1.42001E",
      "acodec": "mp4a.40.2",
      "height": 360,
      "width": 640,
      "fps": 30,
      "filesize": 1234567,
      "format_note": "360p",
      "mime_type": "video/mp4"
    },
    {
      "format_id": "137",
      "url": "https://example.com/video137.mp4?sig=xyz",
      "ext": "mp4",
      "protocol": "https",
      "vcodec": "avc1.640028",
      "acodec": "none",
      "height": 1080,
      "width": 1920,
      "fps": 60,
      "filesize_approx": 48765432,
      "format_note": "1080p60"
    },
    {
      "format_id": "251",
      "url": "https://example.com/audio251.webm",
      "ext": "webm",
      "protocol": "https",
      "vcodec": "none",
      "acodec": "opus",
      "abr": 160,
      "filesize": 2500000,
      "format_note": "medium"
    },
    {
      "format_id": "hls-1",
      "url": "https://example.com/manifest.m3u8",
      "ext": "mp4",
      "protocol": "m3u8_native",
      "vcodec": "avc1.64001F",
      "acodec": "mp4a.40.2",
      "height": 720
    },
    {
      "format_id": "empty-codecs",
      "url": "https://example.com/no-codecs.mp4",
      "ext": "mp4",
      "protocol": "https",
      "vcodec": "none",
      "acodec": "none"
    }
  ]
}`

func TestParseYtdlpOutput_Basic(t *testing.T) {
	result, err := parseYtdlpOutput([]byte(sampleYtdlpJSON))
	if err != nil {
		t.Fatalf("parseYtdlpOutput: %v", err)
	}

	if result.Title != "Test Video" {
		t.Errorf("Title = %q, want %q", result.Title, "Test Video")
	}
	if result.Author != "Test Channel" {
		t.Errorf("Author = %q, want %q", result.Author, "Test Channel")
	}
	if result.Duration != 125 {
		t.Errorf("Duration = %d, want 125", result.Duration)
	}

	// HLS manifest + empty-codecs entries are filtered out; 3 remain.
	if len(result.Formats) != 3 {
		t.Fatalf("got %d formats, want 3 (18, 137, 251)", len(result.Formats))
	}

	byID := map[string]common.ResolvedFormat{}
	for _, f := range result.Formats {
		byID[f.FormatID] = f
	}

	f18, ok := byID["18"]
	if !ok {
		t.Fatal("missing format 18")
	}
	if !f18.HasVideo || !f18.HasAudio {
		t.Errorf("format 18: HasVideo=%v HasAudio=%v, want both true", f18.HasVideo, f18.HasAudio)
	}
	if f18.FileSize != 1234567 {
		t.Errorf("format 18: FileSize=%d, want 1234567", f18.FileSize)
	}
	if f18.Quality != "360p" {
		t.Errorf("format 18: Quality=%q, want 360p", f18.Quality)
	}
	if f18.MimeType != "video/mp4" {
		t.Errorf("format 18: MimeType=%q", f18.MimeType)
	}

	f137 := byID["137"]
	if !f137.HasVideo || f137.HasAudio {
		t.Errorf("format 137: HasVideo=%v HasAudio=%v, want video only", f137.HasVideo, f137.HasAudio)
	}
	if f137.FileSize != 48765432 {
		t.Errorf("format 137: FileSize=%d, want 48765432 (filesize_approx fallback)", f137.FileSize)
	}
	if f137.Height != 1080 || f137.Width != 1920 || f137.Fps != 60 {
		t.Errorf("format 137 dims: %dx%d@%dfps", f137.Width, f137.Height, f137.Fps)
	}

	f251 := byID["251"]
	if f251.HasVideo || !f251.HasAudio {
		t.Errorf("format 251: HasVideo=%v HasAudio=%v, want audio only", f251.HasVideo, f251.HasAudio)
	}
	if f251.AudioBitrate != 160 {
		t.Errorf("format 251: AudioBitrate=%d, want 160", f251.AudioBitrate)
	}
}

func TestParseYtdlpOutput_Empty(t *testing.T) {
	_, err := parseYtdlpOutput([]byte(""))
	if err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestParseYtdlpOutput_InvalidJSON(t *testing.T) {
	_, err := parseYtdlpOutput([]byte("not json"))
	if err == nil {
		t.Fatal("expected json decode error")
	}
}

func TestParseYtdlpOutput_NoFormats(t *testing.T) {
	result, err := parseYtdlpOutput([]byte(`{"title":"T","formats":[]}`))
	if err != nil {
		t.Fatalf("parseYtdlpOutput: %v", err)
	}
	if len(result.Formats) != 0 {
		t.Errorf("expected 0 formats, got %d", len(result.Formats))
	}
}

func TestDeriveQualityLabel(t *testing.T) {
	tests := []struct {
		name string
		in   ytdlpFormat
		want string
	}{
		{"note wins", ytdlpFormat{FormatNote: "720p60", Height: 720}, "720p60"},
		{"height fallback", ytdlpFormat{Height: 1080}, "1080p"},
		{"abr fallback", ytdlpFormat{ABR: 128}, "128 kbps"},
		{"nothing", ytdlpFormat{}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := deriveQualityLabel(tc.in); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsManifestProtocol(t *testing.T) {
	manifests := []string{"m3u8", "m3u8_native", "http_dash_segments", "dash", "M3U8"}
	for _, p := range manifests {
		if !isManifestProtocol(p) {
			t.Errorf("isManifestProtocol(%q) = false, want true", p)
		}
	}
	nonManifests := []string{"https", "http", "", "ftp"}
	for _, p := range nonManifests {
		if isManifestProtocol(p) {
			t.Errorf("isManifestProtocol(%q) = true, want false", p)
		}
	}
}

func TestTailString(t *testing.T) {
	if got := tailString("short", 10); got != "short" {
		t.Errorf("short tail: got %q", got)
	}
	if got := tailString("0123456789abcdef", 4); got != "...cdef" {
		t.Errorf("long tail: got %q", got)
	}
	if got := tailString("  whitespace  ", 20); got != "whitespace" {
		t.Errorf("trimmed: got %q", got)
	}
}

func TestLimitedWriter(t *testing.T) {
	var out [10]byte
	lw := &limitedWriter{w: &captureWriter{buf: out[:0]}, remaining: 5}
	n, _ := lw.Write([]byte("abc"))
	if n != 3 {
		t.Errorf("first write: n=%d, want 3", n)
	}
	if lw.remaining != 2 {
		t.Errorf("remaining=%d, want 2", lw.remaining)
	}
	n, _ = lw.Write([]byte("defgh"))
	if n != 2 {
		t.Errorf("second write: n=%d, want 2 (cap)", n)
	}
	if lw.remaining != 0 {
		t.Errorf("remaining=%d, want 0", lw.remaining)
	}
	// Further writes are silently dropped.
	n, _ = lw.Write([]byte("xyz"))
	if n != 3 {
		t.Errorf("overflow write: n=%d, want 3 (reported but dropped)", n)
	}
}

type captureWriter struct{ buf []byte }

func (cw *captureWriter) Write(p []byte) (int, error) {
	cw.buf = append(cw.buf, p...)
	return len(p), nil
}

// --- RPC-level tests with stubbed exec ---

func TestResolveURL_InvalidParams(t *testing.T) {
	rs := &RPCServer{}
	cases := []struct {
		name string
		p    *common.ResolveURLParams
	}{
		{"nil params", nil},
		{"empty url", &common.ResolveURLParams{URL: ""}},
		{"whitespace url", &common.ResolveURLParams{URL: "   "}},
		{"conflicting cookies", &common.ResolveURLParams{
			URL:             "https://example.com/v",
			CookiesFrom:     "firefox",
			CookiesFromFile: "/tmp/cookies.txt",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := rs.resolveURL(context.Background(), tc.p)
			var je *jrpc2.Error
			if !errors.As(err, &je) {
				t.Fatalf("want jrpc2.Error, got %T: %v", err, err)
			}
			if je.Code != codeInvalidParams {
				t.Errorf("code=%v, want %v", je.Code, codeInvalidParams)
			}
		})
	}
}

func TestResolveURL_BinaryNotFound(t *testing.T) {
	origLookPath := lookPath
	t.Cleanup(func() { lookPath = origLookPath })

	lookPath = func(_ string) (string, error) {
		return "", exec.ErrNotFound
	}

	rs := &RPCServer{}
	_, err := rs.resolveURL(context.Background(), &common.ResolveURLParams{
		URL: "https://youtube.com/watch?v=abc",
	})
	var je *jrpc2.Error
	if !errors.As(err, &je) {
		t.Fatalf("want jrpc2.Error, got %T: %v", err, err)
	}
	if je.Code != codeResolverUnavailable {
		t.Errorf("code=%v, want resolver_unavailable", je.Code)
	}
}

func TestResolveURL_Success(t *testing.T) {
	withStubbedExec(t, sampleYtdlpJSON, "", 0)

	rs := &RPCServer{}
	res, err := rs.resolveURL(context.Background(), &common.ResolveURLParams{
		URL: "https://youtube.com/watch?v=abc",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Title != "Test Video" {
		t.Errorf("Title=%q", res.Title)
	}
	if len(res.Formats) != 3 {
		t.Errorf("want 3 formats, got %d", len(res.Formats))
	}
}

func TestResolveURL_Unsupported(t *testing.T) {
	withStubbedExec(t, "", "ERROR: Unsupported URL: gopher://not-a-site", 1)

	rs := &RPCServer{}
	_, err := rs.resolveURL(context.Background(), &common.ResolveURLParams{
		URL: "https://site-not-supported.example/video",
	})
	var je *jrpc2.Error
	if !errors.As(err, &je) {
		t.Fatalf("want jrpc2.Error, got %T: %v", err, err)
	}
	if je.Code != codeResolverUnsupported {
		t.Errorf("code=%v, want resolver_unsupported", je.Code)
	}
}

func TestResolveURL_GenericFailure(t *testing.T) {
	withStubbedExec(t, "", "ERROR: something went wrong", 1)

	rs := &RPCServer{}
	_, err := rs.resolveURL(context.Background(), &common.ResolveURLParams{
		URL: "https://youtube.com/watch?v=abc",
	})
	var je *jrpc2.Error
	if !errors.As(err, &je) {
		t.Fatalf("want jrpc2.Error, got %T: %v", err, err)
	}
	if je.Code != codeResolverFailed {
		t.Errorf("code=%v, want resolver_failed", je.Code)
	}
	if !strings.Contains(je.Message, "something went wrong") {
		t.Errorf("message should include stderr tail; got %q", je.Message)
	}
}

func TestResolveURL_Timeout(t *testing.T) {
	// Stub exec with a command that sleeps longer than our timeout.
	origLookPath := lookPath
	origExecCommandContext := execCommandContext
	origDefault := defaultResolverTimeout
	t.Cleanup(func() {
		lookPath = origLookPath
		execCommandContext = origExecCommandContext
		defaultResolverTimeout = origDefault
	})
	defaultResolverTimeout = 100 * time.Millisecond

	lookPath = func(string) (string, error) { return "/usr/bin/yt-dlp-stub", nil }
	execCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "sleep 5")
	}

	rs := &RPCServer{}
	_, err := rs.resolveURL(context.Background(), &common.ResolveURLParams{
		URL: "https://youtube.com/watch?v=abc",
	})
	var je *jrpc2.Error
	if !errors.As(err, &je) {
		t.Fatalf("want jrpc2.Error, got %T: %v", err, err)
	}
	if je.Code != codeResolverTimeout {
		t.Errorf("code=%v, want resolver_timeout", je.Code)
	}
}

func TestResolveURL_CookiesMutualExclusion(t *testing.T) {
	rs := &RPCServer{}
	_, err := rs.resolveURL(context.Background(), &common.ResolveURLParams{
		URL:             "https://x/v",
		CookiesFrom:     "firefox",
		CookiesFromFile: "/x",
	})
	var je *jrpc2.Error
	if !errors.As(err, &je) {
		t.Fatalf("want jrpc2.Error, got %T: %v", err, err)
	}
	if je.Code != codeInvalidParams {
		t.Errorf("code=%v, want invalid_params", je.Code)
	}
}

func TestResolveURL_TimeoutCappedAtMax(t *testing.T) {
	// Caller asks for 9999s — we should clamp to maxResolverTimeout.
	// Use a stub that returns fast JSON; the test just verifies no error
	// surfaces due to the huge input.
	withStubbedExec(t, `{"title":"T","formats":[]}`, "", 0)

	rs := &RPCServer{}
	res, err := rs.resolveURL(context.Background(), &common.ResolveURLParams{
		URL:     "https://x/v",
		Timeout: 9999,
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if res.Title != "T" {
		t.Errorf("Title=%q", res.Title)
	}
}

// withStubbedExec installs a fake yt-dlp that echoes stdout/stderr and exits
// with the given code. Restored via t.Cleanup.
func withStubbedExec(t *testing.T, stdout, stderr string, exitCode int) {
	t.Helper()
	origLookPath := lookPath
	origExecCommandContext := execCommandContext
	t.Cleanup(func() {
		lookPath = origLookPath
		execCommandContext = origExecCommandContext
	})

	lookPath = func(string) (string, error) {
		return "/usr/bin/yt-dlp-stub", nil
	}
	execCommandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		// Compose an sh -c script that writes stdout, writes stderr to &2, exits code.
		// We intentionally build argv so there's no shell-injection surface in production;
		// this stub only runs in tests.
		script := ""
		if stdout != "" {
			script += "printf '%s' " + shellSingleQuote(stdout) + ";"
		}
		if stderr != "" {
			script += "printf '%s' " + shellSingleQuote(stderr) + " >&2;"
		}
		script += "exit " + itoa(exitCode)
		return exec.CommandContext(ctx, "sh", "-c", script)
	}
}

func shellSingleQuote(s string) string {
	// Wrap in single quotes and escape embedded single quotes.
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}
