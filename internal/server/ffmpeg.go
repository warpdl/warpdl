package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
)

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

// tailString returns the last n characters of s, prefixed with "..." when
// truncation occurs. Whitespace at edges is trimmed.
func tailString(s string, n int) string {
	if len(s) <= n {
		return strings.TrimSpace(s)
	}
	return "..." + strings.TrimSpace(s[len(s)-n:])
}

// muxBinary is the ffmpeg executable name. Test override permitted.
var muxBinary = "ffmpeg"

// muxLookPath resolves the ffmpeg binary; injectable for tests.
var muxLookPath = exec.LookPath

// muxRun runs the constructed command. Injected for tests so we can verify
// argument construction without invoking real ffmpeg.
var muxRun = func(cmd *exec.Cmd) error { return cmd.Run() }

// errMuxerUnavailable is returned by muxFiles when ffmpeg is not on PATH.
var errMuxerUnavailable = errors.New("ffmpeg not found on PATH")

// muxFiles invokes ffmpeg to remux the given video and audio files into a
// single container at outputPath. No re-encoding is performed; codecs are
// stream-copied. Output container is inferred from the outputPath extension.
//
// Args:
//
//	-y                  overwrite existing output
//	-i <video>          first input
//	-i <audio>          second input
//	-c:v copy -c:a copy stream-copy both
//	-map 0:v:0 -map 1:a:0  pick first video stream of input 0, first audio
//	                        stream of input 1 (defensive against malformed inputs)
//	-movflags +faststart  for mp4 only — moves moov atom to the front
//	<outputPath>
func muxFiles(ctx context.Context, videoPath, audioPath, outputPath string) error {
	bin, err := muxLookPath(muxBinary)
	if err != nil {
		return errMuxerUnavailable
	}

	args := []string{
		"-y",
		"-i", videoPath,
		"-i", audioPath,
		"-c:v", "copy",
		"-c:a", "copy",
		"-map", "0:v:0",
		"-map", "1:a:0",
	}
	if filepath.Ext(outputPath) == ".mp4" {
		args = append(args, "-movflags", "+faststart")
	}
	args = append(args, outputPath)

	cmd := exec.CommandContext(ctx, bin, args...)
	stderr := &bytes.Buffer{}
	cmd.Stderr = &limitedWriter{w: stderr, remaining: 64 * 1024}

	if err := muxRun(cmd); err != nil {
		return fmt.Errorf("ffmpeg failed: %s: %w", tailString(stderr.String(), 400), err)
	}
	return nil
}

// muxAvailable reports whether ffmpeg is currently on PATH. Cheap to call.
func muxAvailable() bool {
	_, err := muxLookPath(muxBinary)
	return err == nil
}

// pickContainer selects a sane output container extension given video + audio
// codec strings. Defaults to mp4; falls back to webm for vp9/opus or mkv for
// any cross-family combination ffmpeg cannot stream-copy into mp4.
func pickContainer(videoCodec, audioCodec string) string {
	v := codecFamily(videoCodec)
	a := codecFamily(audioCodec)
	if v == "avc" || v == "av1" {
		// AVC/AV1 + AAC or Opus → mp4 supports both (modern ffmpeg).
		return "mp4"
	}
	if v == "vp9" {
		if a == "opus" || a == "aac" {
			return "webm"
		}
	}
	// Conservative fallback: mkv accepts almost everything.
	return "mkv"
}

func codecFamily(c string) string {
	switch {
	case c == "":
		return ""
	case startsWithAny(c, "avc1", "avc3", "h264", "h.264"):
		return "avc"
	case startsWithAny(c, "av01", "av1"):
		return "av1"
	case startsWithAny(c, "vp09", "vp9"):
		return "vp9"
	case startsWithAny(c, "vp08", "vp8"):
		return "vp8"
	case startsWithAny(c, "mp4a", "aac"):
		return "aac"
	case startsWithAny(c, "opus"):
		return "opus"
	case startsWithAny(c, "vorbis"):
		return "vorbis"
	}
	return c
}

func startsWithAny(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if len(s) >= len(p) && s[:len(p)] == p {
			return true
		}
	}
	return false
}
