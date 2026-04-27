package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/creachadair/jrpc2"
	youtube "github.com/kkdai/youtube/v2"
	"github.com/warpdl/warpdl/common"
)

func TestYouTubeDownload_MissingParams(t *testing.T) {
	rs := &RPCServer{}

	_, err := rs.youtubeDownload(context.Background(), nil)
	requireJrpcCode(t, err, codeInvalidParams)

	_, err = rs.youtubeDownload(context.Background(), &common.YouTubeDownloadParams{})
	requireJrpcCode(t, err, codeInvalidParams)

	_, err = rs.youtubeDownload(context.Background(), &common.YouTubeDownloadParams{VideoID: "x"})
	requireJrpcCode(t, err, codeInvalidParams)
}

func TestYouTubeDownload_FormatNotFound(t *testing.T) {
	withStubFetcher(t, &stubFetcher{video: makeFixtureVideo()})
	rs := &RPCServer{}
	_, err := rs.youtubeDownload(context.Background(), &common.YouTubeDownloadParams{
		VideoID:       "dQw4w9WgXcQ",
		VideoFormatID: "9999",
	})
	requireJrpcCode(t, err, codeFormatNotFound)
}

func TestYouTubeDownload_NonNumericFormat(t *testing.T) {
	withStubFetcher(t, &stubFetcher{video: makeFixtureVideo()})
	rs := &RPCServer{}
	_, err := rs.youtubeDownload(context.Background(), &common.YouTubeDownloadParams{
		VideoID:       "x",
		VideoFormatID: "abc",
	})
	requireJrpcCode(t, err, codeInvalidParams)
}

func TestYouTubeDownload_VideoIDMustNotBeBlank(t *testing.T) {
	rs := &RPCServer{}
	_, err := rs.youtubeDownload(context.Background(), &common.YouTubeDownloadParams{
		VideoID:       "   ",
		VideoFormatID: "18",
	})
	requireJrpcCode(t, err, codeInvalidParams)
}

func TestYouTubeDownload_FormatMismatch_VideoFormatNotVideo(t *testing.T) {
	// Pass an audio-only itag (251) as videoFormatId, with a separate audio
	// → format_mismatch.
	withStubFetcher(t, &stubFetcher{video: makeFixtureVideo()})
	rs := &RPCServer{}
	_, err := rs.youtubeDownload(context.Background(), &common.YouTubeDownloadParams{
		VideoID:       "x",
		VideoFormatID: "251", // audio-only
		AudioFormatID: "251",
	})
	requireJrpcCode(t, err, codeFormatMismatch)
}

func TestYouTubeDownload_FormatMismatch_AudioFormatNotAudio(t *testing.T) {
	// Pass a video-only itag as audioFormatId.
	withStubFetcher(t, &stubFetcher{video: makeFixtureVideo()})
	rs := &RPCServer{}
	_, err := rs.youtubeDownload(context.Background(), &common.YouTubeDownloadParams{
		VideoID:       "x",
		VideoFormatID: "137",
		AudioFormatID: "137",
	})
	requireJrpcCode(t, err, codeFormatMismatch)
}

func TestYouTubeDownload_AdaptiveRequiresFFmpeg(t *testing.T) {
	withStubFetcher(t, &stubFetcher{video: makeFixtureVideo()})
	prev := muxLookPath
	muxLookPath = func(string) (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() { muxLookPath = prev })

	rs := &RPCServer{}
	_, err := rs.youtubeDownload(context.Background(), &common.YouTubeDownloadParams{
		VideoID:       "x",
		VideoFormatID: "137",
		AudioFormatID: "251",
	})
	requireJrpcCode(t, err, codeMuxerUnavailable)
}

func TestYouTubeDownload_AdaptiveOrchestration(t *testing.T) {
	withStubFetcher(t, &stubFetcher{video: makeFixtureVideo()})

	// Pretend ffmpeg is on PATH.
	prevLP := muxLookPath
	muxLookPath = func(string) (string, error) { return "/usr/bin/ffmpeg", nil }
	t.Cleanup(func() { muxLookPath = prevLP })

	// Stub the leg downloader: write a placeholder file and report progress.
	// Real implementation registers each leg with rs.manager so it appears
	// in `warp list`; the test deliberately bypasses warplib/manager because
	// neither is meaningful here (we're testing orchestration + cleanup).
	prevDL := downloadLeg
	downloadLeg = func(_ *RPCServer, url, out string, _ int32, progress func(int64)) error {
		if err := os.WriteFile(out, []byte("dummy-"+url), 0o644); err != nil {
			return err
		}
		if progress != nil {
			progress(int64(len("dummy-" + url)))
		}
		return nil
	}
	t.Cleanup(func() { downloadLeg = prevDL })

	// Stub mux: just touch the output path.
	prevMux := muxRunner
	var muxCalled int32
	muxRunner = func(_ context.Context, vIn, aIn, out string) error {
		atomic.StoreInt32(&muxCalled, 1)
		// Validate inputs exist (the fake downloader wrote them).
		if _, err := os.Stat(vIn); err != nil {
			t.Errorf("video tmp missing: %v", err)
		}
		if _, err := os.Stat(aIn); err != nil {
			t.Errorf("audio tmp missing: %v", err)
		}
		return os.WriteFile(out, []byte("muxed"), 0o644)
	}
	t.Cleanup(func() { muxRunner = prevMux })

	dir := t.TempDir()
	rs := &RPCServer{}
	res, err := rs.youtubeDownload(context.Background(), &common.YouTubeDownloadParams{
		VideoID:       "dQw4w9WgXcQ",
		VideoFormatID: "137",
		AudioFormatID: "251",
		Dir:           dir,
		FileName:      "myclip",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Muxed {
		t.Error("Muxed should be true for adaptive")
	}
	if res.GID == "" || len(res.GID) != 32 {
		t.Errorf("GID should be 32-char hex, got %q", res.GID)
	}
	if !strings.HasSuffix(res.FileName, ".mp4") && !strings.HasSuffix(res.FileName, ".mkv") {
		t.Errorf("FileName should have container ext, got %q", res.FileName)
	}

	// Wait for goroutine to finish (mux + cleanup).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&muxCalled) == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if atomic.LoadInt32(&muxCalled) != 1 {
		t.Fatal("muxRunner was not invoked")
	}
	// Final file must exist.
	finalPath := filepath.Join(dir, res.FileName)
	if _, err := os.Stat(finalPath); err != nil {
		t.Errorf("final file missing: %v", err)
	}
	// Tmp dir must be cleaned up.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".warpdl-mux-") {
			t.Errorf("tmp dir not cleaned up: %s", e.Name())
		}
	}
}

