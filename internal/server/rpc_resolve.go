package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/creachadair/jrpc2"
	"github.com/warpdl/warpdl/common"
)

// Custom JSON-RPC error codes for resolve.url.
const (
	codeResolverUnavailable = jrpc2.Code(-32101)
	codeResolverTimeout     = jrpc2.Code(-32102)
	codeResolverFailed      = jrpc2.Code(-32103)
	codeResolverUnsupported = jrpc2.Code(-32104)
)

// Resolver default/cap values. Exposed as vars (not consts) so tests can tweak.
var (
	// defaultResolverTimeout is applied when Params.Timeout <= 0.
	defaultResolverTimeout = 30 * time.Second
	// maxResolverTimeout caps Params.Timeout regardless of caller value.
	maxResolverTimeout = 120 * time.Second
	// maxResolverOutputBytes caps yt-dlp stdout to prevent OOM.
	maxResolverOutputBytes int64 = 32 * 1024 * 1024
	// resolverBinary is the yt-dlp binary name. LookPath resolves it at runtime.
	resolverBinary = "yt-dlp"
)

// execCommandContext is replaced in tests with a stub that constructs a Cmd
// running under /bin/sh -c. This keeps the production path as direct exec.
var execCommandContext = exec.CommandContext

// lookPath is replaced in tests to simulate a missing or found binary.
var lookPath = exec.LookPath

// ytdlpVideoInfo is the subset of yt-dlp --dump-single-json we consume.
// yt-dlp emits ~150 fields per video; we tolerate unknown ones by ignoring them.
type ytdlpVideoInfo struct {
	Title     string          `json:"title"`
	Uploader  string          `json:"uploader"`
	Channel   string          `json:"channel"`
	Duration  float64         `json:"duration"`
	Formats   []ytdlpFormat   `json:"formats"`
}

type ytdlpFormat struct {
	FormatID   string   `json:"format_id"`
	URL        string   `json:"url"`
	Ext        string   `json:"ext"`
	Protocol   string   `json:"protocol"`
	VCodec     string   `json:"vcodec"`
	ACodec     string   `json:"acodec"`
	Height     int      `json:"height"`
	Width      int      `json:"width"`
	Fps        float64  `json:"fps"`
	ABR        float64  `json:"abr"`
	FileSize   int64    `json:"filesize"`
	FileSizeA  int64    `json:"filesize_approx"`
	FormatNote string   `json:"format_note"`
	MimeType   string   `json:"mime_type"`
	Quality    float64  `json:"quality"`
}

// resolveURL shells out to yt-dlp to extract downloadable format URLs for the
// given page URL. It is the handler for the JSON-RPC method "resolve.url".
//
// Security: params are validated before being passed to exec. No shell is used;
// the binary is resolved via exec.LookPath and arguments are passed directly.
func (rs *RPCServer) resolveURL(ctx context.Context, p *common.ResolveURLParams) (*common.ResolveURLResult, error) {
	if p == nil || strings.TrimSpace(p.URL) == "" {
		return nil, &jrpc2.Error{Code: codeInvalidParams, Message: "missing required param: url"}
	}
	if _, err := url.Parse(p.URL); err != nil {
		return nil, &jrpc2.Error{Code: codeInvalidParams, Message: "invalid url: " + err.Error()}
	}
	if p.CookiesFrom != "" && p.CookiesFromFile != "" {
		return nil, &jrpc2.Error{Code: codeInvalidParams, Message: "cookiesFrom and cookiesFromFile are mutually exclusive"}
	}

	// Resolve binary. Cache result in a local for the error message.
	bin, err := lookPath(resolverBinary)
	if err != nil {
		return nil, &jrpc2.Error{
			Code:    codeResolverUnavailable,
			Message: fmt.Sprintf("%s not found on PATH; install yt-dlp to use resolve.url", resolverBinary),
		}
	}

	// Apply timeout bounds.
	timeout := time.Duration(p.Timeout) * time.Second
	if timeout <= 0 {
		timeout = defaultResolverTimeout
	}
	if timeout > maxResolverTimeout {
		timeout = maxResolverTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"--no-warnings", "--skip-download", "--dump-single-json"}
	if p.CookiesFrom != "" {
		args = append(args, "--cookies-from-browser", p.CookiesFrom)
	}
	if p.CookiesFromFile != "" {
		args = append(args, "--cookies", p.CookiesFromFile)
	}
	args = append(args, p.URL)

	cmd := execCommandContext(runCtx, bin, args...)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = &limitedWriter{w: stdout, remaining: maxResolverOutputBytes}
	cmd.Stderr = &limitedWriter{w: stderr, remaining: 64 * 1024}

	if err := cmd.Run(); err != nil {
		// Timeout first — context deadline produces exec.ExitError wrapped around ctx.Err
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return nil, &jrpc2.Error{
				Code:    codeResolverTimeout,
				Message: fmt.Sprintf("%s timed out after %s", resolverBinary, timeout),
			}
		}
		// Extractor-not-found-for-URL
		tail := tailString(stderr.String(), 400)
		if strings.Contains(stderr.String(), "Unsupported URL") || strings.Contains(stderr.String(), "No video extraction") {
			return nil, &jrpc2.Error{
				Code:    codeResolverUnsupported,
				Message: "no extractor matched URL: " + tail,
			}
		}
		return nil, &jrpc2.Error{
			Code:    codeResolverFailed,
			Message: fmt.Sprintf("%s failed: %s", resolverBinary, tail),
		}
	}

	info, err := parseYtdlpOutput(stdout.Bytes())
	if err != nil {
		return nil, &jrpc2.Error{
			Code:    codeResolverFailed,
			Message: "failed to parse yt-dlp output: " + err.Error(),
		}
	}

	return info, nil
}

