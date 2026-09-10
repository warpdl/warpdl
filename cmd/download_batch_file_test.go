package cmd

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/common"
)

// newBatchDownloadContext builds a *cli.Context wired for the download
// command's batch path: it registers the flags downloadBatchFromFile
// reads and parses the supplied raw args. The command
// name is "download" so the no-URL guard inside download() does not
// short-circuit (the input-file flag satisfies it).
func newBatchDownloadContext(app *cli.App, rawArgs []string) *cli.Context {
	set := flag.NewFlagSet("download", flag.ContinueOnError)
	set.String("input-file", "", "")
	set.Bool("background", false, "")
	set.Bool("overwrite", false, "")
	set.Bool("no-work-steal", false, "")
	set.String("priority", "normal", "")
	set.String("ssh-key", "", "")
	set.String("speed-limit", "", "")
	set.String("start-at", "", "")
	set.String("start-in", "", "")
	set.String("schedule", "", "")
	_ = set.Parse(rawArgs)
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: "download", Flags: dlFlags}
	return ctx
}

func TestBuildDownloadOptsIncludesBatchFlags(t *testing.T) {
	app := cli.NewApp()
	ctx := newBatchDownloadContext(app, []string{
		"--overwrite",
		"--no-work-steal",
		"-priority", "high",
		"-ssh-key", "/keys/id",
		"-speed-limit", "2MB",
	})

	oldForce, oldConns, oldParts := forceParts, maxConns, maxParts
	oldProxy, oldTimeout := proxyURL, timeout
	oldRetries, oldDelay := maxRetries, retryDelay
	forceParts, maxConns, maxParts = true, 7, 9
	proxyURL, timeout = "http://proxy.example:8080", 12
	maxRetries, retryDelay = 3, 250
	defer func() {
		forceParts, maxConns, maxParts = oldForce, oldConns, oldParts
		proxyURL, timeout = oldProxy, oldTimeout
		maxRetries, retryDelay = oldRetries, oldDelay
	}()

	opts := buildDownloadOpts(ctx, nil, "2030-01-01 10:00", "0 2 * * *")
	if !opts.Overwrite || !opts.DisableWorkStealing || !opts.ForceParts {
		t.Fatalf("boolean batch flags not propagated: %+v", opts)
	}
	if opts.MaxConnections != 7 || opts.MaxSegments != 9 || opts.Timeout != 12 {
		t.Fatalf("transfer limits not propagated: %+v", opts)
	}
	if opts.MaxRetries != 3 || opts.RetryDelay != 250 || opts.SpeedLimit != "2MB" {
		t.Fatalf("retry/speed flags not propagated: %+v", opts)
	}
	if opts.Priority != 2 || opts.SSHKeyPath != "/keys/id" || opts.Proxy != proxyURL {
		t.Fatalf("priority/transport flags not propagated: %+v", opts)
	}
	if opts.StartAt != "2030-01-01 10:00" || opts.Schedule != "0 2 * * *" {
		t.Fatalf("schedule flags not propagated: %+v", opts)
	}
}

func TestDownloadBatchFromFileRejectsSingleFileName(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	inputFile := createTempInputFile(t, "http://example.com/a.bin\n")
	ctx := newBatchDownloadContext(cli.NewApp(), []string{
		"--background",
		"-input-file", inputFile,
	})

	oldDlPath, oldFileName := dlPath, fileName
	dlPath, fileName = t.TempDir(), "one-name.bin"
	defer func() { dlPath, fileName = oldDlPath, oldFileName }()

	var gotErr error
	stdout, stderr := captureOutput(func() { gotErr = download(ctx) })
	assertExitError(t, gotErr)
	if !strings.Contains(stdout+stderr, "--file-name cannot be used with --input-file") {
		t.Fatalf("expected explicit filename rejection, got:\n%s%s", stdout, stderr)
	}
}

