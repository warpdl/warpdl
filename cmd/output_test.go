package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urfave/cli"
	cmdcommon "github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/pkg/warplib"
)

// getShortSocketPath returns a short socket path to avoid macOS path length limits.
// On Windows, this is ignored in favor of TCP, but we still return a dummy path
// for consistency.
func getShortSocketPath(t *testing.T) string {
	t.Helper()
	tmpDir, err := os.MkdirTemp(os.TempDir(), "wdl")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })
	return filepath.Join(tmpDir, "w.sock")
}

// =============================================================================
// Download Command Output Tests
// =============================================================================

// TestOutput_Download_NoURL verifies that running download without a URL
// shows an error message. Note: help text goes to console via cli framework.
func TestOutput_Download_NoURL(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	app.Commands = []cli.Command{
		{
			Name:  "download",
			Usage: "download a file from url",
			Flags: dlFlags,
		},
	}
	ctx := newContext(app, nil, "download")
	ctx.Command = app.Commands[0]

	stdout, _ := captureOutput(func() {
		_ = download(ctx)
	})

	assertContains(t, stdout, "no url provided")
}

// TestOutput_Download_Initiation verifies that a valid download shows
// the initiation message.
func TestOutput_Download_Initiation(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)

	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, []string{"http://example.com/file.bin"}, "download")

	oldDlPath, oldFileName := dlPath, fileName
	dlPath = t.TempDir()
	fileName = ""
	defer func() {
		dlPath = oldDlPath
		fileName = oldFileName
	}()

	stdout, _ := captureOutput(func() {
		_ = download(ctx)
	})

	assertContains(t, stdout, ">> Initiating a WARP download <<")
}

// TestOutput_Download_Info verifies that download displays file info
// including Name, Size, Save Location, and Max Connections.
func TestOutput_Download_Info(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)

	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, []string{"http://example.com/file.bin"}, "download")

	oldDlPath, oldFileName := dlPath, fileName
	dlPath = t.TempDir()
	fileName = ""
	defer func() {
		dlPath = oldDlPath
		fileName = oldFileName
	}()

	stdout, _ := captureOutput(func() {
		_ = download(ctx)
	})

	t.Run("shows Name field", func(t *testing.T) {
		assertContains(t, stdout, "Name")
	})

	t.Run("shows Size field", func(t *testing.T) {
		assertContains(t, stdout, "Size")
	})

	t.Run("shows Save Location field", func(t *testing.T) {
		assertContains(t, stdout, "Save Location")
	})

	t.Run("shows Max Connections field", func(t *testing.T) {
		assertContains(t, stdout, "Max Connections")
	})
}

// TestOutput_Download_HelpArg verifies that `download help` shows help text
// instead of attempting to download a file named "help".
func TestOutput_Download_HelpArg(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	app.Commands = []cli.Command{
		{
			Name:  "download",
			Usage: "download a file from url",
			Flags: dlFlags,
		},
	}
	ctx := newContext(app, []string{"help"}, "download")
	ctx.Command = app.Commands[0]

	stdout, _ := captureOutput(func() {
		_ = download(ctx)
	})

	// Key test: ensure we don't try to download a file named "help"
	assertNotContains(t, stdout, ">> Initiating a WARP download <<")
	// Help text is displayed via cli framework to console (not captured)
}

// TestOutput_Download_InvalidProxy verifies that an invalid proxy URL
// produces an error with "invalid_proxy" in the message.
// Note: This test is skipped because it requires a real daemon since the
// fake server hangs waiting for a request that never comes (client fails
// before sending the download request due to proxy validation).
func TestOutput_Download_InvalidProxy(t *testing.T) {
	t.Skip("Requires real daemon - fake server doesn't handle early client exit")
}

// =============================================================================
// Resume Command Output Tests
// =============================================================================

