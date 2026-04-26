package server

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/creachadair/jrpc2"
	youtube "github.com/kkdai/youtube/v2"
	"github.com/warpdl/warpdl/common"
)

// Custom JSON-RPC error codes for resolve.url and youtube.download.
const (
	codeResolverFailed      = jrpc2.Code(-32101)
	codeResolverTimeout     = jrpc2.Code(-32102)
	codeResolverUnsupported = jrpc2.Code(-32104)
	codeMuxerUnavailable    = jrpc2.Code(-32105)
	codeFormatNotFound      = jrpc2.Code(-32106)
	codeFormatMismatch      = jrpc2.Code(-32107)
)

// Resolver default/cap values. Exposed as vars (not consts) so tests can tweak.
var (
	defaultResolverTimeout       = 30 * time.Second
	maxResolverTimeout           = 120 * time.Second
)

// ytClientFactory builds a kkdai client. Replaced in tests with a stub
// that returns a fake client (driven by an in-memory fixture).
var ytClientFactory = func() ytFetcher {
	return &kkdaiClient{client: &youtube.Client{}}
}

// ytFetcher is the minimal kkdai surface we depend on. Defined as an
// interface so tests can substitute a stub without spinning up HTTP.
type ytFetcher interface {
	GetVideoContext(ctx context.Context, url string) (*youtube.Video, error)
	GetStreamURLContext(ctx context.Context, video *youtube.Video, format *youtube.Format) (string, error)
}

type kkdaiClient struct{ client *youtube.Client }

func (k *kkdaiClient) GetVideoContext(ctx context.Context, url string) (*youtube.Video, error) {
	return k.client.GetVideoContext(ctx, url)
}
func (k *kkdaiClient) GetStreamURLContext(ctx context.Context, v *youtube.Video, f *youtube.Format) (string, error) {
	return k.client.GetStreamURLContext(ctx, v, f)
}

// resolveURL extracts downloadable format metadata for a YouTube URL via
// github.com/kkdai/youtube/v2. The response is metadata-only — direct
// stream URLs are deferred to youtube.download (kkdai requires a roundtrip
// per format to decode the signature cipher).
func (rs *RPCServer) resolveURL(ctx context.Context, p *common.ResolveURLParams) (*common.ResolveURLResult, error) {
	if p == nil || strings.TrimSpace(p.URL) == "" {
		return nil, &jrpc2.Error{Code: codeInvalidParams, Message: "missing required param: url"}
	}

	timeout := time.Duration(p.Timeout) * time.Second
	if timeout <= 0 {
		timeout = defaultResolverTimeout
	}
	if timeout > maxResolverTimeout {
		timeout = maxResolverTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	video, err := ytClientFactory().GetVideoContext(runCtx, p.URL)
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return nil, &jrpc2.Error{
				Code:    codeResolverTimeout,
				Message: fmt.Sprintf("resolve timed out after %s", timeout),
			}
		}
		msg := err.Error()
		if isUnsupportedURLError(msg) {
			return nil, &jrpc2.Error{Code: codeResolverUnsupported, Message: "URL is not a recognized YouTube video: " + msg}
		}
		return nil, &jrpc2.Error{Code: codeResolverFailed, Message: msg}
	}

	return mapVideo(video), nil
}

// mapVideo converts kkdai's Video into our wire shape.
func mapVideo(v *youtube.Video) *common.ResolveURLResult {
	out := &common.ResolveURLResult{
		VideoID:  v.ID,
		Title:    v.Title,
		Author:   v.Author,
		Duration: int(v.Duration / time.Second),
		Formats:  make([]common.ResolvedFormat, 0, len(v.Formats)),
	}
	for i := range v.Formats {
		out.Formats = append(out.Formats, mapFormat(&v.Formats[i]))
	}
	return out
}