// TestDownloadBatchFromFile_Background drives download() down the batch
// path with --background set. The background branch submits each URL to
// the fake daemon (UPDATE_DOWNLOAD) and returns immediately without the
// post-submission wait loop, so this is the fast, fully-deterministic
// way to cover downloadBatchFromFile's setup, option-building, summary
// printing, and the background early-return.
func TestDownloadBatchFromFile_Background(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	content := "http://example.com/a.bin\nhttp://example.com/b.bin\n"
	inputFile := createTempInputFile(t, content)

	app := cli.NewApp()
	ctx := newBatchDownloadContext(app, []string{
		"--background",
		"-input-file", inputFile,
	})

	oldDlPath, oldFileName := dlPath, fileName
	dlPath = t.TempDir()
	fileName = ""
	defer func() {
		dlPath = oldDlPath
		fileName = oldFileName
	}()

	stdout, _ := captureOutput(func() {
		if err := download(ctx); err != nil {
			t.Errorf("download (batch): %v", err)
		}
	})

	if !strings.Contains(stdout, "Initiating WARP batch download") {
		t.Fatalf("expected batch banner, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Batch Download Summary") {
		t.Fatalf("expected batch summary, got:\n%s", stdout)
	}
	// Two URLs both succeed against the fake daemon.
	if !strings.Contains(stdout, "Succeeded:  2") {
		t.Fatalf("expected 2 successes, got:\n%s", stdout)
	}
}

func TestDownloadBatchFromFile_ScheduledDoesNotAttach(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	inputFile := createTempInputFile(t, "http://example.com/a.bin\n")
	ctx := newBatchDownloadContext(cli.NewApp(), []string{
		"-input-file", inputFile,
		"-start-in", "2h",
	})

	oldDlPath, oldFileName := dlPath, fileName
	dlPath, fileName = t.TempDir(), ""
	defer func() { dlPath, fileName = oldDlPath, oldFileName }()

	stdout, _ := captureOutput(func() {
		if err := download(ctx); err != nil {
			t.Errorf("scheduled batch download: %v", err)
		}
	})
	if !strings.Contains(stdout, "Scheduled batch submissions") {
		t.Fatalf("scheduled batch did not return after registration:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Succeeded:  1") {
		t.Fatalf("scheduled submission was not reported as accepted:\n%s", stdout)
	}
}

func TestDownloadBatchFromFile_SubmissionFailureReturnsExitError(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_DOWNLOAD: "submission rejected",
	})
	defer srv.close()

	inputFile := createTempInputFile(t, "http://example.com/a.bin\n")
	ctx := newBatchDownloadContext(cli.NewApp(), []string{
		"--background",
		"-input-file", inputFile,
	})

	oldDlPath, oldFileName := dlPath, fileName
	dlPath, fileName = t.TempDir(), ""
	defer func() { dlPath, fileName = oldDlPath, oldFileName }()

	var gotErr error
	stdout, stderr := captureOutput(func() { gotErr = download(ctx) })
	assertExitError(t, gotErr)
	if !strings.Contains(stdout, "Failed:     1") {
		t.Fatalf("batch summary did not report the failure:\n%s", stdout)
	}
	if !strings.Contains(stderr, "1 of 1 downloads failed") {
		t.Fatalf("batch failure did not reach stderr:\n%s", stderr)
	}
}

func TestWaitForBatchSubmission_UnknownSizeCompletesFromTerminalEvent(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	submission := BatchSubmission{
		DownloadID:    "id",
		SavePath:      filepath.Join(t.TempDir(), "not-created"),
		ContentLength: -1,
	}
	if err := waitForBatchSubmission(submission); err != nil {
		t.Fatalf("unknown-size terminal completion was reported as failure: %v", err)
	}
}

// TestDownloadBatchFromFile_WaitCompletesImmediately drives the foreground
// batch path through a terminal completion notification. A same-sized local
// file is deliberately present to ensure it is not treated as authoritative
// before the daemon reports success.
func TestDownloadBatchFromFile_WaitCompletesImmediately(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	// The fake daemon returns SavePath "file.bin" (relative). download()
	// resolves the download dir but the submission's SavePath comes
	// straight from the daemon reply, so isBatchSubmissionComplete stats
	// "file.bin" relative to the process CWD. Run from a temp CWD and
	// drop a correctly-sized file there.
	tmpCWD := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(tmpCWD); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	if err := os.WriteFile(filepath.Join(tmpCWD, "file.bin"), make([]byte, 10), 0o644); err != nil {
		t.Fatalf("seed complete file: %v", err)
	}

	content := "http://example.com/a.bin\n"
	inputFile := createTempInputFile(t, content)

	app := cli.NewApp()
	ctx := newBatchDownloadContext(app, []string{"-input-file", inputFile})

	oldDlPath, oldFileName := dlPath, fileName
	dlPath = tmpCWD
	fileName = ""
	defer func() {
		dlPath = oldDlPath
		fileName = oldFileName
	}()

	stdout, _ := captureOutput(func() {
		if err := download(ctx); err != nil {
			t.Errorf("download (batch wait): %v", err)
		}
	})

	if !strings.Contains(stdout, "Batch Download Summary") {
		t.Fatalf("expected batch summary, got:\n%s", stdout)
	}
	// The single submission was already complete on disk, so it remains
	// a success (no conversion to error).
	if !strings.Contains(stdout, "Succeeded:  1") {
		t.Fatalf("expected 1 success, got:\n%s", stdout)
	}
}