func TestYouTubeDownload_AdaptiveDownloadFailureBroadcastsError(t *testing.T) {
	withStubFetcher(t, &stubFetcher{video: makeFixtureVideo()})
	prevLP := muxLookPath
	muxLookPath = func(string) (string, error) { return "/usr/bin/ffmpeg", nil }
	t.Cleanup(func() { muxLookPath = prevLP })

	prevDL := downloadLeg
	downloadLeg = func(_ *RPCServer, url, _ string, _ int32, _ func(int64)) error {
		// Audio leg fails; video leg succeeds (writes nothing).
		if strings.Contains(url, "audio") {
			return errors.New("network reset")
		}
		return nil
	}
	t.Cleanup(func() { downloadLeg = prevDL })

	// Replace the URL stub so we can distinguish video/audio in the
	// download runner above.
	withStubFetcher(t, &stubFetcher{
		video: makeFixtureVideo(),
		urlStub: func(_ *youtube.Video, f *youtube.Format) (string, error) {
			if strings.HasPrefix(f.MimeType, "audio/") {
				return "https://stub/audio", nil
			}
			return "https://stub/video", nil
		},
	})

	prevMux := muxRunner
	var muxCalled int32
	muxRunner = func(_ context.Context, _, _, _ string) error {
		atomic.StoreInt32(&muxCalled, 1)
		return nil
	}
	t.Cleanup(func() { muxRunner = prevMux })

	dir := t.TempDir()
	rs := &RPCServer{}
	res, err := rs.youtubeDownload(context.Background(), &common.YouTubeDownloadParams{
		VideoID:       "x",
		VideoFormatID: "137",
		AudioFormatID: "251",
		Dir:           dir,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.GID == "" {
		t.Fatal("expected GID")
	}

	// Allow goroutine to run.
	time.Sleep(120 * time.Millisecond)
	// Mux should not have been called.
	if atomic.LoadInt32(&muxCalled) == 1 {
		t.Error("mux should not run when a download leg fails")
	}
	// Tmp dir must be cleaned up regardless.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".warpdl-mux-") {
			t.Errorf("tmp dir not cleaned up after error: %s", e.Name())
		}
	}
}

func TestSanitizeOutputName(t *testing.T) {
	cases := map[string]string{
		"hello.mp4":              "hello.mp4",
		`bad/file:name?.mp4`:     "bad_file_name_.mp4",
		"  trim me   ":           "trim me",
		"with\x01control":        "withcontrol",
		strings.Repeat("a", 250): strings.Repeat("a", 200),
		"":                       "",
	}
	for in, want := range cases {
		got := sanitizeOutputName(in)
		if got != want {
			t.Errorf("sanitizeOutputName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOutputFileName(t *testing.T) {
	if got := outputFileName("", "Some Title", "mp4"); got != "Some Title.mp4" {
		t.Errorf("default = %q", got)
	}
	if got := outputFileName("override", "ignored", "webm"); got != "override.webm" {
		t.Errorf("override = %q", got)
	}
	if got := outputFileName("video.mp4", "ignored", "mp4"); got != "video.mp4" {
		t.Errorf("noDoubleExt = %q", got)
	}
	if got := outputFileName("", "  ", "mp4"); got != "video.mp4" {
		t.Errorf("blankFallback = %q", got)
	}
}

func TestGenGID(t *testing.T) {
	a, _ := genGID()
	b, _ := genGID()
	if a == b {
		t.Error("genGID should produce unique IDs")
	}
	if len(a) != 32 {
		t.Errorf("len = %d, want 32", len(a))
	}
}

func TestFirst(t *testing.T) {
	if first(nil) != "" {
		t.Error("nil → empty")
	}
	if first([]string{}) != "" {
		t.Error("empty → empty")
	}
	if first([]string{"a", "b"}) != "a" {
		t.Error("a,b → a")
	}
}

// requireJrpcCode asserts err is a *jrpc2.Error with the given code.
func requireJrpcCode(t *testing.T, err error, want jrpc2.Code) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var jerr *jrpc2.Error
	if !errors.As(err, &jerr) {
		t.Fatalf("expected *jrpc2.Error, got %T: %v", err, err)
	}
	if jerr.Code != want {
		t.Errorf("error code = %d, want %d (msg: %s)", jerr.Code, want, jerr.Message)
	}
}
