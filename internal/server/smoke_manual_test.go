//go:build manual_smoke

package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"
)

// Manual smoke test for the native YouTube resolver + downloader. It hits the
// real YouTube site and runs the real ffmpeg mux, so it is excluded from CI
// behind the manual_smoke build tag. Run with:
//
//	go test -tags manual_smoke -run TestManualSmoke_YouTube ./internal/server/ -v -timeout 15m
//
// Requirements: outbound network access to youtube.com and ffmpeg on PATH.
func TestManualSmoke_YouTube(t *testing.T) {
	const watchURL = "https://www.youtube.com/watch?v=aqz-KE-bpKQ" // Big Buck Bunny (CC)

	if err := warplib.SetConfigDir(t.TempDir()); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}
	m, err := warplib.InitManager()
	if err != nil {
		t.Fatalf("InitManager: %v", err)
	}
	defer func() { _ = m.Close() }()

	rs := &RPCServer{
		manager: m,
		client:  &http.Client{CheckRedirect: warplib.RedirectPolicy(warplib.DefaultMaxRedirects)},
	}
	ctx := context.Background()

	res, err := rs.resolveURL(ctx, &common.ResolveURLParams{URL: watchURL})
	if err != nil {
		t.Fatalf("resolve.url failed: %v", err)
	}
	if res.VideoID == "" || res.Title == "" || len(res.Formats) == 0 {
		t.Fatalf("resolve.url returned incomplete metadata: %+v", res)
	}
	t.Logf("resolved %q (%s): %d formats", res.Title, res.VideoID, len(res.Formats))

	// Pick the smallest adaptive video-only format plus an audio-only format
	// to keep the smoke download light; the mux mechanics are identical to HD.
	var videoOnly, audioOnly, progressive *common.ResolvedFormat
	for i := range res.Formats {
		f := &res.Formats[i]
		switch {
		case f.HasVideo && !f.HasAudio:
			if videoOnly == nil || (f.Height > 0 && f.Height < videoOnly.Height) {
				videoOnly = f
			}
		case f.HasAudio && !f.HasVideo:
			if audioOnly == nil || (f.AudioBitrate > 0 && f.AudioBitrate < audioOnly.AudioBitrate) {
				audioOnly = f
			}
		case f.HasVideo && f.HasAudio:
			if progressive == nil {
				progressive = f
			}
		}
	}
	if videoOnly == nil || audioOnly == nil {
		t.Fatalf("no adaptive format pair found (videoOnly=%v audioOnly=%v)", videoOnly, audioOnly)
	}

	t.Run("progressive", func(t *testing.T) {
		if progressive == nil {
			t.Skip("no progressive format offered")
		}
		dir := t.TempDir()
		out, err := rs.youtubeDownload(ctx, &common.YouTubeDownloadParams{
			VideoID:       res.VideoID,
			VideoFormatID: progressive.FormatID,
			Dir:           dir,
			FileName:      "smoke-progressive",
			Connections:   4,
		})
		if err != nil {
			t.Fatalf("youtube.download progressive failed: %v", err)
		}
		if out.Muxed {
			t.Error("progressive download must not report Muxed")
		}
		waitForFile(t, filepath.Join(dir, out.FileName), 10*time.Minute)
	})

	t.Run("adaptive_mux", func(t *testing.T) {
		dir := t.TempDir()
		out, err := rs.youtubeDownload(ctx, &common.YouTubeDownloadParams{
			VideoID:       res.VideoID,
			VideoFormatID: videoOnly.FormatID,
			AudioFormatID: audioOnly.FormatID,
			Dir:           dir,
			FileName:      "smoke-adaptive",
			Connections:   4,
		})
		if err != nil {
			t.Fatalf("youtube.download adaptive failed: %v", err)
		}
		if !out.Muxed {
			t.Error("adaptive download must report Muxed")
		}
		if len(out.GID) != 32 {
			t.Errorf("expected 32-char synthetic GID, got %q", out.GID)
		}
		waitForFile(t, filepath.Join(dir, out.FileName), 10*time.Minute)

		// The mux scratch dir must be gone once the final file exists.
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".warpdl-mux-") {
				t.Errorf("mux tmp dir not cleaned up: %s", e.Name())
			}
		}
	})
}

// waitForFile polls until path exists with non-zero size or the deadline hits.
func waitForFile(t *testing.T, path string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if fi, err := os.Stat(path); err == nil && fi.Size() > 0 {
			t.Logf("file ready: %s (%d bytes)", path, fi.Size())
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("file %s did not appear in %s", path, d)
}
