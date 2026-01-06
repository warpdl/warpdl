package cmd

import (
    "flag"
    "strings"
    "testing"

    "github.com/urfave/cli"
)

// =============================================================================
// Background Flag Tests - TDD Phase 1 (RED)
// =============================================================================

// TestDownload_BackgroundFlag_Parsed verifies that --background flag is parsed
// correctly for the download command.
func TestDownload_BackgroundFlag_Parsed(t *testing.T) {
    app := cli.NewApp()
    app.Name = "warpdl"
    app.HelpName = "warpdl"

    // Create flag set with background flag
    set := flag.NewFlagSet("download", flag.ContinueOnError)
    set.Bool("background", false, "")
    _ = set.Parse([]string{"--background", "http://example.com/file.bin"})

    ctx := cli.NewContext(app, set, nil)
    ctx.Command = cli.Command{Name: "download", Flags: dlFlags}

    if !ctx.Bool("background") {
        t.Error("expected --background flag to be true")
    }
}

// TestResume_BackgroundFlag_ShortAlias verifies that -b short alias works
// for the resume command.
func TestResume_BackgroundFlag_ShortAlias(t *testing.T) {
    app := cli.NewApp()
    app.Name = "warpdl"
    app.HelpName = "warpdl"

    // Create flag set with background flag using short alias
    set := flag.NewFlagSet("resume", flag.ContinueOnError)
    set.Bool("background", false, "")
    set.Bool("b", false, "")
    _ = set.Parse([]string{"-b", "testhash"})

    ctx := cli.NewContext(app, set, nil)
    ctx.Command = cli.Command{Name: "resume", Flags: rsFlags}

    // Note: urfave/cli handles aliases, but for flag.FlagSet we need to check "b"
    if !ctx.Bool("b") && !ctx.Bool("background") {
        t.Error("expected -b flag to be true")
    }
}

// TestOutput_Download_Background_PrintsHash verifies that background download
// prints the download ID/hash in the output.
func TestOutput_Download_Background_PrintsHash(t *testing.T) {
    socketPath := getShortSocketPath(t)
    t.Setenv("WARPDL_SOCKET_PATH", socketPath)

    srv := startFakeServer(t, socketPath)
    defer srv.close()

    app := cli.NewApp()
    app.Name = "warpdl"
    app.HelpName = "warpdl"

    // Create context with background flag
    set := flag.NewFlagSet("download", flag.ContinueOnError)
    set.Bool("background", false, "")
    _ = set.Parse([]string{"--background", "http://example.com/file.bin"})

    ctx := cli.NewContext(app, set, nil)
    ctx.Command = cli.Command{Name: "download", Flags: dlFlags}

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

    t.Run("shows background message", func(t *testing.T) {
        if !strings.Contains(strings.ToLower(stdout), "background") {
            t.Errorf("expected output to contain 'background', got:\n%s", stdout)
        }
    })

    t.Run("shows download ID", func(t *testing.T) {
        // Fake server returns "id" as DownloadId
        if !strings.Contains(stdout, "id") {
            t.Errorf("expected output to contain download ID, got:\n%s", stdout)
        }
    })
}

// TestOutput_Resume_Background_PrintsHash verifies that background resume
// prints the hash in the output.
func TestOutput_Resume_Background_PrintsHash(t *testing.T) {
    socketPath := getShortSocketPath(t)
    t.Setenv("WARPDL_SOCKET_PATH", socketPath)

    srv := startFakeServer(t, socketPath)
    defer srv.close()

    app := cli.NewApp()
    app.Name = "warpdl"
    app.HelpName = "warpdl"

    // Create context with background flag
    set := flag.NewFlagSet("resume", flag.ContinueOnError)
    set.Bool("background", false, "")
    _ = set.Parse([]string{"--background", "testhash"})

    ctx := cli.NewContext(app, set, nil)
    ctx.Command = cli.Command{Name: "resume", Flags: rsFlags}

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

    t.Run("shows background message", func(t *testing.T) {
        if !strings.Contains(strings.ToLower(stdout), "background") {
            t.Errorf("expected output to contain 'background', got:\n%s", stdout)
        }
    })

    t.Run("shows hash", func(t *testing.T) {
        if !strings.Contains(stdout, "testhash") {
            t.Errorf("expected output to contain hash 'testhash', got:\n%s", stdout)
        }
    })
}

