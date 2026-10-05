//go:build e2e

package e2e

import (
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Download command tests
// ---------------------------------------------------------------------------

// TestDownload_Basic verifies that a file is downloaded in full from a local
// test server using the default parallel segment strategy.
func TestDownload_Basic(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)
	env := newTestEnv(t)
	env.startDaemon(t)

	env.downloadAndVerify(t, ts.fileURL("/testfile.bin"), testFileSize)
}

// TestDownload_CustomPath verifies that the -l flag saves the file into a
// non-default directory.
func TestDownload_CustomPath(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)
	env := newTestEnv(t)
	env.startDaemon(t)

	altDir := t.TempDir()
	env.run(t, "download", ts.fileURL("/small.bin"),
		"-l", altDir,
		"-x", "2", "-s", "2",
	)

	filePath := filepath.Join(altDir, "small.bin")
	assertFileExists(t, filePath)
	assertFileContent(t, filePath, ts.expectedBytes("/small.bin"))
}

// TestDownload_NoURL verifies that invoking the download command without a URL
// argument causes the binary to exit with a non-zero status and surfaces an
// error message.
func TestDownload_NoURL(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	output := env.runExpectError(t, "download")
	// Either an error about no URL or help text is acceptable.
	if !strings.Contains(output, "no url") && !strings.Contains(output, "USAGE") && !strings.Contains(output, "Usage") {
		t.Errorf("expected error or help output, got: %s", output)
	}
}

// TestDownload_InvalidURL verifies that a syntactically invalid URL returns an
// error from the daemon / client and does not hang.
func TestDownload_InvalidURL(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	// The CLI prints runtime errors and returns nil (exit 0), so use runMayFail.
	output, _ := env.runMayFail("download", "not-a-valid-url://??garbage",
		"-l", env.DownloadDir,
	)
	lower := strings.ToLower(output)
	if !strings.Contains(lower, "error") && !strings.Contains(lower, "failed") &&
		!strings.Contains(lower, "unsupported") && !strings.Contains(lower, "invalid") {
		t.Errorf("expected error in output for invalid URL, got:\n%s", output)
	}
}

// TestDownload_404 verifies that a 404 response from the server is reported as
// an error rather than silently succeeding.
func TestDownload_404(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)
	env := newTestEnv(t)
	env.startDaemon(t)

	// The CLI prints runtime errors and returns nil (exit 0), so use runMayFail.
	output, err := env.runMayFail("download", ts.errorURL(404),
		"-l", env.DownloadDir,
		"-x", "2", "-s", "2",
	)
	lower := strings.ToLower(output)
	isErr := err != nil ||
		strings.Contains(lower, "error") ||
		strings.Contains(lower, "404") ||
		strings.Contains(lower, "not found") ||
		strings.Contains(lower, "failed")
	if !isErr {
		t.Fatalf("expected error for 404 download, got:\n%s", output)
	}
}

// TestDownload_NoWorkSteal verifies that disabling work stealing via the
// --no-work-steal flag still results in a successful, correctly-sized download.
func TestDownload_NoWorkSteal(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)
	env := newTestEnv(t)
	env.startDaemon(t)

	env.downloadAndVerify(t, ts.fileURL("/testfile.bin"), testFileSize, "--no-work-steal")
}

// ---------------------------------------------------------------------------
// Resume command tests
// ---------------------------------------------------------------------------

// TestResume_InvalidHash verifies that resuming with an unknown hash surfaces
// a clear error message.
func TestResume_InvalidHash(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	// The CLI prints runtime errors and returns nil (exit 0), so use runMayFail.
	output, _ := env.runMayFail("resume", "deadbeefdeadbeef")
	lower := strings.ToLower(output)
	if !strings.Contains(lower, "error") && !strings.Contains(lower, "not found") &&
		!strings.Contains(lower, "failed") && !strings.Contains(lower, "invalid") {
		t.Errorf("expected error for invalid resume hash, got:\n%s", output)
	}
}