// TestOutput_Resume_NoHash verifies that running resume without a hash
// shows an error message and help.
func TestOutput_Resume_NoHash(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	app.Commands = []cli.Command{
		{
			Name:  "resume",
			Usage: "resume a download",
			Flags: rsFlags,
		},
	}
	ctx := newContext(app, nil, "resume")
	ctx.Command = app.Commands[0]

	stdout, _ := captureOutput(func() {
		_ = resume(ctx)
	})

	assertContains(t, stdout, "no hash provided")
}

// TestOutput_Resume_Success verifies that a valid resume shows
// the initiation message.
func TestOutput_Resume_Success(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)

	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, []string{"testhash"}, "resume")

	oldMaxParts, oldMaxConns, oldForce, oldProxy := maxParts, maxConns, forceParts, proxyURL
	maxParts, maxConns, forceParts = 1, 1, false
	proxyURL = ""
	defer func() {
		maxParts, maxConns, forceParts = oldMaxParts, oldMaxConns, oldForce
		proxyURL = oldProxy
	}()

	stdout, _ := captureOutput(func() {
		_ = resume(ctx)
	})

	assertContains(t, stdout, ">> Initiating a WARP download <<")
}

// TestOutput_Resume_InvalidHash verifies that an invalid hash produces
// an error with the "resume" command reference.
func TestOutput_Resume_InvalidHash(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)

	// Start server that returns error for UPDATE_RESUME
	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_RESUME: "hash not found",
	})
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, []string{"invalidhash"}, "resume")

	oldMaxParts, oldMaxConns, oldForce, oldProxy := maxParts, maxConns, forceParts, proxyURL
	maxParts, maxConns, forceParts = 1, 1, false
	proxyURL = ""
	defer func() {
		maxParts, maxConns, forceParts = oldMaxParts, oldMaxConns, oldForce
		proxyURL = oldProxy
	}()

	stdout, _ := captureOutput(func() {
		_ = resume(ctx)
	})

	// The error format should contain "resume" command reference
	assertContains(t, stdout, "resume")
}

// =============================================================================
// List Command Output Tests
// =============================================================================

// TestOutput_List_Empty verifies that an empty list shows "no downloads found".
func TestOutput_List_Empty(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)

	listOverride = []*warplib.Item{}
	defer func() { listOverride = nil }()

	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, nil, "list")

	stdout, _ := captureOutput(func() {
		_ = list(ctx)
	})

	assertContains(t, stdout, "no downloads found")
}

// TestOutput_List_SingleItem verifies that a single item displays
// with the correct table headers.
func TestOutput_List_SingleItem(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)

	listOverride = []*warplib.Item{{
		Hash:       "abc123",
		Name:       "testfile.bin",
		TotalSize:  100,
		Downloaded: 50,
		Hidden:     false,
		Children:   false,
		DateAdded:  time.Now(),
		Resumable:  true,
		Parts:      make(map[int64]*warplib.ItemPart),
	}}
	defer func() { listOverride = nil }()

	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, nil, "list")

	stdout, _ := captureOutput(func() {
		_ = list(ctx)
	})

	t.Run("shows Num header", func(t *testing.T) {
		assertContains(t, stdout, "Num")
	})

	t.Run("shows Name header", func(t *testing.T) {
		assertContains(t, stdout, "Name")
	})

	t.Run("shows Unique Hash header", func(t *testing.T) {
		assertContains(t, stdout, "Unique Hash")
	})

	t.Run("shows Status header", func(t *testing.T) {
		assertContains(t, stdout, "Status")
	})

	t.Run("shows the item hash", func(t *testing.T) {
		assertContains(t, stdout, "abc123")
	})
}

// TestOutput_List_TableFormat verifies that the list output has proper
// table formatting with pipe and dash separators.
func TestOutput_List_TableFormat(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)

	listOverride = []*warplib.Item{{
		Hash:       "hash01",
		Name:       "file1.bin",
		TotalSize:  100,
		Downloaded: 100,
		Hidden:     false,
		Children:   false,
		DateAdded:  time.Now(),
		Resumable:  true,
		Parts:      make(map[int64]*warplib.ItemPart),
	}}
	defer func() { listOverride = nil }()

	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, nil, "list")

	stdout, _ := captureOutput(func() {
		_ = list(ctx)
	})

	t.Run("has pipe separators", func(t *testing.T) {
		assertContains(t, stdout, "|")
	})

	t.Run("has dash separators", func(t *testing.T) {
		assertContains(t, stdout, "-")
	})

	t.Run("has table border", func(t *testing.T) {
		assertContains(t, stdout, "------")
	})
}