// mapFormat translates a kkdai Format into a ResolvedFormat. URL is left
// empty: it is decoded lazily by youtube.download.
func mapFormat(f *youtube.Format) common.ResolvedFormat {
	mainMime, codecs := splitMimeType(f.MimeType)
	hasVideo := strings.HasPrefix(mainMime, "video/")
	hasAudio := strings.HasPrefix(mainMime, "audio/") || (hasVideo && len(codecs) >= 2)

	var vcodec, acodec string
	switch {
	case hasVideo && hasAudio:
		// Progressive: codecs are [video, audio].
		if len(codecs) >= 1 {
			vcodec = codecs[0]
		}
		if len(codecs) >= 2 {
			acodec = codecs[1]
		}
	case hasVideo:
		if len(codecs) >= 1 {
			vcodec = codecs[0]
		}
	case hasAudio:
		if len(codecs) >= 1 {
			acodec = codecs[0]
		}
	}

	return common.ResolvedFormat{
		FormatID:     strconv.Itoa(f.ItagNo),
		URL:          "", // resolved at download time
		Ext:          extFromMime(mainMime, f.MimeType),
		MimeType:     f.MimeType,
		Quality:      pickQualityLabel(f),
		FileSize:     f.ContentLength,
		HasVideo:     hasVideo,
		HasAudio:     hasAudio,
		VideoCodec:   vcodec,
		AudioCodec:   acodec,
		Height:       f.Height,
		Width:        f.Width,
		Fps:          f.FPS,
		AudioBitrate: bitrateKbps(f, hasAudio && !hasVideo),
	}
}

// splitMimeType separates the type/subtype from codecs.
//   `video/mp4; codecs="avc1.640028, mp4a.40.2"` →
//   ("video/mp4", ["avc1.640028", "mp4a.40.2"])
func splitMimeType(mt string) (string, []string) {
	if mt == "" {
		return "", nil
	}
	parts := strings.SplitN(mt, ";", 2)
	main := strings.TrimSpace(parts[0])
	if len(parts) < 2 {
		return main, nil
	}
	rest := strings.TrimSpace(parts[1])
	rest = strings.TrimPrefix(rest, "codecs=")
	rest = strings.Trim(rest, `"`)
	if rest == "" {
		return main, nil
	}
	out := strings.Split(rest, ",")
	for i := range out {
		out[i] = strings.TrimSpace(out[i])
	}
	return main, out
}

// extFromMime maps a MIME type to a typical file extension. Falls back to
// the part after the slash if not recognized.
func extFromMime(mainMime, full string) string {
	switch mainMime {
	case "video/mp4":
		return "mp4"
	case "video/webm":
		return "webm"
	case "audio/mp4":
		return "m4a"
	case "audio/webm":
		// audio/webm with opus → opus container preferred for raw audio,
		// but most users expect ".webm". Stay with webm to match upstream tools.
		return "webm"
	}
	if idx := strings.Index(mainMime, "/"); idx > 0 {
		return mainMime[idx+1:]
	}
	_ = full
	return ""
}

// pickQualityLabel returns the best human-facing label for a format.
func pickQualityLabel(f *youtube.Format) string {
	if f.QualityLabel != "" {
		return f.QualityLabel
	}
	if f.Quality != "" {
		return f.Quality
	}
	if f.Height > 0 {
		return strconv.Itoa(f.Height) + "p"
	}
	if f.Bitrate > 0 {
		return strconv.Itoa(f.Bitrate/1000) + " kbps"
	}
	return ""
}

// bitrateKbps returns the audio bitrate in kbps for audio-only streams,
// derived from kkdai's per-format bitrate (which is in bps).
func bitrateKbps(f *youtube.Format, audioOnly bool) int {
	if !audioOnly {
		return 0
	}
	if f.Bitrate <= 0 {
		return 0
	}
	return f.Bitrate / 1000
}

// isUnsupportedURLError reports whether a kkdai error string looks like a
// "not a YouTube URL" failure rather than a network/extractor failure.
func isUnsupportedURLError(msg string) bool {
	low := strings.ToLower(msg)
	return strings.Contains(low, "invalid characters in video id") ||
		strings.Contains(low, "url format not supported") ||
		strings.Contains(low, "no video id found") ||
		strings.Contains(low, "extractvideoid") ||
		strings.Contains(low, "video id is empty")
}