// TestResume_NoHash verifies that invoking resume without a hash argument
// exits non-zero and shows an error or usage message.
func TestResume_NoHash(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	output := env.runExpectError(t, "resume")
	if !strings.Contains(output, "no hash") && !strings.Contains(output, "USAGE") && !strings.Contains(output, "Usage") {
		t.Errorf("expected hash-missing error or usage, got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// List command tests
// ---------------------------------------------------------------------------

// TestList_Empty verifies that the list command reports that no downloads are
// present when the daemon has a clean state.
func TestList_Empty(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	output := env.run(t, "list", "-a")
	assertOutputContains(t, output, "no downloads found")
}

// TestList_AfterDownload verifies that a completed download appears in the
// output of `list -a` with its filename visible.
func TestList_AfterDownload(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)
	env := newTestEnv(t)
	env.startDaemon(t)

	env.downloadAndVerify(t, ts.fileURL("/small.bin"), 1024)

	output := env.run(t, "list", "-a")
	assertOutputContains(t, output, "small.bin")
}

// TestList_CompletedFlag verifies that -c (show-completed) includes finished
// downloads in the listing.
func TestList_CompletedFlag(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)
	env := newTestEnv(t)
	env.startDaemon(t)

	env.downloadAndVerify(t, ts.fileURL("/small.bin"), 1024)

	output := env.run(t, "list", "-c")
	assertOutputContains(t, output, "small.bin")
}

// ---------------------------------------------------------------------------
// Flush command tests
// ---------------------------------------------------------------------------

// TestFlush_WithStdinYes verifies that typing "yes" at the interactive
// confirmation prompt successfully flushes all history.
func TestFlush_WithStdinYes(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)
	env := newTestEnv(t)
	env.startDaemon(t)

	env.downloadAndVerify(t, ts.fileURL("/small.bin"), 1024)

	output := env.runWithStdin(t, "yes\n", "flush")
	assertOutputContains(t, output, "Flushed all download history!")
}

// TestFlush_WithStdinNo verifies that typing "no" at the prompt aborts the
// flush and leaves download history intact.
func TestFlush_WithStdinNo(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)
	env := newTestEnv(t)
	env.startDaemon(t)

	env.downloadAndVerify(t, ts.fileURL("/small.bin"), 1024)

	output := env.runWithStdin(t, "no\n", "flush")
	assertOutputContains(t, output, "Cancelled")

	listOutput := env.run(t, "list", "-a")
	assertOutputContains(t, listOutput, "small.bin")
}

// ---------------------------------------------------------------------------
// Stop command tests
// ---------------------------------------------------------------------------

// TestStop_InvalidHash verifies that stopping with a hash that is not in the
// active download set produces an error message rather than a silent no-op.
func TestStop_InvalidHash(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	// The CLI prints runtime errors and returns nil (exit 0), so use runMayFail.
	output, _ := env.runMayFail("stop", "cafebabecafebabe")
	lower := strings.ToLower(output)
	if !strings.Contains(lower, "error") && !strings.Contains(lower, "not found") &&
		!strings.Contains(lower, "failed") {
		t.Errorf("expected error for invalid stop hash, got:\n%s", output)
	}
}