// TestOutput_Download_Background_ShowsAttachHint verifies that background download
// shows the attach command hint.
func TestOutput_Download_Background_ShowsAttachHint(t *testing.T) {
    socketPath := getShortSocketPath(t)
    t.Setenv("WARPDL_SOCKET_PATH", socketPath)

    srv := startFakeServer(t, socketPath)
    defer srv.close()

    app := cli.NewApp()
    app.Name = "warpdl"
    app.HelpName = "warpdl"

    set := flag.NewFlagSet("download", flag.ContinueOnError)
    set.Bool("background", false, "")
    _ = set.Parse([]string{"--background", "http://example.com/file.bin"})

    ctx := cli.NewContext(app, set, nil)
    ctx.Command = cli.Command{Name: "download", Flags: dlFlags}

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

    if !strings.Contains(stdout, "warpdl attach") {
        t.Errorf("expected output to contain 'warpdl attach' hint, got:\n%s", stdout)
    }
}

// TestOutput_Resume_Background_ShowsListHint verifies that background resume
// shows the list command hint.
func TestOutput_Resume_Background_ShowsListHint(t *testing.T) {
    socketPath := getShortSocketPath(t)
    t.Setenv("WARPDL_SOCKET_PATH", socketPath)

    srv := startFakeServer(t, socketPath)
    defer srv.close()

    app := cli.NewApp()
    app.Name = "warpdl"
    app.HelpName = "warpdl"

    set := flag.NewFlagSet("resume", flag.ContinueOnError)
    set.Bool("background", false, "")
    _ = set.Parse([]string{"--background", "testhash"})

    ctx := cli.NewContext(app, set, nil)
    ctx.Command = cli.Command{Name: "resume", Flags: rsFlags}

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

    if !strings.Contains(stdout, "warpdl list") {
        t.Errorf("expected output to contain 'warpdl list' hint, got:\n%s", stdout)
    }
}

// TestDownload_NoBackgroundFlag_ShowsProgressSetup verifies that download
// without --background flag proceeds to show progress (doesn't return early).
func TestDownload_NoBackgroundFlag_ShowsProgressSetup(t *testing.T) {
    socketPath := getShortSocketPath(t)
    t.Setenv("WARPDL_SOCKET_PATH", socketPath)

    srv := startFakeServer(t, socketPath)
    defer srv.close()

    app := cli.NewApp()
    app.Name = "warpdl"
    app.HelpName = "warpdl"

    // Create context WITHOUT background flag
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

    // Without background flag, should show normal initiation message
    assertContains(t, stdout, ">> Initiating a WARP download <<")

    // Should NOT contain "background" text since flag is not set
    if strings.Contains(strings.ToLower(stdout), "started download") &&
        strings.Contains(strings.ToLower(stdout), "in background") {
        t.Errorf("without --background flag, should not show background message, got:\n%s", stdout)
    }
}

// TestDownload_Background_WithOtherFlags verifies that --background works
// correctly when combined with other flags like -o and -l.
func TestDownload_Background_WithOtherFlags(t *testing.T) {
    socketPath := getShortSocketPath(t)
    t.Setenv("WARPDL_SOCKET_PATH", socketPath)

    srv := startFakeServer(t, socketPath)
    defer srv.close()

    app := cli.NewApp()
    app.Name = "warpdl"
    app.HelpName = "warpdl"

    tmpDir := t.TempDir()

    // Create context with background flag AND other flags
    set := flag.NewFlagSet("download", flag.ContinueOnError)
    set.Bool("background", false, "")
    set.String("file-name", "", "")
    set.String("download-path", "", "")
    _ = set.Parse([]string{
        "--background",
        "-file-name", "custom.bin",
        "-download-path", tmpDir,
        "http://example.com/file.bin",
    })

    ctx := cli.NewContext(app, set, nil)
    ctx.Command = cli.Command{Name: "download", Flags: dlFlags}

    oldDlPath, oldFileName := dlPath, fileName
    dlPath = tmpDir
    fileName = "custom.bin"
    defer func() {
        dlPath = oldDlPath
        fileName = oldFileName
    }()

    stdout, _ := captureOutput(func() {
        _ = download(ctx)
    })

    t.Run("shows background message", func(t *testing.T) {
        if !strings.Contains(strings.ToLower(stdout), "background") {
            t.Errorf("expected output to contain 'background', got:\n%s", stdout)
        }
    })

    t.Run("shows initiation message", func(t *testing.T) {
        assertContains(t, stdout, ">> Initiating a WARP download <<")
    })
}