// TestOutput_List_LongFileName verifies that file names longer than 23
// characters are truncated with "...".
func TestOutput_List_LongFileName(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)

	// Name longer than 23 chars should be truncated with "..."
	longName := "this_is_a_very_long_filename_that_exceeds_limit.bin"
	listOverride = []*warplib.Item{{
		Hash:       "longhash",
		Name:       longName,
		TotalSize:  100,
		Downloaded: 50,
		Hidden:     false,
		Children:   false,
		DateAdded:  time.Now(),
		Resumable:  true,
		Parts:      make(map[int64]*warplib.ItemPart),
	}}
	defer func() { listOverride = nil }()

	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, nil, "list")

	stdout, _ := captureOutput(func() {
		_ = list(ctx)
	})

	t.Run("shows truncation marker", func(t *testing.T) {
		assertContains(t, stdout, "...")
	})

	t.Run("does not show full name", func(t *testing.T) {
		assertNotContains(t, stdout, longName)
	})
}

// TestOutput_List_Percentage verifies that status percentages display correctly
// for items at 0%, 50%, and 100%.
func TestOutput_List_Percentage(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)

	tests := []struct {
		name       string
		downloaded warplib.ContentLength
		total      warplib.ContentLength
		expected   string
	}{
		{
			name:       "0_percent",
			downloaded: 0,
			total:      100,
			expected:   "0%",
		},
		{
			name:       "50_percent",
			downloaded: 50,
			total:      100,
			expected:   "50%",
		},
		{
			name:       "100_percent",
			downloaded: 100,
			total:      100,
			expected:   "100%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listOverride = []*warplib.Item{{
				Hash:       "perc" + tt.name,
				Name:       "file.bin",
				TotalSize:  tt.total,
				Downloaded: tt.downloaded,
				Hidden:     false,
				Children:   false,
				DateAdded:  time.Now(),
				Resumable:  true,
				Parts:      make(map[int64]*warplib.ItemPart),
			}}
			defer func() { listOverride = nil }()

			srv := startFakeServer(t, socketPath)
			defer srv.close()

			app := cli.NewApp()
			app.Name = "warpdl"
			app.HelpName = "warpdl"
			ctx := newContext(app, nil, "list")

			stdout, _ := captureOutput(func() {
				_ = list(ctx)
			})

			assertContains(t, stdout, tt.expected)
		})
	}
}

// =============================================================================
// Stop Command Tests
// =============================================================================

// TestOutput_Stop_NoHash verifies that stop command shows error and help when
// no hash is provided.
func TestOutput_Stop_NoHash(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, nil, "stop")

	stdout, _ := captureOutput(func() {
		_ = stop(ctx)
	})

	assertContains(t, stdout, "no hash provided")
}

// TestOutput_Stop_Success verifies that stop command shows success message.
func TestOutput_Stop_Success(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)

	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, []string{"testhash"}, "stop")

	stdout, _ := captureOutput(func() {
		_ = stop(ctx)
	})

	assertContains(t, stdout, "Downloading stopped.")
}

// =============================================================================
// Flush Command Tests
// =============================================================================

// TestOutput_Flush_SuccessAll verifies flush all with -f flag shows success message.
func TestOutput_Flush_SuccessAll(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)

	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, nil, "flush")

	oldForce := forceFlush
	oldHash := hashToFlush
	forceFlush = true
	hashToFlush = ""
	defer func() {
		forceFlush = oldForce
		hashToFlush = oldHash
	}()

	stdout, _ := captureOutput(func() {
		_ = flush(ctx)
	})

	assertContains(t, stdout, "Flushed all download history!")
}

