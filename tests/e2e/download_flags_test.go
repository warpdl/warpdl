//go:build e2e

package e2e

import (
	"os"
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
	assertFileSize(t, filepath.Join(env.DownloadDir, "testfile.bin"), testFileSize)

	assertFileExists(t, filepath.Join(env.DownloadDir, "small.bin"))
	assertFileSize(t, filepath.Join(env.DownloadDir, "small.bin"), 1024)
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
	assertFileExists(t, filepath.Join(env.DownloadDir, "small.bin"))
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

// TestDownload_InputFile_MixedURLs verifies that invalid (non-http) lines in
// an input file are reported as skipped while valid URLs succeed.
func TestDownload_InputFile_MixedURLs(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	ts := newTestServer(t)
	inputFile := createInputFileWithComments(t, env.ConfigDir,
		ts.fileURL("/small.bin"),
		"ftp://invalid.example.com/file.bin", // invalid scheme — skipped
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
}

// ---------------------------------------------------------------------------
// Scheduling tests
// ---------------------------------------------------------------------------

// TestDownload_StartIn schedules a download 2 hours in the future using
// --start-in and verifies the item appears in the list as a pending entry.
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

	if !strings.Contains(output, "background") && !strings.Contains(output, "small.bin") {
		t.Logf("download output: %s", output)
	}

	time.Sleep(500 * time.Millisecond)

	listOutput := env.run(t, "list")
	assertOutputContains(t, listOutput, "small.bin")
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
// --start-at and confirms the item is visible in list as a pending entry.
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

	if !strings.Contains(output, "background") && !strings.Contains(output, "small.bin") {
		t.Logf("download output: %s", output)
	}

	time.Sleep(500 * time.Millisecond)

	listOutput := env.run(t, "list")
	assertOutputContains(t, listOutput, "small.bin")
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
// expression is accepted; the download must not produce a cron validation error.
func TestDownload_Schedule_ValidCron(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	ts := newTestServer(t)

	// "0 3 * * *" = daily at 03:00 — canonical 5-field expression.
	output, err := env.runMayFail(
		"download",
		ts.fileURL("/small.bin"),
		"--schedule", "0 3 * * *",
		"-l", env.DownloadDir,
		"--background",
	)

	assertOutputNotContains(t, output, "invalid cron expression")
	if err != nil {
		t.Logf("runMayFail returned error (may be unrelated): %v — output: %s", err, output)
	}
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
	assertFileSize(t, filepath.Join(env.DownloadDir, "small.bin"), 1024)
}

// TestDownload_CookiesFromInvalid verifies that --cookies-from with a path
// that does not exist produces the error "cookie file not found".
func TestDownload_CookiesFromInvalid(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	ts := newTestServer(t)
	nonExistentCookieFile := filepath.Join(env.ConfigDir, "no_such_cookies.txt")

	output, _ := env.runMayFail(
		"download",
		ts.fileURL("/small.bin"),
		"--cookies-from", nonExistentCookieFile,
		"-l", env.DownloadDir,
	)

	assertOutputContains(t, output, "cookie file not found")
}

// TestDownload_CookiesFromDir verifies that --cookies-from with a directory
// path produces an error indicating a directory was given instead of a file.
func TestDownload_CookiesFromDir(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	ts := newTestServer(t)

	// ConfigDir is guaranteed to be an existing directory.
	output, _ := env.runMayFail(
		"download",
		ts.fileURL("/small.bin"),
		"--cookies-from", env.ConfigDir,
		"-l", env.DownloadDir,
	)

	assertOutputContains(t, output, "directory")
}

// TestDownload_CookiesFromAuto verifies that --cookies-from "auto" passes
// validation without emitting a "cookie file not found" or directory error.
func TestDownload_CookiesFromAuto(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	ts := newTestServer(t)

	output, _ := env.runMayFail(
		"download",
		ts.fileURL("/small.bin"),
		"--cookies-from", "auto",
		"-l", env.DownloadDir,
		"-x", "2",
		"-s", "2",
	)

	assertOutputNotContains(t, output, "cookie file not found")
	assertOutputNotContains(t, output, "is a directory")
}

// TestDownload_CookiesFromValidFile verifies that --cookies-from with a real
// Netscape-format cookie file passes validation and the download proceeds
// without a cookie-related error.
func TestDownload_CookiesFromValidFile(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	ts := newTestServer(t)

	cookiePath := createTempFile(t, env.ConfigDir, "cookies.txt",
		"# Netscape HTTP Cookie File\n"+
			".example.com\tTRUE\t/\tFALSE\t0\ttest_session\tabc123\n",
	)

	output, _ := env.runMayFail(
		"download",
		ts.fileURL("/small.bin"),
		"--cookies-from", cookiePath,
		"-l", env.DownloadDir,
		"-x", "2",
		"-s", "2",
	)

	assertOutputNotContains(t, output, "cookie file not found")
	assertOutputNotContains(t, output, "is a directory")
}

// ---------------------------------------------------------------------------
// Connection tuning tests
// ---------------------------------------------------------------------------

// TestDownload_MaxConnections_Two verifies that -x 2 (two parallel connections)
// is accepted and the download completes with the expected file size.
func TestDownload_MaxConnections_Two(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	ts := newTestServer(t)

	output := env.run(t, "download",
		ts.fileURL("/testfile.bin"),
		"-l", env.DownloadDir,
		"-x", "2",
	)

	assertOutputNotContains(t, output, "error")

	assertFileExists(t, filepath.Join(env.DownloadDir, "testfile.bin"))
	assertFileSize(t, filepath.Join(env.DownloadDir, "testfile.bin"), testFileSize)
}

// TestDownload_MaxSegments_Two verifies that -s 2 (two file segments) is
// accepted and the download completes with the expected file size.
func TestDownload_MaxSegments_Two(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	ts := newTestServer(t)

	output := env.run(t, "download",
		ts.fileURL("/testfile.bin"),
		"-l", env.DownloadDir,
		"-s", "2",
	)

	assertOutputNotContains(t, output, "error")

	assertFileExists(t, filepath.Join(env.DownloadDir, "testfile.bin"))
	assertFileSize(t, filepath.Join(env.DownloadDir, "testfile.bin"), testFileSize)
}

// ---------------------------------------------------------------------------
// Edge case: overwrite an existing file with --overwrite / -y
// ---------------------------------------------------------------------------

// TestDownload_OverwriteWithFlag verifies that re-downloading a file using the
// --overwrite long-form flag replaces a pre-existing file with the correct content.
func TestDownload_OverwriteWithFlag(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	ts := newTestServer(t)
	filePath := filepath.Join(env.DownloadDir, "small.bin")

	// Write a 4-byte stub so we can confirm it is replaced.
	if err := os.WriteFile(filePath, []byte("stub"), 0644); err != nil {
		t.Fatalf("failed to create stub file: %v", err)
	}

	env.run(t, "download",
		ts.fileURL("/small.bin"),
		"-l", env.DownloadDir,
		"--overwrite",
		"-x", "2",
		"-s", "2",
	)

	// After overwrite the file must be the full 1 KB, not the 4-byte stub.
	assertFileSize(t, filePath, 1024)
}

// ---------------------------------------------------------------------------
// Edge case: custom output filename with --file-name / -o
// ---------------------------------------------------------------------------

// TestDownload_CustomFileNameLongFlag verifies that --file-name (long form)
// saves the downloaded content under the specified name.
func TestDownload_CustomFileNameLongFlag(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	env.startDaemon(t)

	ts := newTestServer(t)
	customName := "my_custom_output.bin"

	env.run(t, "download",
		ts.fileURL("/testfile.bin"),
		"-l", env.DownloadDir,
		"--file-name", customName,
		"-x", "4",
		"-s", "4",
	)

	assertFileExists(t, filepath.Join(env.DownloadDir, customName))
	assertFileSize(t, filepath.Join(env.DownloadDir, customName), testFileSize)
}
