//go:build e2e

package e2e

import (
	"path/filepath"
	"testing"
	"time"
)

// TestScheduledDownload_StartAt verifies the one-shot --start-at path end to
// end against the local test server: the CLI registers the item as scheduled
// without starting it, the daemon's scheduler fires it at the trigger minute,
// and the assembled file matches the served bytes exactly.
//
// --start-at only carries minute precision, so the trigger is the first whole
// minute at least a few seconds away: enough slack for the CLI to reach the
// daemon on a loaded runner, while keeping the wait under roughly a minute.
func TestScheduledDownload_StartAt(t *testing.T) {
	t.Parallel()

	ts := newTestServer(t)
	env := newTestEnv(t)
	env.startDaemon(t)

	startAt := time.Now().Add(65 * time.Second).Truncate(time.Minute)
	output := env.run(t, "download",
		ts.fileURL("/testfile.bin"),
		"--start-at", startAt.Format("2006-01-02 15:04"),
		"-l", env.DownloadDir,
		"-x", "4",
		"-s", "4",
	)
	assertOutputContains(t, output, "Scheduled download")

	// Registration must list the pending item without starting the transfer.
	listOutput := env.run(t, "list")
	assertOutputContains(t, listOutput, "testfile.bin")

	filePath := filepath.Join(env.DownloadDir, "testfile.bin")
	expected := ts.expectedBytes("/testfile.bin")
	assertFileNotComplete(t, filePath, expected)

	// The scheduler must fire at the trigger minute and download every byte:
	// the deadline covers the remaining wait plus a margin for the transfer.
	assertFileContentWithin(t, filePath, expected, time.Until(startAt)+time.Minute)
	assertOutputContains(t, env.run(t, "list", "-a"), "testfile.bin")
}