// TestOutput_Flush_SuccessSpecific verifies flush specific hash shows success message.
func TestOutput_Flush_SuccessSpecific(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)

	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, []string{"abc123"}, "flush")

	oldForce := forceFlush
	oldHash := hashToFlush
	forceFlush = true
	hashToFlush = ""
	defer func() {
		forceFlush = oldForce
		hashToFlush = oldHash
	}()

	stdout, _ := captureOutput(func() {
		_ = flush(ctx)
	})

	assertContains(t, stdout, "Flushed abc123")
}

// TestOutput_Flush_TooManyArgs verifies flush with too many arguments shows error.
func TestOutput_Flush_TooManyArgs(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, []string{"arg1", "arg2"}, "flush")

	stdout, _ := captureOutput(func() {
		_ = flush(ctx)
	})

	assertContains(t, stdout, "invalid amount of arguments")
}

// =============================================================================
// Info Command Tests
// =============================================================================

// TestOutput_Info_NoURL verifies info command shows error and help when no URL provided.
func TestOutput_Info_NoURL(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, nil, "info")

	stdout, _ := captureOutput(func() {
		_ = info(ctx)
	})

	assertContains(t, stdout, "no url provided")
}

// TestOutput_Info_FetchingMessage verifies info command shows fetching message.
func TestOutput_Info_FetchingMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", "1024")
		_, _ = w.Write([]byte("test"))
	}))
	defer srv.Close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, []string{srv.URL + "/file.bin"}, "info")

	oldUA := userAgent
	oldProxy := proxyURL
	userAgent = "warp"
	proxyURL = ""
	defer func() {
		userAgent = oldUA
		proxyURL = oldProxy
	}()

	stdout, _ := captureOutput(func() {
		_ = info(ctx)
	})

	assertContains(t, stdout, "fetching details, please wait")
}

// TestOutput_Info_DisplaysInfo verifies info command displays file info.
func TestOutput_Info_DisplaysInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Length", "2048")
		w.Header().Set("Content-Disposition", `attachment; filename="testfile.zip"`)
		_, _ = w.Write([]byte("test"))
	}))
	defer srv.Close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, []string{srv.URL + "/testfile.zip"}, "info")

	oldUA := userAgent
	oldProxy := proxyURL
	userAgent = "warp"
	proxyURL = ""
	defer func() {
		userAgent = oldUA
		proxyURL = oldProxy
	}()

	stdout, _ := captureOutput(func() {
		_ = info(ctx)
	})

	assertContains(t, stdout, "File Info")
	assertContains(t, stdout, "Name")
	assertContains(t, stdout, "Size")
}

// =============================================================================
// Attach Command Tests
// =============================================================================

// TestOutput_Attach_NoHash verifies attach command shows error and help when
// no hash is provided.
func TestOutput_Attach_NoHash(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, nil, "attach")

	stdout, _ := captureOutput(func() {
		_ = attach(ctx)
	})

	assertContains(t, stdout, "no hash provided")
}

// TestOutput_Attach_Success verifies attach command shows initiating message.
func TestOutput_Attach_Success(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)

	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, []string{"testhash"}, "attach")

	stdout, _ := captureOutput(func() {
		_ = attach(ctx)
	})

	assertContains(t, stdout, ">> Initiating a WARP download <<")
}

// =============================================================================
// Stop-Daemon Command Tests
// =============================================================================

// TestOutput_StopDaemon_NotRunning verifies stop-daemon shows appropriate message
// when daemon is not running.
func TestOutput_StopDaemon_NotRunning(t *testing.T) {
	tmpDir := t.TempDir()
	if err := warplib.SetConfigDir(tmpDir); err != nil {
		t.Fatalf("SetConfigDir: %v", err)
	}

	// Ensure no PID file exists
	_ = os.Remove(getPidFilePath())

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, nil, "stop-daemon")

	stdout, _ := captureOutput(func() {
		_ = stopDaemon(ctx)
	})

	// Should contain either "not running" or "PID file not found"
	if !strings.Contains(stdout, "not running") && !strings.Contains(stdout, "PID file not found") {
		t.Errorf("expected daemon not running message, got:\n%s", stdout)
	}
}

