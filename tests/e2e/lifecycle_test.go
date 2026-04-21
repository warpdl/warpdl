//go:build e2e

package e2e

import (
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// TestLifecycle_DownloadListFlush
//
// Verifies the canonical three-step lifecycle:
//  1. Download a local 1 MB file to completion.
//  2. Confirm the completed item appears in `list -a`.
//  3. Flush it with `flush --force` and confirm `list` reports no downloads.
// ---------------------------------------------------------------------------

func TestLifecycle_DownloadListFlush(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	env := newTestEnv(t)
	env.startDaemon(t)

	// Download the test file synchronously (blocks until complete).
	_ = env.downloadAndVerify(t, ts.fileURL("/testfile.bin"), testFileSize)

	// The completed item must appear when --show-all is given.
	listOutput := env.run(t, "list", "-a")
	assertOutputContains(t, listOutput, "testfile.bin")

	// Flush all completed history without interactive confirmation.
	flushOutput := env.run(t, "flush", "--force")
	assertOutputContains(t, flushOutput, "Flushed")

	// After flush the list must be empty.
	emptyOutput := env.run(t, "list", "-a")
	assertOutputContains(t, emptyOutput, "no downloads found")
	assertOutputNotContains(t, emptyOutput, "testfile.bin")
}

// ---------------------------------------------------------------------------
// TestLifecycle_DownloadListFlush_SingleItem
//
// Like the above but uses `flush --force -i <hash>` to flush a specific item,
// leaving any other history untouched.  We download two files, flush only the
// first, and confirm the second still appears.
// ---------------------------------------------------------------------------

func TestLifecycle_DownloadListFlush_SingleItem(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	env := newTestEnv(t)
	env.startDaemon(t)

	// Download two distinct files.
	_ = env.downloadAndVerify(t, ts.fileURL("/testfile.bin"), testFileSize)
	_ = env.downloadAndVerify(t, ts.fileURL("/small.bin"), 1024)

	listOutput := env.run(t, "list", "-a")
	assertOutputContains(t, listOutput, "testfile.bin")
	assertOutputContains(t, listOutput, "small.bin")

	// Extract the hash belonging to testfile.bin.
	hash := extractHashFromLineContaining(listOutput, "testfile.bin")
	if hash == "" {
		t.Fatal("could not extract hash for testfile.bin from list output")
	}

	// Flush only that item.
	flushOutput := env.run(t, "flush", "--force", "-i", hash)
	assertOutputContains(t, flushOutput, hash)

	// small.bin must still be present; testfile.bin must be gone.
	afterOutput := env.run(t, "list", "-a")
	assertOutputContains(t, afterOutput, "small.bin")
	assertOutputNotContains(t, afterOutput, "testfile.bin")
}

// ---------------------------------------------------------------------------
// TestLifecycle_DownloadStopResume
//
// Verifies stop/resume semantics against a throttled local server:
//  1. Start a background download of a slow medium-sized file.
//  2. Let a few chunks land, then `stop` it.
//  3. `resume` (foreground, blocking until done) and verify the final file.
// ---------------------------------------------------------------------------

func TestLifecycle_DownloadStopResume(t *testing.T) {
	t.Parallel()
	// Use a 10 MB file with 500 ms per-chunk latency so the download takes
	// several seconds and we have a reliable window to stop it mid-flight.
	ts := newTestServer(t)
	ts.setSlowLatency(500 * time.Millisecond)

	env := newTestEnv(t)
	env.startDaemon(t)

	// Start download in background (returns immediately after daemon accepts it).
	bgOutput := env.run(t, "download",
		ts.slowFileURL("/medium.bin"),
		"-l", env.DownloadDir,
		"-x", "2",
		"-s", "2",
		"--background",
	)
	assertOutputContains(t, bgOutput, "background")

	// Give the download time to start consuming bytes before we stop it.
	time.Sleep(3 * time.Second)

	// List to retrieve the item hash.
	listOutput := env.run(t, "list")
	hash := extractHashFromListOutput(listOutput)
	if hash == "" {
		t.Fatalf("no in-progress download found in list output:\n%s", listOutput)
	}

	// Stop the in-progress download.
	stopOutput := env.run(t, "stop", hash)
	assertOutputContains(t, stopOutput, "stopped")

	// Brief pause to let the daemon persist the stopped state.
	time.Sleep(500 * time.Millisecond)

	// Remove throttling so the remainder downloads quickly within commandTimeout.
	ts.setSlowLatency(0)

	// Resume without --background so the call blocks until completion.
	// Resume uses the stored URL which still points to /slow/medium.bin on the
	// same test server, but with latency set to 0 it will complete quickly.
	resumeOutput := env.run(t, "resume",
		hash,
		"-x", "4",
		"-s", "4",
	)
	assertOutputContains(t, resumeOutput, "WARP download")

	// Verify the completed file has the expected size.
	expectedPath := filepath.Join(env.DownloadDir, "medium.bin")
	assertFileExists(t, expectedPath)
	assertFileSize(t, expectedPath, 10*1024*1024)
}

// ---------------------------------------------------------------------------
// TestDaemon_StartStop
//
// Confirms the daemon can be started and that `queue status` succeeds while it
// is running, then verifies that `stop-daemon` shuts it down gracefully so
// that subsequent commands get a connection error.
// ---------------------------------------------------------------------------

func TestDaemon_StartStop(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)

	// startDaemon registers a cleanup that calls stopDaemon; we will also call
	// stop-daemon explicitly below to test the command itself.
	env.startDaemon(t)

	// `queue status` must succeed while the daemon is alive.
	queueOutput := env.run(t, "queue", "status")
	assertOutputContains(t, queueOutput, "Queue Status")

	// Stop the daemon through the CLI command.
	_ = env.run(t, "stop-daemon")

	// Wait for the process to exit and the socket to disappear.
	time.Sleep(1 * time.Second)

	// Any command that requires a live daemon must now fail.
	failOutput, err := env.runMayFail("list")
	if err == nil && !strings.Contains(strings.ToLower(failOutput), "error") &&
		!strings.Contains(strings.ToLower(failOutput), "connect") &&
		!strings.Contains(strings.ToLower(failOutput), "refused") &&
		!strings.Contains(strings.ToLower(failOutput), "no such") {
		t.Fatalf("expected connection error after daemon stopped, got exit=nil output:\n%s", failOutput)
	}
}

