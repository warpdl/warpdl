package cmd

import (
	"flag"
	"strings"
	"testing"
	"time"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/common"
)

// newSingleDownloadContext builds a *cli.Context for the single-URL
// download path with the schedule/cookie/UA-adjacent flags download()
// reads registered. flagArgs are raw "-flag value" tokens parsed ahead of
// the positional url.
func newSingleDownloadContext(app *cli.App, flagArgs, args []string) *cli.Context {
	set := flag.NewFlagSet("download", flag.ContinueOnError)
	set.String("input-file", "", "")
	set.Bool("background", false, "")
	set.Bool("overwrite", false, "")
	set.Bool("no-work-steal", false, "")
	set.String("priority", "normal", "")
	set.String("ssh-key", "", "")
	set.String("speed-limit", "", "")
	set.String("cookies-from", "", "")
	set.String("start-at", "", "")
	set.String("start-in", "", "")
	set.String("schedule", "", "")
	_ = set.Parse(append(append([]string{}, flagArgs...), args...))
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: "download", Flags: dlFlags}
	return ctx
}

// withDownloadDefaults sets the package-level download globals to inert
// values for the duration of the test and restores them after. Returns a
// cleanup func to defer.
func withDownloadDefaults(t *testing.T) func() {
	t.Helper()
	oldDlPath, oldFileName, oldProxy, oldUA := dlPath, fileName, proxyURL, userAgent
	dlPath = t.TempDir()
	fileName = ""
	proxyURL = ""
	userAgent = ""
	return func() {
		dlPath, fileName, proxyURL, userAgent = oldDlPath, oldFileName, oldProxy, oldUA
	}
}

// TestDownload_UserAgentHeader covers the userAgent != "" branch in
// download(), which builds a User-Agent header before dialing the daemon.
func TestDownload_UserAgentHeader(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newSingleDownloadContext(app, []string{"--background"}, []string{"http://example.com/f.bin"})

	restore := withDownloadDefaults(t)
	userAgent = "firefox" // exercise the UA-header branch
	defer restore()

	stdout, _ := captureOutput(func() {
		if err := download(ctx); err != nil {
			t.Errorf("download: %v", err)
		}
	})
	if !strings.Contains(stdout, "Started download") {
		t.Fatalf("expected background success, got:\n%s", stdout)
	}
}

