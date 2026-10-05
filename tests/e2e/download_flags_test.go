//go:build e2e

package e2e

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Batch download tests (--input-file / -i)
// ---------------------------------------------------------------------------

// TestDownload_InputFile verifies that --input-file downloads all listed URLs
// and emits a batch summary with the correct totals.
func TestDownload_InputFile(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	ts := newTestServer(t)
	inputFile := createInputFile(t, env.ConfigDir,
		ts.fileURL("/testfile.bin"),
		ts.fileURL("/small.bin"),
	)

	output := env.run(t, "download",
		"-i", inputFile,
		"-l", env.DownloadDir,
		"-x", "4",
		"-s", "4",
	)

	assertOutputContains(t, output, "Batch Download Summary")
	assertOutputContains(t, output, "Total URLs: 2")
	assertOutputContains(t, output, "Succeeded:  2")
	assertOutputContains(t, output, "Failed:     0")

	assertFileExists(t, filepath.Join(env.DownloadDir, "testfile.bin"))
	assertFileContent(t, filepath.Join(env.DownloadDir, "testfile.bin"), ts.expectedBytes("/testfile.bin"))

	assertFileExists(t, filepath.Join(env.DownloadDir, "small.bin"))
	assertFileContent(t, filepath.Join(env.DownloadDir, "small.bin"), ts.expectedBytes("/small.bin"))
}

// TestDownload_InputFile_Comments verifies that # comment lines and blank
// lines in an input file are silently skipped while real URLs are downloaded.
func TestDownload_InputFile_Comments(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	ts := newTestServer(t)
	inputFile := createInputFileWithComments(t, env.ConfigDir,
		"# This is a comment",
		ts.fileURL("/testfile.bin"),
		"# Another comment",
		"",
		ts.fileURL("/small.bin"),
		"# End of file",
	)

	output := env.run(t, "download",
		"-i", inputFile,
		"-l", env.DownloadDir,
		"-x", "4",
		"-s", "4",
	)

	assertOutputContains(t, output, "Batch Download Summary")
	assertOutputContains(t, output, "Total URLs: 2")
	assertOutputContains(t, output, "Succeeded:  2")

	assertFileExists(t, filepath.Join(env.DownloadDir, "testfile.bin"))
	assertFileContent(t, filepath.Join(env.DownloadDir, "testfile.bin"), ts.expectedBytes("/testfile.bin"))
	assertFileExists(t, filepath.Join(env.DownloadDir, "small.bin"))
	assertFileContent(t, filepath.Join(env.DownloadDir, "small.bin"), ts.expectedBytes("/small.bin"))
}

// TestDownload_InputFile_NotFound verifies that a nonexistent file path passed
// to --input-file produces an error referencing the missing path.
func TestDownload_InputFile_NotFound(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	missingPath := filepath.Join(env.ConfigDir, "does_not_exist.txt")

	// The CLI prints runtime errors and returns nil (exit 0), so use runMayFail.
	output, _ := env.runMayFail("download",
		"-i", missingPath,
		"-l", env.DownloadDir,
	)

	if !strings.Contains(output, "not found") && !strings.Contains(output, missingPath) &&
		!strings.Contains(output, "no such file") && !strings.Contains(output, "does not exist") {
		t.Errorf("expected error referencing missing file, got: %s", output)
	}
}

// TestDownload_InputFile_OnlyComments verifies that an input file with only
// comment / blank lines causes the CLI to report an empty-file condition
// rather than silently succeeding with zero downloads.
func TestDownload_InputFile_OnlyComments(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	inputFile := createInputFileWithComments(t, env.ConfigDir,
		"# First comment",
		"# Second comment",
		"",
	)

	output, _ := env.runMayFail(
		"download",
		"-i", inputFile,
		"-l", env.DownloadDir,
	)

	if !strings.Contains(output, "no valid URLs") && !strings.Contains(output, "empty") {
		t.Errorf("expected empty-file error, got: %s", output)
	}
}