// TestDownloadBatchFromFile_WaitAttachThenManagerComplete drives the
// foreground batch wait path through its attach branch. The submission's
// on-disk file is absent, so isBatchSubmissionComplete is false and
// waitForBatchSubmission dials a fresh client and calls AttachDownload.
// The fake daemon answers UPDATE_ATTACH and pushes a DownloadComplete for
// MAIN_HASH, which the batch wait handler turns into a Disconnect,
// ending Listen(). The post-Listen grace check then asks the daemon's
// manager (UPDATE_LIST): the fake reports the matching download as fully
// downloaded, so the grace returns complete and the submission stays a
// success. This exercises the attach → Listen → manager-complete arc
// deterministically (no fixed-duration grace wait is hit because the
// manager reports completion on the first poll).
func TestDownloadBatchFromFile_WaitAttachThenManagerComplete(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	// Run from a temp CWD where "file.bin" (the fake SavePath) does NOT
	// exist, so the disk check fails and the attach path is taken.
	tmpCWD := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(tmpCWD); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	content := "http://example.com/a.bin\n"
	inputFile := createTempInputFile(t, content)

	app := cli.NewApp()
	ctx := newBatchDownloadContext(app, []string{"-input-file", inputFile})

	oldDlPath, oldFileName := dlPath, fileName
	dlPath = tmpCWD
	fileName = ""
	defer func() {
		dlPath = oldDlPath
		fileName = oldFileName
	}()

	stdout, _ := captureOutput(func() {
		if err := download(ctx); err != nil {
			t.Errorf("download (batch attach): %v", err)
		}
	})
	if !strings.Contains(stdout, "Batch Download Summary") {
		t.Fatalf("expected batch summary, got:\n%s", stdout)
	}
	// The daemon's manager reports the download complete, so the
	// submission remains a success after the wait.
	if !strings.Contains(stdout, "Succeeded:  1") {
		t.Fatalf("expected 1 success after attach+manager-complete, got:\n%s", stdout)
	}
}

// TestDownloadBatchFromFile_WaitAttachErrorThenManagerComplete covers the
// attach-failure recovery arm of waitForBatchSubmission. The fake daemon
// fails UPDATE_ATTACH, so AttachDownload errors; waitForBatchSubmission
// then closes the client and falls back to the grace check, which queries
// the manager (UPDATE_LIST). The fake reports the matching download as
// complete, so the grace returns true and the submission stays a success
// despite the attach failure. This exercises the
// "AttachDownload error → grace → manager-complete" path deterministically.
func TestDownloadBatchFromFile_WaitAttachErrorThenManagerComplete(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_ATTACH: "attach failed",
	})
	defer srv.close()

	tmpCWD := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(tmpCWD); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	content := "http://example.com/a.bin\n"
	inputFile := createTempInputFile(t, content)

	app := cli.NewApp()
	ctx := newBatchDownloadContext(app, []string{"-input-file", inputFile})

	oldDlPath, oldFileName := dlPath, fileName
	dlPath = tmpCWD
	fileName = ""
	defer func() {
		dlPath = oldDlPath
		fileName = oldFileName
	}()

	stdout, _ := captureOutput(func() {
		if err := download(ctx); err != nil {
			t.Errorf("download (batch attach-error): %v", err)
		}
	})
	if !strings.Contains(stdout, "Batch Download Summary") {
		t.Fatalf("expected batch summary, got:\n%s", stdout)
	}
	// Attach failed but the manager reports completion, so the
	// submission is still counted as a success.
	if !strings.Contains(stdout, "Succeeded:  1") {
		t.Fatalf("expected 1 success after attach-error grace, got:\n%s", stdout)
	}
}

