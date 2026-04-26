package server

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestPickContainer(t *testing.T) {
	cases := []struct {
		v, a string
		want string
	}{
		{"avc1.640028", "mp4a.40.2", "mp4"},
		{"av01.0.05M.08", "mp4a.40.2", "mp4"},
		{"vp9", "opus", "webm"},
		{"vp09.00.50.08", "opus", "webm"},
		{"vp9", "vorbis", "mkv"},
		{"vp9", "mp4a", "webm"},
		{"unknown", "unknown", "mkv"},
	}
	for _, tt := range cases {
		got := pickContainer(tt.v, tt.a)
		if got != tt.want {
			t.Errorf("pickContainer(%q,%q) = %q, want %q", tt.v, tt.a, got, tt.want)
		}
	}
}

func TestCodecFamily(t *testing.T) {
	cases := map[string]string{
		"":             "",
		"avc1.640028":  "avc",
		"avc3.42E0":    "avc",
		"h264":         "avc",
		"av01.0.05M":   "av1",
		"vp09.00.50":   "vp9",
		"vp9":          "vp9",
		"mp4a.40.2":    "aac",
		"aac":          "aac",
		"opus":         "opus",
		"vorbis":       "vorbis",
		"completely-novel": "completely-novel",
	}
	for in, want := range cases {
		got := codecFamily(in)
		if got != want {
			t.Errorf("codecFamily(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMuxFiles_Unavailable(t *testing.T) {
	prev := muxLookPath
	muxLookPath = func(string) (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() { muxLookPath = prev })

	err := muxFiles(context.Background(), "/v.mp4", "/a.m4a", "/out.mp4")
	if !errors.Is(err, errMuxerUnavailable) {
		t.Errorf("expected errMuxerUnavailable, got %v", err)
	}
}

func TestMuxFiles_ConstructsArgs(t *testing.T) {
	prev := muxLookPath
	muxLookPath = func(name string) (string, error) { return "/usr/bin/ffmpeg", nil }
	t.Cleanup(func() { muxLookPath = prev })

	prevRun := muxRun
	var capturedArgs []string
	muxRun = func(cmd *exec.Cmd) error {
		capturedArgs = append([]string{}, cmd.Args...)
		return nil
	}
	t.Cleanup(func() { muxRun = prevRun })

	err := muxFiles(context.Background(), "/tmp/v.mp4", "/tmp/a.m4a", "/out/final.mp4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(capturedArgs) == 0 {
		t.Fatal("no args captured")
	}
	if capturedArgs[0] != "/usr/bin/ffmpeg" {
		t.Errorf("argv[0] = %q, want /usr/bin/ffmpeg", capturedArgs[0])
	}
	joined := strings.Join(capturedArgs, " ")
	if !strings.Contains(joined, "-c:v copy") || !strings.Contains(joined, "-c:a copy") {
		t.Errorf("expected -c:v copy and -c:a copy, got: %s", joined)
	}
	if !strings.Contains(joined, "-i /tmp/v.mp4") {
		t.Errorf("expected -i /tmp/v.mp4, got: %s", joined)
	}
	if !strings.Contains(joined, "-i /tmp/a.m4a") {
		t.Errorf("expected -i /tmp/a.m4a, got: %s", joined)
	}
	if !strings.HasSuffix(joined, "/out/final.mp4") {
		t.Errorf("output should be last arg; got: %s", joined)
	}
	// mp4 path → faststart flag
	if !strings.Contains(joined, "-movflags +faststart") {
		t.Errorf("expected -movflags +faststart for mp4 output; got: %s", joined)
	}
}

func TestMuxFiles_NoFaststartForNonMp4(t *testing.T) {
	prev := muxLookPath
	muxLookPath = func(string) (string, error) { return "/usr/bin/ffmpeg", nil }
	t.Cleanup(func() { muxLookPath = prev })

	prevRun := muxRun
	var capturedArgs []string
	muxRun = func(cmd *exec.Cmd) error {
		capturedArgs = cmd.Args
		return nil
	}
	t.Cleanup(func() { muxRun = prevRun })

	_ = muxFiles(context.Background(), "/v.webm", "/a.webm", "/out/x.webm")
	joined := strings.Join(capturedArgs, " ")
	if strings.Contains(joined, "faststart") {
		t.Errorf("faststart should not be applied to non-mp4 output; got: %s", joined)
	}
}

func TestMuxFiles_RunFailureIncludesStderr(t *testing.T) {
	prev := muxLookPath
	muxLookPath = func(string) (string, error) { return "/usr/bin/ffmpeg", nil }
	t.Cleanup(func() { muxLookPath = prev })

	prevRun := muxRun
	muxRun = func(cmd *exec.Cmd) error {
		// Simulate ffmpeg writing to stderr then exiting non-zero.
		if cmd.Stderr != nil {
			cmd.Stderr.Write([]byte("Codec mismatch: cannot copy\n"))
		}
		return errors.New("exit status 1")
	}
	t.Cleanup(func() { muxRun = prevRun })

	err := muxFiles(context.Background(), "/v.webm", "/a.m4a", "/out/x.mkv")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Codec mismatch") {
		t.Errorf("expected stderr in error message, got: %v", err)
	}
}

func TestMuxAvailable(t *testing.T) {
	prev := muxLookPath
	t.Cleanup(func() { muxLookPath = prev })

	muxLookPath = func(string) (string, error) { return "/usr/bin/ffmpeg", nil }
	if !muxAvailable() {
		t.Error("expected available")
	}
	muxLookPath = func(string) (string, error) { return "", errors.New("no") }
	if muxAvailable() {
		t.Error("expected unavailable")
	}
}

func TestLimitedWriter_BytesUnderLimit(t *testing.T) {
	buf := &bytes.Buffer{}
	lw := &limitedWriter{w: buf, remaining: 10}
	n, err := lw.Write([]byte("hello"))
	if err != nil || n != 5 {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if buf.String() != "hello" {
		t.Errorf("buf = %q", buf.String())
	}
}

func TestLimitedWriter_BytesOverLimit(t *testing.T) {
	buf := &bytes.Buffer{}
	lw := &limitedWriter{w: buf, remaining: 5}
	_, _ = lw.Write([]byte("hello world"))
	// First call should write 5, then drop the rest.
	if buf.String() != "hello" {
		t.Errorf("buf = %q, want %q", buf.String(), "hello")
	}
	// Subsequent writes silently drop.
	n, _ := lw.Write([]byte("more"))
	if n != 4 {
		t.Errorf("expected len-of-input return for dropped writes, got %d", n)
	}
	if buf.String() != "hello" {
		t.Errorf("buf changed unexpectedly: %q", buf.String())
	}
}

func TestTailString(t *testing.T) {
	if got := tailString("abc", 10); got != "abc" {
		t.Errorf("tailString short = %q", got)
	}
	if got := tailString("0123456789abcdef", 5); got != "...bcdef" {
		t.Errorf("tailString long = %q", got)
	}
	if got := tailString("  abc  ", 10); got != "abc" {
		t.Errorf("tailString trim = %q", got)
	}
}