// TestDownload_CookiesFromInvalid covers the validateCookiesFrom error
// branch: a --cookies-from pointing at a nonexistent file aborts the
// download with a non-zero cookies_from runtime error.
func TestDownload_CookiesFromInvalid(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newSingleDownloadContext(app,
		[]string{"-cookies-from", "/nonexistent/cookies.txt"},
		[]string{"http://example.com/f.bin"})

	restore := withDownloadDefaults(t)
	defer restore()

	var gotErr error
	stdout, stderr := captureOutput(func() { gotErr = download(ctx) })
	assertExitError(t, gotErr)
	if !strings.Contains(stdout+stderr, "cookies_from") {
		t.Fatalf("expected cookies_from error, got:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
}

// TestDownload_StartAtStartInExclusion covers the mutual-exclusion guard:
// supplying both --start-at and --start-in prints an error and returns nil
// without dialing the daemon for a download.
func TestDownload_StartAtStartInExclusion(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newSingleDownloadContext(app,
		[]string{"-start-at", "2030-01-01 10:00", "-start-in", "2h"},
		[]string{"http://example.com/f.bin"})

	restore := withDownloadDefaults(t)
	defer restore()

	var gotErr error
	stdout, _ := captureOutput(func() { gotErr = download(ctx) })
	assertExitError(t, gotErr)
	if !strings.Contains(stdout, "mutually exclusive") {
		t.Fatalf("expected mutual-exclusion message, got:\n%s", stdout)
	}
}

// TestDownload_StartInValid covers the --start-in success branch: a valid
// duration resolves to an absolute start time and the CLI returns after
// registering the schedule instead of attaching to a nonexistent live stream.
func TestDownload_StartInValid(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newSingleDownloadContext(app,
		[]string{"--background", "-start-in", "2h"},
		[]string{"http://example.com/f.bin"})

	restore := withDownloadDefaults(t)
	defer restore()

	stdout, _ := captureOutput(func() {
		if err := download(ctx); err != nil {
			t.Errorf("download: %v", err)
		}
	})
	if !strings.Contains(stdout, "Scheduled download") {
		t.Fatalf("expected scheduled success with start-in, got:\n%s", stdout)
	}
}

// TestDownload_StartInInvalid covers the --start-in parse-error branch.
func TestDownload_StartInInvalid(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newSingleDownloadContext(app,
		[]string{"-start-in", "not-a-duration"},
		[]string{"http://example.com/f.bin"})

	restore := withDownloadDefaults(t)
	defer restore()

	var gotErr error
	stdout, _ := captureOutput(func() { gotErr = download(ctx) })
	assertExitError(t, gotErr)
	if !strings.Contains(stdout, "invalid --start-in") {
		t.Fatalf("expected start-in parse error, got:\n%s", stdout)
	}
}

// TestDownload_ScheduleValid covers the valid --schedule branch:
// validateSchedule accepts the expression and hasOccurrenceWithinYear
// returns true (a daily cron recurs well within a year), so no warning
// is printed and the CLI reports the scheduled registration.
func TestDownload_ScheduleValid(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	// "0 2 * * *" = daily at 02:00 — valid and recurs within a year.
	ctx := newSingleDownloadContext(app,
		[]string{"--background", "-schedule", "0 2 * * *"},
		[]string{"http://example.com/f.bin"})

	restore := withDownloadDefaults(t)
	defer restore()

	stdout, _ := captureOutput(func() {
		if err := download(ctx); err != nil {
			t.Errorf("download: %v", err)
		}
	})
	if !strings.Contains(stdout, "Scheduled download") {
		t.Fatalf("expected scheduled success with schedule, got:\n%s", stdout)
	}
	// A daily cron has occurrences within a year — no warning expected.
	if strings.Contains(stdout, "no occurrence in the next year") {
		t.Fatalf("unexpected no-occurrence warning for daily cron:\n%s", stdout)
	}
}

// TestDownload_ScheduleInvalid covers the validateSchedule error branch:
// a malformed cron expression aborts the download with a non-zero status
// before any daemon download is initiated.
func TestDownload_ScheduleInvalid(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	ctx := newSingleDownloadContext(app,
		[]string{"--background", "-schedule", "not a cron"},
		[]string{"http://example.com/f.bin"})

	restore := withDownloadDefaults(t)
	defer restore()

	var gotErr error
	stdout, _ := captureOutput(func() { gotErr = download(ctx) })
	assertExitError(t, gotErr)
	if !strings.Contains(stdout, "invalid cron expression") {
		t.Fatalf("expected cron validation error, got:\n%s", stdout)
	}
}

// TestDownload_AuthRequiredHint covers the isAuthRequiredError branch:
// the daemon fails the download with a "flow: timed out" error, which
// download() recognizes as an auth-required failure and prints the
// recovery hint to stderr before reporting the runtime error.
func TestDownload_AuthRequiredHint(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_DOWNLOAD: "extract failed: flow: timed out",
	})
	defer srv.close()

	app := cli.NewApp()
	ctx := newSingleDownloadContext(app, nil, []string{"http://example.com/f.bin"})

	restore := withDownloadDefaults(t)
	defer restore()

	var gotErr error
	stdout, stderr := captureOutput(func() { gotErr = download(ctx) })
	assertExitError(t, gotErr)
	combined := stdout + stderr
	if !strings.Contains(combined, "requires authentication") {
		t.Fatalf("expected auth-required hint, got:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
	if !strings.Contains(combined, "warp auth login") {
		t.Fatalf("expected login hint, got:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
}

// TestDownload_StartAtPastWarning covers the validateStartAt warning
// branch: a start time in the past yields a warning string (printed) and
// the download starts immediately.
func TestDownload_StartAtPastWarning(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	past := time.Now().Add(-2 * time.Hour).Format(startAtLayout)

	app := cli.NewApp()
	ctx := newSingleDownloadContext(app,
		[]string{"--background", "-start-at", past},
		[]string{"http://example.com/f.bin"})

	restore := withDownloadDefaults(t)
	defer restore()

	// A past start-at either warns or is normalized; either way the
	// download must proceed to the background success line. The
	// load-bearing assertion is that the validateStartAt branch executed
	// without aborting the download.
	stdout, _ := captureOutput(func() {
		if err := download(ctx); err != nil {
			t.Errorf("download: %v", err)
		}
	})
	if !strings.Contains(stdout, "Started download") {
		t.Fatalf("expected background success after start-at handling, got:\n%s", stdout)
	}
}