// TestDownloadBatchFromFile_ResolvePathError exercises the
// resolveDownloadPath failure branch inside downloadBatchFromFile: an
// invalid download directory makes resolveDownloadPath return a non-zero
// CLI error.
func TestDownloadBatchFromFile_ResolvePathError(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	content := "http://example.com/a.bin\n"
	inputFile := createTempInputFile(t, content)

	// Point the download dir at a regular file so directory validation
	// fails inside resolveDownloadPath.
	notADir := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	app := cli.NewApp()
	ctx := newBatchDownloadContext(app, []string{
		"--background",
		"-input-file", inputFile,
	})

	oldDlPath, oldFileName := dlPath, fileName
	dlPath = notADir
	fileName = ""
	defer func() {
		dlPath = oldDlPath
		fileName = oldFileName
	}()

	var gotErr error
	stdout, stderr := captureOutput(func() { gotErr = download(ctx) })
	assertExitError(t, gotErr)
	combined := stdout + stderr
	if !strings.Contains(combined, "resolve_path") {
		t.Fatalf("expected resolve_path error, got:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
}

// TestDownloadBatchFromFile_InvalidProxy exercises the proxy-validation
// failure branch inside downloadBatchFromFile.
func TestDownloadBatchFromFile_InvalidProxy(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	content := "http://example.com/a.bin\n"
	inputFile := createTempInputFile(t, content)

	app := cli.NewApp()
	ctx := newBatchDownloadContext(app, []string{
		"--background",
		"-input-file", inputFile,
	})

	oldDlPath, oldFileName, oldProxy := dlPath, fileName, proxyURL
	dlPath = t.TempDir()
	fileName = ""
	proxyURL = "://invalid"
	defer func() {
		dlPath = oldDlPath
		fileName = oldFileName
		proxyURL = oldProxy
	}()

	var gotErr error
	stdout, stderr := captureOutput(func() { gotErr = download(ctx) })
	assertExitError(t, gotErr)
	combined := stdout + stderr
	if !strings.Contains(combined, "invalid_proxy") {
		t.Fatalf("expected invalid_proxy error, got:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
}

// TestDownloadBatchFromFile_BatchDownloadError exercises the
// DownloadBatch error branch: a missing input file makes ParseInputFile
// (inside DownloadBatch) fail, which downloadBatchFromFile reports with a
// non-zero batch_download error.
func TestDownloadBatchFromFile_BatchDownloadError(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	missing := filepath.Join(t.TempDir(), "does-not-exist.txt")

	app := cli.NewApp()
	ctx := newBatchDownloadContext(app, []string{
		"--background",
		"-input-file", missing,
	})

	oldDlPath, oldFileName := dlPath, fileName
	dlPath = t.TempDir()
	fileName = ""
	defer func() {
		dlPath = oldDlPath
		fileName = oldFileName
	}()

	var gotErr error
	stdout, stderr := captureOutput(func() { gotErr = download(ctx) })
	assertExitError(t, gotErr)
	combined := stdout + stderr
	if !strings.Contains(combined, "batch_download") {
		t.Fatalf("expected batch_download error, got:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
}

// TestDownloadBatchFromFile_WithDirectURL covers the branch that reports
// "Additional URLs" when positional URL args accompany the input file.
func TestDownloadBatchFromFile_WithDirectURL(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)
	srv := startFakeServer(t, socketPath)
	defer srv.close()

	content := "http://example.com/a.bin\n"
	inputFile := createTempInputFile(t, content)

	app := cli.NewApp()
	ctx := newBatchDownloadContext(app, []string{
		"--background",
		"-input-file", inputFile,
		"http://example.com/direct.bin",
	})

	oldDlPath, oldFileName := dlPath, fileName
	dlPath = t.TempDir()
	fileName = ""
	defer func() {
		dlPath = oldDlPath
		fileName = oldFileName
	}()

	stdout, _ := captureOutput(func() {
		if err := download(ctx); err != nil {
			t.Errorf("download: %v", err)
		}
	})
	if !strings.Contains(stdout, "Additional URLs: 1") {
		t.Fatalf("expected additional-URLs line, got:\n%s", stdout)
	}
	// One file URL + one direct URL = 2 total submissions.
	if !strings.Contains(stdout, "Succeeded:  2") {
		t.Fatalf("expected 2 successes, got:\n%s", stdout)
	}
}

// Compile-time guard: the fake server must keep handling UPDATE_DOWNLOAD
// for the batch submissions above. This references the constant so a
// rename forces a test update.
var _ = common.UPDATE_DOWNLOAD