// parseYtdlpOutput converts yt-dlp's --dump-single-json payload into the
// public ResolveURLResult shape. Exported for tests (via lowercase wrapper).
func parseYtdlpOutput(raw []byte) (*common.ResolveURLResult, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, errors.New("empty output")
	}

	// --dump-single-json emits one object for videos, a playlist wrapper
	// for playlists. We take the first "video" entry in a playlist, or
	// the single object directly. Try single-video first.
	var info ytdlpVideoInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, fmt.Errorf("json decode: %w", err)
	}

	// If this was a playlist, formats will be empty and there's an "entries"
	// array. We don't currently support playlist resolution; return the
	// metadata with an empty formats list rather than failing outright.
	result := &common.ResolveURLResult{
		Title:   info.Title,
		Author:  pickNonEmpty(info.Uploader, info.Channel),
		Formats: make([]common.ResolvedFormat, 0, len(info.Formats)),
	}
	if info.Duration > 0 {
		result.Duration = int(info.Duration)
	}

	for _, f := range info.Formats {
		if f.URL == "" {
			continue
		}
		// Skip streaming manifests (HLS, DASH). They need separate handling.
		if isManifestProtocol(f.Protocol) {
			continue
		}

		hasVideo := f.VCodec != "" && f.VCodec != "none"
		hasAudio := f.ACodec != "" && f.ACodec != "none"
		if !hasVideo && !hasAudio {
			continue
		}

		size := f.FileSize
		if size == 0 {
			size = f.FileSizeA
		}

		result.Formats = append(result.Formats, common.ResolvedFormat{
			FormatID:     f.FormatID,
			URL:          f.URL,
			Ext:          f.Ext,
			MimeType:     f.MimeType,
			Quality:      deriveQualityLabel(f),
			FileSize:     size,
			HasVideo:     hasVideo,
			HasAudio:     hasAudio,
			VideoCodec:   codecOrEmpty(f.VCodec),
			AudioCodec:   codecOrEmpty(f.ACodec),
			Height:       f.Height,
			Width:        f.Width,
			Fps:          int(f.Fps),
			AudioBitrate: int(f.ABR),
		})
	}

	return result, nil
}

func isManifestProtocol(p string) bool {
	switch strings.ToLower(p) {
	case "m3u8", "m3u8_native", "http_dash_segments", "dash":
		return true
	}
	return false
}

func codecOrEmpty(c string) string {
	if c == "" || c == "none" {
		return ""
	}
	return c
}

func deriveQualityLabel(f ytdlpFormat) string {
	if f.FormatNote != "" {
		return f.FormatNote
	}
	if f.Height > 0 {
		return fmt.Sprintf("%dp", f.Height)
	}
	if f.ABR > 0 {
		return fmt.Sprintf("%d kbps", int(f.ABR))
	}
	return ""
}

func pickNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func tailString(s string, n int) string {
	if len(s) <= n {
		return strings.TrimSpace(s)
	}
	return "..." + strings.TrimSpace(s[len(s)-n:])
}

// limitedWriter caps how much data passes through to the underlying buffer.
// Oversized input is silently dropped after the limit is exceeded.
type limitedWriter struct {
	w         io.Writer
	remaining int64
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	if lw.remaining <= 0 {
		return len(p), nil
	}
	if int64(len(p)) > lw.remaining {
		p = p[:lw.remaining]
	}
	n, err := lw.w.Write(p)
	lw.remaining -= int64(n)
	return n, err
}