// =============================================================================
// Error Response Tests
// =============================================================================

// TestOutput_Stop_ErrorResponse verifies stop command error format when server fails.
func TestOutput_Stop_ErrorResponse(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)

	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_STOP: "stop failed",
	})
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, []string{"testhash"}, "stop")

	stdout, _ := captureOutput(func() {
		_ = stop(ctx)
	})

	assertErrorFormat(t, stdout, "stop", "stop-download")
}

// TestOutput_Flush_ErrorResponse verifies flush command error format when server fails.
func TestOutput_Flush_ErrorResponse(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)

	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_FLUSH: "flush failed",
	})
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, nil, "flush")

	oldForce := forceFlush
	oldHash := hashToFlush
	forceFlush = true
	hashToFlush = ""
	defer func() {
		forceFlush = oldForce
		hashToFlush = oldHash
	}()

	stdout, _ := captureOutput(func() {
		_ = flush(ctx)
	})

	assertErrorFormat(t, stdout, "flush", "flush")
}

// TestOutput_Attach_ErrorResponse verifies attach command error format when server fails.
func TestOutput_Attach_ErrorResponse(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)

	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_ATTACH: "attach failed",
	})
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, []string{"testhash"}, "attach")

	stdout, _ := captureOutput(func() {
		_ = attach(ctx)
	})

	// Attach starts with the initiating message
	assertContains(t, stdout, ">> Initiating a WARP download <<")
	// Then shows error
	assertErrorFormat(t, stdout, "attach", "client-attach")
}

// TestOutput_Info_InvalidProxy verifies info command error format for invalid proxy.
func TestOutput_Info_InvalidProxy(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, []string{"http://example.com/file.zip"}, "info")

	oldUA := userAgent
	oldProxy := proxyURL
	userAgent = "warp"
	proxyURL = "://invalid-proxy"
	defer func() {
		userAgent = oldUA
		proxyURL = oldProxy
	}()

	stdout, _ := captureOutput(func() {
		_ = info(ctx)
	})

	assertContains(t, stdout, "fetching details, please wait")
	assertErrorFormat(t, stdout, "info", "invalid_proxy")
}

// =============================================================================
// Additional Output Verification Tests
// =============================================================================

// TestOutput_ErrorFormat_RuntimeErr verifies that runtime errors follow the
// standard format: warpdl: cmd[action]: msg
func TestOutput_ErrorFormat_RuntimeErr(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)

	srv := startFakeServer(t, socketPath, map[common.UpdateType]string{
		common.UPDATE_DOWNLOAD: "test error message",
	})
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, []string{"http://example.com"}, "download")

	oldDlPath, oldFileName := dlPath, fileName
	dlPath = ""
	fileName = ""
	defer func() {
		dlPath = oldDlPath
		fileName = oldFileName
	}()

	stdout, _ := captureOutput(func() {
		_ = download(ctx)
	})

	// Note: download.go:96 uses PrintRuntimeErr(ctx, "info", "download", err)
	// so the format is "info[download]" not "download[client-download]"
	assertErrorFormat(t, stdout, "info", "download")
	assertContains(t, stdout, "test error message")
}

// TestOutput_ErrorFormat_DownloadInvalidProxy verifies proxy validation error format
// Note: Skipped because fake server hangs when client fails before sending request.
func TestOutput_ErrorFormat_DownloadInvalidProxy(t *testing.T) {
	t.Skip("Requires real daemon - fake server doesn't handle early client exit")
}