// TestDownload_InputFile_MixedURLs verifies that unsupported URL schemes in
// an input file are reported as skipped while valid URLs succeed.
func TestDownload_InputFile_MixedURLs(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	ts := newTestServer(t)
	inputFile := createInputFileWithComments(t, env.ConfigDir,
		ts.fileURL("/small.bin"),
		"gopher://invalid.example.com/file.bin", // unsupported scheme — skipped
	)

	output := env.run(t, "download",
		"-i", inputFile,
		"-l", env.DownloadDir,
		"-x", "2",
		"-s", "2",
	)

	assertOutputContains(t, output, "Batch Download Summary")
	assertOutputContains(t, output, "Succeeded:  1")
	assertOutputContains(t, output, "Skipped")

	assertFileExists(t, filepath.Join(env.DownloadDir, "small.bin"))
	assertFileContent(t, filepath.Join(env.DownloadDir, "small.bin"), ts.expectedBytes("/small.bin"))
}

// ---------------------------------------------------------------------------
// Scheduling tests
// ---------------------------------------------------------------------------

// TestDownload_StartIn schedules a download 2 hours in the future using
// --start-in and verifies the CLI reports the registration, the item is listed
// as pending, and the transfer does not start early.
func TestDownload_StartIn(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	ts := newTestServer(t)

	output := env.run(t, "download",
		ts.fileURL("/small.bin"),
		"--start-in", "2h",
		"-l", env.DownloadDir,
		"--background",
	)
	assertOutputContains(t, output, "Scheduled download")

	listOutput := env.run(t, "list")
	assertOutputContains(t, listOutput, "small.bin")
	assertFileNotComplete(t, filepath.Join(env.DownloadDir, "small.bin"), ts.expectedBytes("/small.bin"))
}

// TestDownload_StartIn_InvalidDuration verifies that a garbage string passed
// to --start-in is rejected with an appropriate error message.
func TestDownload_StartIn_InvalidDuration(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	ts := newTestServer(t)

	output, _ := env.runMayFail(
		"download",
		ts.fileURL("/small.bin"),
		"--start-in", "not-a-duration",
		"-l", env.DownloadDir,
	)

	assertOutputContains(t, output, "invalid --start-in")
}

// TestDownload_StartAtAndStartIn verifies that supplying both --start-at and
// --start-in is rejected because the flags are mutually exclusive.
func TestDownload_StartAtAndStartIn(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	ts := newTestServer(t)
	futureAt := time.Now().Add(3 * time.Hour).Format("2006-01-02 15:04")

	output, _ := env.runMayFail(
		"download",
		ts.fileURL("/small.bin"),
		"--start-at", futureAt,
		"--start-in", "30m",
		"-l", env.DownloadDir,
	)

	assertOutputContains(t, output, "mutually exclusive")
}

// TestDownload_StartAt_FutureTime schedules a download 3 minutes ahead via
// --start-at and verifies the CLI reports the registration, the item is listed
// as pending, and the transfer does not start early.
func TestDownload_StartAt_FutureTime(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	ts := newTestServer(t)
	futureAt := time.Now().Add(3 * time.Minute).Format("2006-01-02 15:04")

	output := env.run(t, "download",
		ts.fileURL("/small.bin"),
		"--start-at", futureAt,
		"-l", env.DownloadDir,
		"--background",
	)
	assertOutputContains(t, output, "Scheduled download")

	listOutput := env.run(t, "list")
	assertOutputContains(t, listOutput, "small.bin")
	assertFileNotComplete(t, filepath.Join(env.DownloadDir, "small.bin"), ts.expectedBytes("/small.bin"))
}

// TestDownload_Schedule_InvalidCron verifies that an invalid cron expression
// supplied via --schedule results in an informative error.
func TestDownload_Schedule_InvalidCron(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	ts := newTestServer(t)

	output, _ := env.runMayFail(
		"download",
		ts.fileURL("/small.bin"),
		"--schedule", "not-a-cron",
		"-l", env.DownloadDir,
	)

	assertOutputContains(t, output, "invalid cron expression")
}

