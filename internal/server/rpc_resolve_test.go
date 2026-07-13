package server

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	jsonrpc "github.com/gumeniukcom/golang-jsonrpc2/v2"
	youtube "github.com/kkdai/youtube/v2"
	"github.com/warpdl/warpdl/common"
)

// stubFetcher is a hand-rolled ytFetcher for test injection.
type stubFetcher struct {
	video   *youtube.Video
	err     error
	urlStub func(*youtube.Video, *youtube.Format) (string, error)
	delay   time.Duration
}

func (s *stubFetcher) GetVideoContext(ctx context.Context, _ string) (*youtube.Video, error) {
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.video, nil
}

func (s *stubFetcher) GetStreamURLContext(_ context.Context, v *youtube.Video, f *youtube.Format) (string, error) {
	if s.urlStub != nil {
		return s.urlStub(v, f)
	}
	return "https://stub/" + f.MimeType, nil
}

// withStubFetcher swaps ytClientFactory for the duration of the test.
func withStubFetcher(t *testing.T, sf *stubFetcher) {
	t.Helper()
	prev := ytClientFactory
	ytClientFactory = func() ytFetcher { return sf }
	t.Cleanup(func() { ytClientFactory = prev })
}

func makeFixtureVideo() *youtube.Video {
	return &youtube.Video{
		ID:       "dQw4w9WgXcQ",
		Title:    "Never Gonna Give You Up",
		Author:   "Rick Astley",
		Duration: 213 * time.Second,
		Formats: youtube.FormatList{
			// Progressive 360p mp4 (itag 18) — has video + audio.
			{
				ItagNo:        18,
				MimeType:      `video/mp4; codecs="avc1.42001E, mp4a.40.2"`,
				Quality:       "medium",
				QualityLabel:  "360p",
				Width:         640,
				Height:        360,
				FPS:           30,
				Bitrate:       500000,
				ContentLength: 12_345_678,
			},
			// Adaptive video-only 1080p60 mp4 (itag 137).
			{
				ItagNo:        137,
				MimeType:      `video/mp4; codecs="avc1.640028"`,
				Quality:       "hd1080",
				QualityLabel:  "1080p60",
				Width:         1920,
				Height:        1080,
				FPS:           60,
				Bitrate:       4_500_000,
				ContentLength: 75_000_000,
			},
			// Adaptive audio-only opus 160k (itag 251).
			{
				ItagNo:        251,
				MimeType:      `audio/webm; codecs="opus"`,
				Quality:       "tiny",
				QualityLabel:  "",
				Bitrate:       160_000,
				ContentLength: 2_500_000,
			},
			// Adaptive video-only webm (itag 248) — different container.
			{
				ItagNo:        248,
				MimeType:      `video/webm; codecs="vp9"`,
				Quality:       "hd1080",
				QualityLabel:  "1080p",
				Width:         1920,
				Height:        1080,
				FPS:           30,
				Bitrate:       3_500_000,
				ContentLength: 60_000_000,
			},
		},
	}
}