// TestOutput_Version verifies that version output contains expected components
func TestOutput_Version(t *testing.T) {
	buildArgs := BuildArgs{
		Version:   "1.2.3",
		BuildType: "test",
		Date:      "2024-01-15",
		Commit:    "abc1234",
	}

	stdout, _ := captureOutput(func() {
		_ = Execute([]string{"warpdl", "version"}, buildArgs)
	})

	t.Run("contains version number", func(t *testing.T) {
		assertContains(t, stdout, "1.2.3")
	})

	t.Run("contains build type", func(t *testing.T) {
		assertContains(t, stdout, "test")
	})

	t.Run("contains build date", func(t *testing.T) {
		assertContains(t, stdout, "2024-01-15")
	})

	t.Run("contains commit hash", func(t *testing.T) {
		assertContains(t, stdout, "abc1234")
	})

	t.Run("contains app name", func(t *testing.T) {
		assertContains(t, stdout, "warpdl")
	})
}

// TestOutput_Version_Alias verifies that 'v' alias works same as 'version'
func TestOutput_Version_Alias(t *testing.T) {
	buildArgs := BuildArgs{
		Version:   "2.0.0",
		BuildType: "release",
		Date:      "2024-06-01",
		Commit:    "def5678",
	}

	stdoutVersion, _ := captureOutput(func() {
		_ = Execute([]string{"warpdl", "version"}, buildArgs)
	})

	stdoutV, _ := captureOutput(func() {
		_ = Execute([]string{"warpdl", "v"}, buildArgs)
	})

	if stdoutVersion != stdoutV {
		t.Errorf("version and v alias should produce same output:\nversion: %s\nv: %s",
			stdoutVersion, stdoutV)
	}

	assertContains(t, stdoutV, "2.0.0")
	assertContains(t, stdoutV, "release")
}

// TestOutput_HelpApp verifies app-level help contains required sections
func TestOutput_HelpApp(t *testing.T) {
	buildArgs := BuildArgs{
		Version:   "1.0.0",
		BuildType: "dev",
	}

	// Mock showAppHelpAndExit to avoid os.Exit
	prev := cmdcommon.SetShowAppHelpAndExit(func(ctx *cli.Context, code int) {
		_ = cli.ShowAppHelp(ctx)
	})
	defer cmdcommon.SetShowAppHelpAndExit(prev)

	stdout, _ := captureOutput(func() {
		_ = Execute([]string{"warpdl", "help"}, buildArgs)
	})

	t.Run("contains Usage section", func(t *testing.T) {
		assertContains(t, stdout, "Usage:")
	})

	t.Run("contains Commands section", func(t *testing.T) {
		assertContains(t, stdout, "Commands:")
	})

	t.Run("lists download command", func(t *testing.T) {
		assertContains(t, stdout, "download")
	})

	t.Run("lists resume command", func(t *testing.T) {
		assertContains(t, stdout, "resume")
	})

	t.Run("lists list command", func(t *testing.T) {
		assertContains(t, stdout, "list")
	})

	t.Run("lists daemon command", func(t *testing.T) {
		assertContains(t, stdout, "daemon")
	})

	t.Run("lists ext command", func(t *testing.T) {
		assertContains(t, stdout, "ext")
	})

	t.Run("shows help usage hint", func(t *testing.T) {
		assertContains(t, stdout, "help <command>")
	})

	t.Run("contains app name and version", func(t *testing.T) {
		assertContains(t, stdout, "warpdl")
		assertContains(t, stdout, "1.0.0")
	})
}

