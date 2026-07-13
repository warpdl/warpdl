package server

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	jsonrpc "github.com/gumeniukcom/golang-jsonrpc2/v2"
	youtube "github.com/kkdai/youtube/v2"
	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func TestYouTubeDownload_MissingParams(t *testing.T) {
	rs := &RPCServer{}

	_, err := rs.youtubeDownload(context.Background(), nil)
	requireRPCCode(t, err, codeInvalidParams)

	_, err = rs.youtubeDownload(context.Background(), &common.YouTubeDownloadParams{})
	requireRPCCode(t, err, codeInvalidParams)

	_, err = rs.youtubeDownload(context.Background(), &common.YouTubeDownloadParams{VideoID: "x"})
	requireRPCCode(t, err, codeInvalidParams)
}

func TestYouTubeDownload_FormatNotFound(t *testing.T) {
	withStubFetcher(t, &stubFetcher{video: makeFixtureVideo()})
	rs := &RPCServer{}
	_, err := rs.youtubeDownload(context.Background(), &common.YouTubeDownloadParams{
		VideoID:       "dQw4w9WgXcQ",
		VideoFormatID: "9999",
	})
	requireRPCCode(t, err, codeFormatNotFound)
}

func TestYouTubeDownload_NonNumericFormat(t *testing.T) {
	withStubFetcher(t, &stubFetcher{video: makeFixtureVideo()})
	rs := &RPCServer{}
	_, err := rs.youtubeDownload(context.Background(), &common.YouTubeDownloadParams{
		VideoID:       "x",
		VideoFormatID: "abc",
	})
	requireRPCCode(t, err, codeInvalidParams)
}

func TestYouTubeDownload_VideoIDMustNotBeBlank(t *testing.T) {
	rs := &RPCServer{}
	_, err := rs.youtubeDownload(context.Background(), &common.YouTubeDownloadParams{
		VideoID:       "   ",
		VideoFormatID: "18",
	})
	requireRPCCode(t, err, codeInvalidParams)
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
	requireRPCCode(t, err, codeFormatMismatch)
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
	requireRPCCode(t, err, codeFormatMismatch)
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
	requireRPCCode(t, err, codeMuxerUnavailable)
}

// newAdaptiveTestServer fakes ffmpeg discovery and returns an RPCServer
// whose leg downloader and muxer are stubbed via the instance test seams
// (no package globals, so nothing to restore and nothing for the adaptive
// goroutine to race against). The leg stub writes a placeholder file and
// reports progress; the real implementation registers each leg with
// rs.manager so it appears in `warp list`, but neither warplib nor the
// manager is meaningful here (we're testing orchestration + cleanup).
// The returned flag is set to 1 once the muxer has run.
func newAdaptiveTestServer(t *testing.T) (rs *RPCServer, muxCalled *int32) {
	t.Helper()

	prevLP := muxLookPath
	muxLookPath = func(string) (string, error) { return "/usr/bin/ffmpeg", nil }
	t.Cleanup(func() { muxLookPath = prevLP })

	muxCalled = new(int32)
	rs = &RPCServer{
		legDownloader: func(_ *RPCServer, url, out string, _ int32, progress func(int64)) error {
			if err := os.WriteFile(out, []byte("dummy-"+url), 0o644); err != nil {
				return err
			}
			if progress != nil {
				progress(int64(len("dummy-" + url)))
			}
			return nil
		},
		// Stub mux: validate the fake downloader wrote both inputs, then
		// touch the output path.
		muxer: func(_ context.Context, vIn, aIn, out string) error {
			atomic.StoreInt32(muxCalled, 1)
			if _, err := os.Stat(vIn); err != nil {
				t.Errorf("video tmp missing: %v", err)
			}
			if _, err := os.Stat(aIn); err != nil {
				t.Errorf("audio tmp missing: %v", err)
			}
			return os.WriteFile(out, []byte("muxed"), 0o644)
		},
	}
	return rs, muxCalled
}

// waitFor polls cond every 20ms until it returns true or the deadline passes.
func waitFor(cond func() bool, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

// muxTmpDirs lists leftover .warpdl-mux-* entries in dir.
func muxTmpDirs(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".warpdl-mux-") {
			out = append(out, e.Name())
		}
	}
	return out
}

// requireMuxTmpDirsCleaned polls until the adaptive goroutine's deferred
// cleanup has removed all mux tmp dirs (cleanup runs after the mux flag is
// set, so an immediate scan would race it).
func requireMuxTmpDirsCleaned(t *testing.T, dir string) {
	t.Helper()
	if !waitFor(func() bool { return len(muxTmpDirs(dir)) == 0 }, 2*time.Second) {
		t.Errorf("tmp dir not cleaned up: %v", muxTmpDirs(dir))
	}
}