// ---------------------------------------------------------------------------
// TestDaemon_PersistState
//
// Confirms that the download manager's on-disk state survives a daemon
// restart.  A file is downloaded, the daemon is stopped cleanly, a new daemon
// instance is started, and `list -a` must still show the completed download.
// ---------------------------------------------------------------------------

func TestDaemon_PersistState(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	env := newTestEnv(t)

	// First daemon instance.
	env.startDaemon(t)

	_ = env.downloadAndVerify(t, ts.fileURL("/small.bin"), 1024)

	// Confirm entry exists before restart.
	beforeOutput := env.run(t, "list", "-a")
	assertOutputContains(t, beforeOutput, "small.bin")

	// Gracefully stop the daemon so its GOB state file is flushed.
	env.stopDaemon(t)

	// Give the OS time to release the socket path.
	time.Sleep(500 * time.Millisecond)

	// Start a second daemon instance against the same configDir so it reads
	// the persisted state file.
	env.startDaemon(t)

	afterOutput := env.run(t, "list", "-a")
	assertOutputContains(t, afterOutput, "small.bin")
}

// ---------------------------------------------------------------------------
// TestError_DaemonNotRunning
//
// Verifies that every user-facing command that requires a live daemon produces
// an intelligible connection error when the daemon has not been started.
// ---------------------------------------------------------------------------

func TestError_DaemonNotRunning(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	// Intentionally do NOT call env.startDaemon.

	commands := [][]string{
		{"list"},
		{"list", "-a"},
		{"queue", "status"},
		{"flush", "--force"},
	}

	for _, args := range commands {
		args := args // capture
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			output, _ := env.runMayFail(args...)
			lower := strings.ToLower(output)
			connected := strings.Contains(lower, "connect") ||
				strings.Contains(lower, "refused") ||
				strings.Contains(lower, "no such") ||
				strings.Contains(lower, "error") ||
				strings.Contains(lower, "new_client")
			if !connected {
				t.Errorf("expected connection error for command %v, got:\n%s", args, output)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestError_ServerDown
//
// Verifies that a download attempt against an unreachable server (closed port)
// is reported as an error rather than silently succeeding.
// ---------------------------------------------------------------------------

func TestError_ServerDown(t *testing.T) {
	t.Parallel()
	ts := newTestServer(t)
	env := newTestEnv(t)
	env.startDaemon(t)

	// Point at an HTTP error path that returns 404.
	badURL := ts.errorURL(404)

	output, err := env.runMayFail(
		"download", badURL,
		"-l", env.DownloadDir,
		"-x", "4",
		"-s", "4",
	)

	lower := strings.ToLower(output)
	isErr := err != nil ||
		strings.Contains(lower, "error") ||
		strings.Contains(lower, "404") ||
		strings.Contains(lower, "not found") ||
		strings.Contains(lower, "failed")
	if !isErr {
		t.Fatalf("expected error for server-down download, got exit=nil output:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// TestError_ServerDown_NoPort
//
// Verifies behaviour when the TCP port itself is not listening (ECONNREFUSED).
// ---------------------------------------------------------------------------

func TestError_ServerDown_NoPort(t *testing.T) {
	t.Parallel()
	env := newTestEnv(t)
	env.startDaemon(t)

	// Allocate a port, then close the listener — guarantees nothing is listening.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to allocate port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	deadURL := fmt.Sprintf("http://127.0.0.1:%d/file.bin", port)

	output, err := env.runMayFail(
		"download", deadURL,
		"-l", env.DownloadDir,
		"-x", "2",
		"-s", "2",
	)

	lower := strings.ToLower(output)
	isErr := err != nil ||
		strings.Contains(lower, "error") ||
		strings.Contains(lower, "connect") ||
		strings.Contains(lower, "refused") ||
		strings.Contains(lower, "failed")
	if !isErr {
		t.Fatalf("expected error for dead-port download, got exit=nil output:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// Private helpers local to this file
// ---------------------------------------------------------------------------

// extractHashFromLineContaining finds the first list-output line that contains
// the given substring and returns the hash column from that line.  The list
// format is: | N | Name | Hash | Status | Scheduled |
func extractHashFromLineContaining(output, needle string) string {
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, needle) {
			continue
		}
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 5 {
			continue
		}
		hash := strings.TrimSpace(parts[3])
		if hash != "" && hash != "Unique Hash" && !strings.Contains(hash, "-") {
			return hash
		}
	}
	return ""
}