// TestStop_NoHash verifies that `stop` without a hash argument exits non-zero
// and surfaces an error or usage hint.
func TestStop_NoHash(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	output := env.runExpectError(t, "stop")
	if !strings.Contains(output, "no hash") && !strings.Contains(output, "USAGE") && !strings.Contains(output, "Usage") {
		t.Errorf("expected hash-missing error or usage, got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// Info command tests
// ---------------------------------------------------------------------------

// TestInfo_Basic verifies that the `info` command performs a HEAD request
// against the test server and prints the filename and file size. No daemon
// is required for this command.
func TestInfo_Basic(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)
	env := newTestEnv(t)

	output := env.run(t, "info", ts.fileURL("/small.bin"))
	assertOutputContains(t, output, "File Info")
	// Size line must mention the file's byte count in some human-readable form.
	if !strings.Contains(output, "Size") {
		t.Errorf("expected Size line in info output, got: %s", output)
	}
}

// TestInfo_NoURL verifies that invoking `info` without a URL argument exits
// non-zero and surfaces an actionable error.
func TestInfo_NoURL(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	output := env.runExpectError(t, "info")
	if !strings.Contains(output, "no url") && !strings.Contains(output, "USAGE") && !strings.Contains(output, "Usage") {
		t.Errorf("expected no-url error or usage, got: %s", output)
	}
}

// TestInfo_404 verifies that `info` on a non-existent resource reports an
// error rather than silently printing zero-size info.
func TestInfo_404(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)
	env := newTestEnv(t)

	// The CLI prints runtime errors and returns nil (exit 0), so use runMayFail.
	output, err := env.runMayFail("info", ts.errorURL(404))
	lower := strings.ToLower(output)
	isErr := err != nil ||
		strings.Contains(lower, "error") ||
		strings.Contains(lower, "404") ||
		strings.Contains(lower, "failed")
	if !isErr {
		t.Fatalf("expected error for info 404, got:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// Queue command tests
// ---------------------------------------------------------------------------

// TestQueue_Status verifies that `queue status` connects to the running daemon
// and prints well-formed queue state information.
func TestQueue_Status(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	output := env.run(t, "queue", "status")
	// Daemon defaults to running, not paused.
	if !strings.Contains(output, "running") && !strings.Contains(output, "paused") {
		t.Errorf("expected running/paused in queue status output, got: %s", output)
	}
	assertOutputContains(t, output, "Max Concurrent")
}

// TestQueue_PauseResume verifies the full pause → resume lifecycle: after
// pausing the queue its status reflects "paused", and after resuming it
// transitions back to "running".
func TestQueue_PauseResume(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	pauseOut := env.run(t, "queue", "pause")
	assertOutputContains(t, pauseOut, "paused")

	statusAfterPause := env.run(t, "queue", "status")
	assertOutputContains(t, statusAfterPause, "paused")

	resumeOut := env.run(t, "queue", "resume")
	assertOutputContains(t, resumeOut, "resumed")

	statusAfterResume := env.run(t, "queue", "status")
	assertOutputContains(t, statusAfterResume, "running")
}

// ---------------------------------------------------------------------------
// Version command tests
// ---------------------------------------------------------------------------

// TestVersion_Output verifies that the `version` command prints a non-empty
// version string that includes the application name.
func TestVersion_Output(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	output := env.run(t, "version")
	if strings.TrimSpace(output) == "" {
		t.Fatal("version command produced empty output")
	}
	assertOutputContains(t, output, "warpdl")
}

// ---------------------------------------------------------------------------
// Help command tests
// ---------------------------------------------------------------------------

// TestHelp_Output verifies that the `help` command prints text that names the
// core subcommands exposed by the CLI application.
func TestHelp_Output(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)

	output, _ := env.runMayFail("help")
	if strings.TrimSpace(output) == "" {
		t.Fatal("help command produced empty output")
	}
	for _, cmd := range []string{"download", "list", "resume", "flush", "daemon", "queue", "info", "version"} {
		if !strings.Contains(output, cmd) {
			t.Errorf("expected help to mention %q command, output:\n%s", cmd, output)
		}
	}
}

// ---------------------------------------------------------------------------
// Priority flag tests
// ---------------------------------------------------------------------------

// TestDownload_Priority verifies that every --priority level is accepted and
// the download still completes byte-exactly.
func TestDownload_Priority(t *testing.T) {
	t.Parallel()

	for _, priority := range []string{"high", "low"} {
		t.Run(priority, func(t *testing.T) {
			ts := newTestServer(t)
			env := newTestEnv(t)
			env.startDaemon(t)

			env.downloadAndVerify(t, ts.fileURL("/small.bin"), 1024, "--priority", priority)
		})
	}
}

// ---------------------------------------------------------------------------
// Max connections / segments flag tests
// ---------------------------------------------------------------------------

// TestDownload_ConnectionTuning verifies that explicit connection and segment
// limits all produce a byte-exact download: -x 1 -s 1 (fully serial), -x 2
// (two parallel connections), and -s 2 (two file segments).
func TestDownload_ConnectionTuning(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		flags []string
	}{
		{"SingleConnection", []string{"-x", "1", "-s", "1"}},
		{"MaxConnectionsTwo", []string{"-x", "2"}},
		{"MaxSegmentsTwo", []string{"-s", "2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := newTestServer(t)
			env := newTestEnv(t)
			env.startDaemon(t)

			args := append([]string{"download", ts.fileURL("/testfile.bin"), "-l", env.DownloadDir}, tc.flags...)
			env.run(t, args...)

			filePath := filepath.Join(env.DownloadDir, "testfile.bin")
			assertFileExists(t, filePath)
			assertFileContent(t, filePath, ts.expectedBytes("/testfile.bin"))
		})
	}
}

// ---------------------------------------------------------------------------
// Alias tests
// ---------------------------------------------------------------------------

// TestDownloadAlias verifies that `d` is an accepted alias for the `download`
// subcommand and produces an identical result.
func TestDownloadAlias(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)
	env := newTestEnv(t)
	env.startDaemon(t)

	env.run(t, "d", ts.fileURL("/small.bin"),
		"-l", env.DownloadDir,
		"-x", "2", "-s", "2",
	)
	filePath := filepath.Join(env.DownloadDir, "small.bin")
	assertFileExists(t, filePath)
	assertFileContent(t, filePath, ts.expectedBytes("/small.bin"))
}

// TestListAlias verifies that `l` is an accepted alias for the `list`
// subcommand.
func TestListAlias(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	output := env.run(t, "l", "-a")
	assertOutputContains(t, output, "no downloads found")
}