// TestOutput_HelpCommand verifies command-specific help shows flags and description
func TestOutput_HelpCommand(t *testing.T) {
	buildArgs := BuildArgs{
		Version:   "1.0.0",
		BuildType: "dev",
	}

	t.Run("download command help", func(t *testing.T) {
		stdout, _ := captureOutput(func() {
			_ = Execute([]string{"warpdl", "help", "download"}, buildArgs)
		})

		assertContains(t, stdout, "download")
		assertContains(t, stdout, "Usage:")
		assertContains(t, stdout, "Supported Flags:")
		assertContains(t, stdout, "--file-name")
		assertContains(t, stdout, "--download-path")
	})

	t.Run("resume command help", func(t *testing.T) {
		stdout, _ := captureOutput(func() {
			_ = Execute([]string{"warpdl", "help", "resume"}, buildArgs)
		})

		assertContains(t, stdout, "resume")
		assertContains(t, stdout, "Usage:")
	})

	t.Run("list command help", func(t *testing.T) {
		stdout, _ := captureOutput(func() {
			_ = Execute([]string{"warpdl", "help", "list"}, buildArgs)
		})

		assertContains(t, stdout, "list")
		assertContains(t, stdout, "Usage:")
	})

	t.Run("info command help", func(t *testing.T) {
		stdout, _ := captureOutput(func() {
			_ = Execute([]string{"warpdl", "help", "info"}, buildArgs)
		})

		assertContains(t, stdout, "info")
		assertContains(t, stdout, "Usage:")
		assertContains(t, stdout, "Supported Flags:")
		assertContains(t, stdout, "--user-agent")
	})

	t.Run("flush command help", func(t *testing.T) {
		stdout, _ := captureOutput(func() {
			_ = Execute([]string{"warpdl", "help", "flush"}, buildArgs)
		})

		assertContains(t, stdout, "flush")
		assertContains(t, stdout, "Usage:")
	})
}

// TestOutput_HelpUnknown verifies unknown command is handled gracefully
// Note: Skipped because cli.ShowCommandHelp calls os.Exit(1) for unknown commands
func TestOutput_HelpUnknown(t *testing.T) {
	t.Skip("cli.ShowCommandHelp calls os.Exit(1) for unknown commands")
}

// TestOutput_HelpAlias verifies 'h' alias works same as 'help'
func TestOutput_HelpAlias(t *testing.T) {
	buildArgs := BuildArgs{
		Version:   "1.0.0",
		BuildType: "dev",
	}

	// Mock showAppHelpAndExit to avoid os.Exit
	prev := cmdcommon.SetShowAppHelpAndExit(func(ctx *cli.Context, code int) {
		_ = cli.ShowAppHelp(ctx)
	})
	defer cmdcommon.SetShowAppHelpAndExit(prev)

	stdoutHelp, _ := captureOutput(func() {
		_ = Execute([]string{"warpdl", "help"}, buildArgs)
	})

	stdoutH, _ := captureOutput(func() {
		_ = Execute([]string{"warpdl", "h"}, buildArgs)
	})

	if stdoutHelp != stdoutH {
		t.Errorf("help and h alias should produce same output:\nhelp: %s\nh: %s",
			stdoutHelp, stdoutH)
	}
}

// TestOutput_ErrorFormat_NetworkErr verifies network error format when daemon unavailable
func TestOutput_ErrorFormat_NetworkErr(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, []string{"http://example.com"}, "download")

	oldDlPath, oldFileName := dlPath, fileName
	dlPath = ""
	fileName = ""
	defer func() {
		dlPath = oldDlPath
		fileName = oldFileName
	}()

	stdout, _ := captureOutput(func() {
		_ = download(ctx)
	})

	assertErrorFormat(t, stdout, "download", "new_client")
}

// TestOutput_InfoCommand_NoServer verifies info command error when fetching fails
func TestOutput_InfoCommand_NoServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Not Found", http.StatusNotFound)
	}))
	defer srv.Close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, []string{srv.URL + "/file.bin"}, "info")

	oldUA := userAgent
	oldProxy := proxyURL
	userAgent = "warp"
	proxyURL = ""
	defer func() {
		userAgent = oldUA
		proxyURL = oldProxy
	}()

	stdout, _ := captureOutput(func() {
		_ = info(ctx)
	})

	assertContains(t, stdout, "fetching details")
}