func TestResolveURL_Success(t *testing.T) {
	withStubFetcher(t, &stubFetcher{video: makeFixtureVideo()})

	rs := &RPCServer{}
	res, err := rs.resolveURL(context.Background(), &common.ResolveURLParams{URL: "https://youtube.com/watch?v=dQw4w9WgXcQ"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.VideoID != "dQw4w9WgXcQ" {
		t.Errorf("VideoID = %q, want %q", res.VideoID, "dQw4w9WgXcQ")
	}
	if res.Title != "Never Gonna Give You Up" {
		t.Errorf("Title = %q", res.Title)
	}
	if res.Author != "Rick Astley" {
		t.Errorf("Author = %q", res.Author)
	}
	if res.Duration != 213 {
		t.Errorf("Duration = %d, want 213", res.Duration)
	}
	if len(res.Formats) != 4 {
		t.Fatalf("len(Formats) = %d, want 4", len(res.Formats))
	}
}

func TestResolveURL_FormatMappingProgressive(t *testing.T) {
	withStubFetcher(t, &stubFetcher{video: makeFixtureVideo()})
	rs := &RPCServer{}
	res, err := rs.resolveURL(context.Background(), &common.ResolveURLParams{URL: "https://youtube.com/watch?v=x"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// itag 18 — progressive
	f := findFormat(t, res.Formats, "18")
	if !f.HasVideo || !f.HasAudio {
		t.Errorf("itag 18 should have both video+audio, got video=%v audio=%v", f.HasVideo, f.HasAudio)
	}
	if f.VideoCodec != "avc1.42001E" {
		t.Errorf("itag 18 VideoCodec = %q", f.VideoCodec)
	}
	if f.AudioCodec != "mp4a.40.2" {
		t.Errorf("itag 18 AudioCodec = %q", f.AudioCodec)
	}
	if f.Ext != "mp4" {
		t.Errorf("itag 18 Ext = %q", f.Ext)
	}
	if f.Quality != "360p" {
		t.Errorf("itag 18 Quality = %q", f.Quality)
	}
	if f.URL != "" {
		t.Errorf("URL must be empty in resolve.url response, got %q", f.URL)
	}
}

func TestResolveURL_FormatMappingVideoOnly(t *testing.T) {
	withStubFetcher(t, &stubFetcher{video: makeFixtureVideo()})
	rs := &RPCServer{}
	res, _ := rs.resolveURL(context.Background(), &common.ResolveURLParams{URL: "x"})
	f := findFormat(t, res.Formats, "137")
	if !f.HasVideo || f.HasAudio {
		t.Errorf("itag 137 should be video-only, got video=%v audio=%v", f.HasVideo, f.HasAudio)
	}
	if f.VideoCodec != "avc1.640028" {
		t.Errorf("itag 137 VideoCodec = %q", f.VideoCodec)
	}
	if f.AudioCodec != "" {
		t.Errorf("itag 137 AudioCodec must be empty, got %q", f.AudioCodec)
	}
	if f.Quality != "1080p60" {
		t.Errorf("itag 137 Quality = %q", f.Quality)
	}
	if f.Fps != 60 {
		t.Errorf("itag 137 Fps = %d", f.Fps)
	}
}

func TestResolveURL_FormatMappingAudioOnly(t *testing.T) {
	withStubFetcher(t, &stubFetcher{video: makeFixtureVideo()})
	rs := &RPCServer{}
	res, _ := rs.resolveURL(context.Background(), &common.ResolveURLParams{URL: "x"})
	f := findFormat(t, res.Formats, "251")
	if f.HasVideo || !f.HasAudio {
		t.Errorf("itag 251 should be audio-only, got video=%v audio=%v", f.HasVideo, f.HasAudio)
	}
	if f.AudioCodec != "opus" {
		t.Errorf("itag 251 AudioCodec = %q", f.AudioCodec)
	}
	if f.AudioBitrate != 160 {
		t.Errorf("itag 251 AudioBitrate = %d, want 160", f.AudioBitrate)
	}
	if f.Ext != "webm" {
		t.Errorf("itag 251 Ext = %q", f.Ext)
	}
}

func TestResolveURL_FormatMappingWebmContainer(t *testing.T) {
	withStubFetcher(t, &stubFetcher{video: makeFixtureVideo()})
	rs := &RPCServer{}
	res, _ := rs.resolveURL(context.Background(), &common.ResolveURLParams{URL: "x"})
	f := findFormat(t, res.Formats, "248")
	if f.Ext != "webm" {
		t.Errorf("itag 248 Ext = %q, want webm", f.Ext)
	}
	if f.VideoCodec != "vp9" {
		t.Errorf("itag 248 VideoCodec = %q", f.VideoCodec)
	}
}

func TestResolveURL_MissingURL(t *testing.T) {
	rs := &RPCServer{}
	_, err := rs.resolveURL(context.Background(), &common.ResolveURLParams{URL: ""})
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
	var rerr *jsonrpc.RPCError
	if !errors.As(err, &rerr) || rerr.Code != codeInvalidParams {
		t.Errorf("expected codeInvalidParams, got %v", err)
	}
}

func TestResolveURL_NilParams(t *testing.T) {
	rs := &RPCServer{}
	_, err := rs.resolveURL(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil params")
	}
	var rerr *jsonrpc.RPCError
	if !errors.As(err, &rerr) || rerr.Code != codeInvalidParams {
		t.Errorf("expected codeInvalidParams, got %v", err)
	}
}

func TestResolveURL_Timeout(t *testing.T) {
	prev := defaultResolverTimeout
	defaultResolverTimeout = 50 * time.Millisecond
	t.Cleanup(func() { defaultResolverTimeout = prev })

	withStubFetcher(t, &stubFetcher{delay: 200 * time.Millisecond})
	rs := &RPCServer{}
	_, err := rs.resolveURL(context.Background(), &common.ResolveURLParams{URL: "https://youtube.com/watch?v=x"})
	if err == nil {
		t.Fatal("expected timeout error")
	}
	var rerr *jsonrpc.RPCError
	if !errors.As(err, &rerr) || rerr.Code != codeResolverTimeout {
		t.Errorf("expected codeResolverTimeout, got %v", err)
	}
}

func TestResolveURL_TimeoutClamping(t *testing.T) {
	withStubFetcher(t, &stubFetcher{video: makeFixtureVideo()})
	rs := &RPCServer{}
	// 99999 → clamp to maxResolverTimeout (120s); test only that it doesn't error.
	_, err := rs.resolveURL(context.Background(), &common.ResolveURLParams{URL: "x", Timeout: 99999})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveURL_GenericFailure(t *testing.T) {
	withStubFetcher(t, &stubFetcher{err: errors.New("status code: 403")})
	rs := &RPCServer{}
	_, err := rs.resolveURL(context.Background(), &common.ResolveURLParams{URL: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	var rerr *jsonrpc.RPCError
	if !errors.As(err, &rerr) || rerr.Code != codeResolverFailed {
		t.Errorf("expected codeResolverFailed, got %v", err)
	}
	// The client-visible detail rides in error.data (the old protocol had
	// it in error.message).
	if rerr.Data != "status code: 403" {
		t.Errorf("expected data %q, got %v", "status code: 403", rerr.Data)
	}
}

func TestResolveURL_UnsupportedURL(t *testing.T) {
	withStubFetcher(t, &stubFetcher{err: errors.New("invalid characters in video id")})
	rs := &RPCServer{}
	_, err := rs.resolveURL(context.Background(), &common.ResolveURLParams{URL: "https://example.com/notyoutube"})
	if err == nil {
		t.Fatal("expected error")
	}
	var rerr *jsonrpc.RPCError
	if !errors.As(err, &rerr) || rerr.Code != codeResolverUnsupported {
		t.Errorf("expected codeResolverUnsupported, got %v", err)
	}
	if want := "URL is not a recognized YouTube video: invalid characters in video id"; rerr.Data != want {
		t.Errorf("expected data %q, got %v", want, rerr.Data)
	}
}

func TestSplitMimeType(t *testing.T) {
	tests := []struct {
		in       string
		wantMain string
		wantN    int
	}{
		{`video/mp4; codecs="avc1.640028, mp4a.40.2"`, "video/mp4", 2},
		{`audio/webm; codecs="opus"`, "audio/webm", 1},
		{`video/webm`, "video/webm", 0},
		{``, "", 0},
		{`video/mp4; codecs=""`, "video/mp4", 0},
	}
	for _, tt := range tests {
		main, codecs := splitMimeType(tt.in)
		if main != tt.wantMain {
			t.Errorf("splitMimeType(%q) main = %q, want %q", tt.in, main, tt.wantMain)
		}
		if len(codecs) != tt.wantN {
			t.Errorf("splitMimeType(%q) codecs count = %d, want %d", tt.in, len(codecs), tt.wantN)
		}
	}
}

func TestExtFromMime(t *testing.T) {
	cases := map[string]string{
		"video/mp4":  "mp4",
		"video/webm": "webm",
		"audio/mp4":  "m4a",
		"audio/webm": "webm",
		"audio/ogg":  "ogg", // fallback
		"":           "",
	}
	for mt, want := range cases {
		got := extFromMime(mt, mt)
		if got != want {
			t.Errorf("extFromMime(%q) = %q, want %q", mt, got, want)
		}
	}
}

func TestPickQualityLabel(t *testing.T) {
	tests := []struct {
		f    youtube.Format
		want string
	}{
		{youtube.Format{QualityLabel: "1080p60"}, "1080p60"},
		{youtube.Format{Quality: "medium"}, "medium"},
		{youtube.Format{Height: 720}, "720p"},
		{youtube.Format{Bitrate: 128000}, "128 kbps"},
		{youtube.Format{}, ""},
	}
	for _, tt := range tests {
		got := pickQualityLabel(&tt.f)
		if got != tt.want {
			t.Errorf("pickQualityLabel(%+v) = %q, want %q", tt.f, got, tt.want)
		}
	}
}

func TestBitrateKbps(t *testing.T) {
	if bitrateKbps(&youtube.Format{Bitrate: 128000}, true) != 128 {
		t.Error("audio-only 128k → 128")
	}
	if bitrateKbps(&youtube.Format{Bitrate: 4_500_000}, false) != 0 {
		t.Error("video-only must report 0 bitrate (we use AudioBitrate field)")
	}
	if bitrateKbps(&youtube.Format{}, true) != 0 {
		t.Error("zero bitrate → 0")
	}
}

func TestIsUnsupportedURLError(t *testing.T) {
	yes := []string{
		"invalid characters in video id",
		"URL format not supported",
		"no video id found",
		"extractVideoID failed",
		"video id is empty",
	}
	for _, s := range yes {
		if !isUnsupportedURLError(s) {
			t.Errorf("expected unsupported for %q", s)
		}
	}
	no := []string{"network error", "status code: 500", "json parse"}
	for _, s := range no {
		if isUnsupportedURLError(s) {
			t.Errorf("expected NOT unsupported for %q", s)
		}
	}
}

// findFormat asserts a ResolvedFormat with the given itag exists.
func findFormat(t *testing.T, formats []common.ResolvedFormat, itag string) common.ResolvedFormat {
	t.Helper()
	for i := range formats {
		if formats[i].FormatID == itag {
			return formats[i]
		}
	}
	t.Fatalf("itag %s not found in formats; got: %v", itag, summarizeFormats(formats))
	return common.ResolvedFormat{}
}

func summarizeFormats(formats []common.ResolvedFormat) string {
	ids := make([]string, len(formats))
	for i := range formats {
		ids[i] = formats[i].FormatID
	}
	return strings.Join(ids, ",")
}