func TestYouTubeDownload_AdaptiveOrchestration(t *testing.T) {
	withStubFetcher(t, &stubFetcher{video: makeFixtureVideo()})
	rs, muxCalled := newAdaptiveTestServer(t)

	dir := t.TempDir()
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
	if len(res.GID) != 32 {
		t.Errorf("GID should be 32-char hex, got %q", res.GID)
	}
	if !strings.HasSuffix(res.FileName, ".mp4") && !strings.HasSuffix(res.FileName, ".mkv") {
		t.Errorf("FileName should have container ext, got %q", res.FileName)
	}

	// Wait for the adaptive goroutine to reach the mux step.
	if !waitFor(func() bool { return atomic.LoadInt32(muxCalled) == 1 }, 2*time.Second) {
		t.Fatal("muxRunner was not invoked")
	}
	// Final file must exist.
	if _, err := os.Stat(filepath.Join(dir, res.FileName)); err != nil {
		t.Errorf("final file missing: %v", err)
	}
	requireMuxTmpDirsCleaned(t, dir)
}

func TestYouTubeDownload_AdaptiveDownloadFailureBroadcastsError(t *testing.T) {
	prevLP := muxLookPath
	muxLookPath = func(string) (string, error) { return "/usr/bin/ffmpeg", nil }
	t.Cleanup(func() { muxLookPath = prevLP })

	// The URL stub distinguishes video/audio so the leg stub below can fail
	// just the audio leg.
	withStubFetcher(t, &stubFetcher{
		video: makeFixtureVideo(),
		urlStub: func(_ *youtube.Video, f *youtube.Format) (string, error) {
			if strings.HasPrefix(f.MimeType, "audio/") {
				return "https://stub/audio", nil
			}
			return "https://stub/video", nil
		},
	})

	var muxCalled int32
	rs := &RPCServer{
		legDownloader: func(_ *RPCServer, url, _ string, _ int32, _ func(int64)) error {
			// Audio leg fails; video leg succeeds (writes nothing).
			if strings.Contains(url, "audio") {
				return errors.New("network reset")
			}
			return nil
		},
		muxer: func(_ context.Context, _, _, _ string) error {
			atomic.StoreInt32(&muxCalled, 1)
			return nil
		},
	}

	dir := t.TempDir()
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

	// Tmp dir must be cleaned up regardless; cleanup completing also means
	// the adaptive goroutine has finished, so the mux check below is not
	// racing it.
	requireMuxTmpDirsCleaned(t, dir)
	if atomic.LoadInt32(&muxCalled) == 1 {
		t.Error("mux should not run when a download leg fails")
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

// TestDefaultDownloadLeg_StartFailureSurfaces is a regression test for the
// d.Start error routing: a synchronous Start failure (here a missing parent
// directory for the output file, so openFile fails with ENOENT) must be
// delivered to the leg's done channel. Before the fix, Start errors were
// discarded and the leg blocked forever waiting for handlers that never fire.
func TestDefaultDownloadLeg_StartFailureSurfaces(t *testing.T) {
	if err := warplib.SetConfigDir(t.TempDir()); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	m, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer func() { _ = m.Close() }()

	srv := newRangeServer(bytes.Repeat([]byte("leg"), 2048))
	defer srv.Close()

	// The leg assumes its tmp dir already exists; pointing it at a missing
	// directory makes warplib's openFile fail synchronously inside Start.
	outPath := filepath.Join(t.TempDir(), "missing-subdir", "video.mp4")

	rs := &RPCServer{
		manager: m,
		client:  &http.Client{CheckRedirect: warplib.RedirectPolicy(warplib.DefaultMaxRedirects)},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- defaultDownloadLeg(rs, srv.URL+"/video.bin", outPath, 1, nil)
	}()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected an error for a missing output directory")
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Errorf("expected a not-exist error, got: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("defaultDownloadLeg hung: Start error was not delivered to done channel")
	}
}

// requireRPCCode asserts err is a *jsonrpc.RPCError with the given code.
func requireRPCCode(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var rerr *jsonrpc.RPCError
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *jsonrpc.RPCError, got %T: %v", err, err)
	}
	if rerr.Code != want {
		t.Errorf("error code = %d, want %d (err: %v)", rerr.Code, want, rerr)
	}
}