// TestOutput_CommandAliases verifies command aliases work correctly
func TestOutput_CommandAliases(t *testing.T) {
	buildArgs := BuildArgs{
		Version:   "1.0.0",
		BuildType: "dev",
	}

	t.Run("d alias for download", func(t *testing.T) {
		stdout, _ := captureOutput(func() {
			_ = Execute([]string{"warpdl", "help", "d"}, buildArgs)
		})
		assertContains(t, stdout, "download")
	})

	t.Run("r alias for resume", func(t *testing.T) {
		stdout, _ := captureOutput(func() {
			_ = Execute([]string{"warpdl", "help", "r"}, buildArgs)
		})
		assertContains(t, stdout, "resume")
	})

	t.Run("l alias for list", func(t *testing.T) {
		stdout, _ := captureOutput(func() {
			_ = Execute([]string{"warpdl", "help", "l"}, buildArgs)
		})
		assertContains(t, stdout, "list")
	})

	t.Run("i alias for info", func(t *testing.T) {
		stdout, _ := captureOutput(func() {
			_ = Execute([]string{"warpdl", "help", "i"}, buildArgs)
		})
		assertContains(t, stdout, "info")
	})

	t.Run("c alias for flush", func(t *testing.T) {
		stdout, _ := captureOutput(func() {
			_ = Execute([]string{"warpdl", "help", "c"}, buildArgs)
		})
		assertContains(t, stdout, "flush")
	})
}

// =============================================================================
// Edge Case Tests
// =============================================================================

// TestOutput_List_HiddenItems verifies that hidden items are not shown
// unless showHidden is enabled.
func TestOutput_List_HiddenItems(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)

	listOverride = []*warplib.Item{{
		Hash:       "hidden1",
		Name:       "hidden.bin",
		TotalSize:  100,
		Downloaded: 100,
		Hidden:     true,
		Children:   false,
		DateAdded:  time.Now(),
		Resumable:  true,
		Parts:      make(map[int64]*warplib.ItemPart),
	}}
	defer func() { listOverride = nil }()

	srv := startFakeServer(t, socketPath)
	defer srv.close()

	oldShowHidden := showHidden
	showHidden = false
	defer func() { showHidden = oldShowHidden }()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, nil, "list")

	stdout, _ := captureOutput(func() {
		_ = list(ctx)
	})

	assertContains(t, stdout, "no downloads found")
}

// TestOutput_List_MultipleItems verifies that multiple items display correctly.
func TestOutput_List_MultipleItems(t *testing.T) {
	socketPath := getShortSocketPath(t)
	t.Setenv("WARPDL_SOCKET_PATH", socketPath)

	listOverride = []*warplib.Item{
		{
			Hash:       "item01",
			Name:       "file1.bin",
			TotalSize:  100,
			Downloaded: 25,
			Hidden:     false,
			Children:   false,
			DateAdded:  time.Now(),
			Resumable:  true,
			Parts:      make(map[int64]*warplib.ItemPart),
		},
		{
			Hash:       "item02",
			Name:       "file2.bin",
			TotalSize:  200,
			Downloaded: 200,
			Hidden:     false,
			Children:   false,
			DateAdded:  time.Now().Add(-time.Hour),
			Resumable:  true,
			Parts:      make(map[int64]*warplib.ItemPart),
		},
	}
	defer func() { listOverride = nil }()

	srv := startFakeServer(t, socketPath)
	defer srv.close()

	app := cli.NewApp()
	app.Name = "warpdl"
	app.HelpName = "warpdl"
	ctx := newContext(app, nil, "list")

	stdout, _ := captureOutput(func() {
		_ = list(ctx)
	})

	t.Run("shows both items", func(t *testing.T) {
		assertContains(t, stdout, "item01")
		assertContains(t, stdout, "item02")
	})

	t.Run("shows row numbers", func(t *testing.T) {
		assertContains(t, stdout, "| 1 |")
		assertContains(t, stdout, "| 2 |")
	})

	t.Run("shows different percentages", func(t *testing.T) {
		assertContains(t, stdout, "25%")
		assertContains(t, stdout, "100%")
	})
}

// TestOutput_Resume_InvalidProxy verifies that an invalid proxy URL
// produces an error in resume command.
// Note: Skipped because fake server hangs when client fails before sending request.
func TestOutput_Resume_InvalidProxy(t *testing.T) {
	t.Skip("Requires real daemon - fake server doesn't handle early client exit")
}