// TestDownload_Schedule_ValidCron verifies that a well-formed 5-field cron
// expression is accepted and registered as a recurring schedule: the item is
// listed with its cron expression and nothing is downloaded before the first
// occurrence.
func TestDownload_Schedule_ValidCron(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	ts := newTestServer(t)

	// "0 3 * * *" = daily at 03:00 — canonical 5-field expression.
	output := env.run(t, "download",
		ts.fileURL("/small.bin"),
		"--schedule", "0 3 * * *",
		"-l", env.DownloadDir,
		"--background",
	)
	assertOutputContains(t, output, "Scheduled download")
	assertOutputNotContains(t, output, "invalid cron expression")

	// The pending item carries its recurrence, and no bytes have been fetched.
	listOutput := env.run(t, "list")
	assertOutputContains(t, listOutput, "small.bin")
	assertOutputContains(t, listOutput, "recurring")
	assertFileNotComplete(t, filepath.Join(env.DownloadDir, "small.bin"), ts.expectedBytes("/small.bin"))
}

// ---------------------------------------------------------------------------
// Cookie tests
// ---------------------------------------------------------------------------

// TestDownload_CookieFlag verifies that the global --cookie flag is accepted
// (multiple values) and does not trigger a cookie-parsing error.
func TestDownload_CookieFlag(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	ts := newTestServer(t)

	// --cookie is a global flag; it must precede the subcommand name.
	output := env.run(t,
		"--cookie", "session=abc123",
		"--cookie", "pref=dark",
		"download",
		ts.fileURL("/small.bin"),
		"-l", env.DownloadDir,
		"-x", "2",
		"-s", "2",
	)

	assertOutputNotContains(t, output, "parse_cookies")

	assertFileExists(t, filepath.Join(env.DownloadDir, "small.bin"))
	assertFileContent(t, filepath.Join(env.DownloadDir, "small.bin"), ts.expectedBytes("/small.bin"))
}

// ---------------------------------------------------------------------------
// Edge case: overwrite an existing file with --overwrite / -y
// ---------------------------------------------------------------------------

// TestDownload_Overwrite verifies that both the short -y and long --overwrite
// flags replace a pre-existing file with the full, byte-exact download.
func TestDownload_Overwrite(t *testing.T) {
	t.Parallel()

	for _, flag := range []string{"-y", "--overwrite"} {
		t.Run(flag, func(t *testing.T) {
			env := newTestEnv(t)
			env.startDaemon(t)

			ts := newTestServer(t)
			filePath := filepath.Join(env.DownloadDir, "small.bin")

			// A stub of a different size and content must be fully replaced.
			createDummyFile(t, filePath, 512)
			assertFileSize(t, filePath, 512)

			env.run(t, "download",
				ts.fileURL("/small.bin"),
				"-l", env.DownloadDir,
				flag,
				"-x", "2",
				"-s", "2",
			)

			assertFileContent(t, filePath, ts.expectedBytes("/small.bin"))
		})
	}
}

// ---------------------------------------------------------------------------
// Edge case: custom output filename with --file-name / -o
// ---------------------------------------------------------------------------

// TestDownload_CustomFileName verifies that both the short -o and long
// --file-name flags save the download under the requested name with the served
// bytes intact.
func TestDownload_CustomFileName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		flag string
		out  string
	}{
		{"-o", "renamed-output.bin"},
		{"--file-name", "my_custom_output.bin"},
	}
	for _, tc := range cases {
		t.Run(tc.out, func(t *testing.T) {
			env := newTestEnv(t)
			env.startDaemon(t)

			ts := newTestServer(t)

			env.run(t, "download",
				ts.fileURL("/testfile.bin"),
				"-l", env.DownloadDir,
				tc.flag, tc.out,
				"-x", "4",
				"-s", "4",
			)

			filePath := filepath.Join(env.DownloadDir, tc.out)
			assertFileExists(t, filePath)
			assertFileContent(t, filePath, ts.expectedBytes("/testfile.bin"))
		})
	}
}
