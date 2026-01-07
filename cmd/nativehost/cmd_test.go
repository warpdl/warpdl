package nativehost

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/common"
	"github.com/warpdl/warpdl/internal/nativehost"
	"github.com/warpdl/warpdl/pkg/warpcli"
)

func newContext(app *cli.App, args []string, name string, flags []cli.Flag) *cli.Context {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	for _, f := range flags {
		switch sf := f.(type) {
		case cli.StringFlag:
			set.String(sf.Name, sf.Value, sf.Usage)
		}
	}
	_ = set.Parse(args)
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: name}
	return ctx
}

// captureOutput captures stdout and stderr during function execution.
func captureOutput(f func()) (stdout, stderr string) {
	oldStdout := os.Stdout
	oldStderr := os.Stderr

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	f()

	wOut.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	var bufOut, bufErr bytes.Buffer
	io.Copy(&bufOut, rOut)
	io.Copy(&bufErr, rErr)
	rOut.Close()
	rErr.Close()

	return bufOut.String(), bufErr.String()
}

// assertContains checks if output contains the expected substring.
func assertContains(t *testing.T, output, expected string) {
	t.Helper()
	if !strings.Contains(output, expected) {
		t.Errorf("expected output to contain %q, got:\n%s", expected, output)
	}
}

func TestInstallCommand_MissingExtensionIDs(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"
	ctx := newContext(app, nil, "install", installFlags)

	var err error
	captureOutput(func() {
		err = install(ctx)
	})

	if err == nil {
		t.Error("Expected error when no extension IDs provided")
	}
	// Check that it's an ExitCoder with code 1
	if exitErr, ok := err.(cli.ExitCoder); ok {
		if exitErr.ExitCode() != 1 {
			t.Errorf("Expected exit code 1, got %d", exitErr.ExitCode())
		}
	}
}

func TestUninstallManifest(t *testing.T) {
	// Create a temp manifest file
	tmpDir := t.TempDir()
	manifestDir := filepath.Join(tmpDir, "NativeMessagingHosts")
	if err := os.MkdirAll(manifestDir, 0755); err != nil {
		t.Fatalf("Failed to create manifest dir: %v", err)
	}

	manifestFile := filepath.Join(manifestDir, nativehost.HostName+".json")
	if err := os.WriteFile(manifestFile, []byte(`{"name":"test"}`), 0644); err != nil {
		t.Fatalf("Failed to create manifest: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(manifestFile); os.IsNotExist(err) {
		t.Fatal("Manifest file should exist before uninstall")
	}

	// Uninstall
	if err := nativehost.UninstallManifest(manifestFile); err != nil {
		t.Fatalf("UninstallManifest failed: %v", err)
	}

	// Verify file removed
	if _, err := os.Stat(manifestFile); !os.IsNotExist(err) {
		t.Error("Manifest file should be removed after uninstall")
	}
}

func TestStatusCommand(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"
	ctx := newContext(app, nil, "status", nil)

	var err error
	stdout, _ := captureOutput(func() {
		err = status(ctx)
	})

	if err != nil {
		t.Errorf("Status command failed: %v", err)
	}

	assertContains(t, stdout, "Native Messaging Host Status")
	assertContains(t, stdout, nativehost.HostName)
}

func TestCommandsRegistered(t *testing.T) {
	if len(Commands) != 4 {
		t.Errorf("Expected 4 commands, got %d", len(Commands))
	}

	expectedNames := []string{"install", "uninstall", "run", "status"}
	for _, expected := range expectedNames {
		found := false
		for _, cmd := range Commands {
			if cmd.Name == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Command %s not found", expected)
		}
	}
}

func TestInstallFlagsRegistered(t *testing.T) {
	var installCmd *cli.Command
	for _, cmd := range Commands {
		if cmd.Name == "install" {
			installCmd = &cmd
			break
		}
	}

	if installCmd == nil {
		t.Fatal("Install command not found")
	}

	expectedFlags := []string{"browser", "chrome-extension-id", "firefox-extension-id"}
	for _, expected := range expectedFlags {
		found := false
		for _, f := range installCmd.Flags {
			if sf, ok := f.(cli.StringFlag); ok && sf.Name == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Flag %s not found in install command", expected)
		}
	}
}

func TestRunCommandHidden(t *testing.T) {
	var runCmd *cli.Command
	for _, cmd := range Commands {
		if cmd.Name == "run" {
			runCmd = &cmd
			break
		}
	}

	if runCmd == nil {
		t.Fatal("Run command not found")
	}

	if !runCmd.Hidden {
		t.Error("Run command should be hidden")
	}
}

func TestInstallUnknownBrowser(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"

	set := flag.NewFlagSet("install", flag.ContinueOnError)
	set.String("browser", "unknown", "")
	set.String("chrome-extension-id", "test123", "")
	set.String("firefox-extension-id", "", "")
	_ = set.Parse(nil)
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: "install"}

	var err error
	captureOutput(func() {
		err = install(ctx)
	})

	if err == nil {
		t.Error("Expected error for unknown browser")
	}
}

func TestUninstallUnknownBrowser(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"

	set := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	set.String("browser", "unknown", "")
	_ = set.Parse(nil)
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: "uninstall"}

	var err error
	captureOutput(func() {
		err = uninstall(ctx)
	})

	if err == nil {
		t.Error("Expected error for unknown browser")
	}
}

// TestRunConnectionFailure tests the run command when daemon connection fails
func TestRunConnectionFailure(t *testing.T) {
	// Save original and restore after test
	origFunc := newClientFunc
	defer func() { newClientFunc = origFunc }()

	// Mock to return an error
	newClientFunc = func() (*warpcli.Client, error) {
		return nil, errors.New("connection refused")
	}

	app := cli.NewApp()
	app.Name = "warpdl"
	ctx := newContext(app, nil, "run", nil)

	var err error
	captureOutput(func() {
		err = run(ctx)
	})

	if err == nil {
		t.Error("Expected error when connection fails")
	}
	if exitErr, ok := err.(cli.ExitCoder); ok {
		if exitErr.ExitCode() != 1 {
			t.Errorf("Expected exit code 1, got %d", exitErr.ExitCode())
		}
	}
}

// TestWarpcliAdapterDownload tests the adapter's Download method with options
func TestWarpcliAdapterDownload(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	client := warpcli.NewClientForTesting(c1)
	adapter := &warpcliAdapter{Client: client}

	// Mock server
	go func() {
		reqBytes, _ := warpcli.ReadForTesting(c2)
		var req struct {
			Method  string          `json:"method"`
			Message json.RawMessage `json:"message"`
		}
		_ = json.Unmarshal(reqBytes, &req)

		resp := struct {
			Ok     bool `json:"ok"`
			Update struct {
				Type    string          `json:"type"`
				Message json.RawMessage `json:"message"`
			} `json:"update"`
		}{
			Ok: true,
		}
		resp.Update.Type = string(req.Method)
		resp.Update.Message, _ = json.Marshal(common.DownloadResponse{DownloadId: "test-123"})
		respBytes, _ := json.Marshal(resp)
		_ = warpcli.WriteForTesting(c2, respBytes)
	}()

	opts := &nativehost.DownloadOptions{
		ForceParts:     true,
		MaxConnections: 8,
		MaxSegments:    16,
		Overwrite:      true,
		Proxy:          "http://proxy:8080",
		Timeout:        30,
		SpeedLimit:     "1M",
	}

	result, err := adapter.Download("https://example.com/file.zip", "file.zip", "/tmp", opts)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	if result.DownloadId != "test-123" {
		t.Errorf("Expected DownloadId 'test-123', got %s", result.DownloadId)
	}
}

// TestWarpcliAdapterDownloadNilOpts tests Download with nil options
func TestWarpcliAdapterDownloadNilOpts(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	client := warpcli.NewClientForTesting(c1)
	adapter := &warpcliAdapter{Client: client}

	go func() {
		reqBytes, _ := warpcli.ReadForTesting(c2)
		var req struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(reqBytes, &req)

		resp := struct {
			Ok     bool `json:"ok"`
			Update struct {
				Type    string          `json:"type"`
				Message json.RawMessage `json:"message"`
			} `json:"update"`
		}{Ok: true}
		resp.Update.Type = string(req.Method)
		resp.Update.Message, _ = json.Marshal(common.DownloadResponse{DownloadId: "test-nil"})
		respBytes, _ := json.Marshal(resp)
		_ = warpcli.WriteForTesting(c2, respBytes)
	}()

	result, err := adapter.Download("https://example.com/file.zip", "", "", nil)
	if err != nil {
		t.Fatalf("Download with nil opts failed: %v", err)
	}
	if result.DownloadId != "test-nil" {
		t.Errorf("Expected DownloadId 'test-nil', got %s", result.DownloadId)
	}
}

// TestWarpcliAdapterList tests the adapter's List method
func TestWarpcliAdapterList(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	client := warpcli.NewClientForTesting(c1)
	adapter := &warpcliAdapter{Client: client}

	go func() {
		reqBytes, _ := warpcli.ReadForTesting(c2)
		var req struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(reqBytes, &req)

		resp := struct {
			Ok     bool `json:"ok"`
			Update struct {
				Type    string          `json:"type"`
				Message json.RawMessage `json:"message"`
			} `json:"update"`
		}{Ok: true}
		resp.Update.Type = string(req.Method)
		resp.Update.Message, _ = json.Marshal(common.ListResponse{})
		respBytes, _ := json.Marshal(resp)
		_ = warpcli.WriteForTesting(c2, respBytes)
	}()

	_, err := adapter.List(nil)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
}

// TestWarpcliAdapterGetDaemonVersion tests the adapter's GetDaemonVersion method
func TestWarpcliAdapterGetDaemonVersion(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	client := warpcli.NewClientForTesting(c1)
	adapter := &warpcliAdapter{Client: client}

	go func() {
		reqBytes, _ := warpcli.ReadForTesting(c2)
		var req struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(reqBytes, &req)

		resp := struct {
			Ok     bool `json:"ok"`
			Update struct {
				Type    string          `json:"type"`
				Message json.RawMessage `json:"message"`
			} `json:"update"`
		}{Ok: true}
		resp.Update.Type = string(req.Method)
		resp.Update.Message, _ = json.Marshal(common.VersionResponse{Version: "1.0.0", Commit: "abc123"})
		respBytes, _ := json.Marshal(resp)
		_ = warpcli.WriteForTesting(c2, respBytes)
	}()

	result, err := adapter.GetDaemonVersion()
	if err != nil {
		t.Fatalf("GetDaemonVersion failed: %v", err)
	}
	if result.Version != "1.0.0" {
		t.Errorf("Expected version '1.0.0', got %s", result.Version)
	}
}

// TestWarpcliAdapterStopDownload tests the adapter's StopDownload method
func TestWarpcliAdapterStopDownload(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	client := warpcli.NewClientForTesting(c1)
	adapter := &warpcliAdapter{Client: client}

	go func() {
		reqBytes, _ := warpcli.ReadForTesting(c2)
		var req struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(reqBytes, &req)

		resp := struct {
			Ok     bool `json:"ok"`
			Update struct {
				Type    string          `json:"type"`
				Message json.RawMessage `json:"message"`
			} `json:"update"`
		}{Ok: true}
		resp.Update.Type = string(req.Method)
		resp.Update.Message = json.RawMessage(`{}`)
		respBytes, _ := json.Marshal(resp)
		_ = warpcli.WriteForTesting(c2, respBytes)
	}()

	result, err := adapter.StopDownload("test-123")
	if err != nil {
		t.Fatalf("StopDownload failed: %v", err)
	}
	if !result {
		t.Error("Expected StopDownload to return true")
	}
}

// TestWarpcliAdapterResume tests the adapter's Resume method with options
func TestWarpcliAdapterResume(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	client := warpcli.NewClientForTesting(c1)
	adapter := &warpcliAdapter{Client: client}

	go func() {
		reqBytes, _ := warpcli.ReadForTesting(c2)
		var req struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(reqBytes, &req)

		resp := struct {
			Ok     bool `json:"ok"`
			Update struct {
				Type    string          `json:"type"`
				Message json.RawMessage `json:"message"`
			} `json:"update"`
		}{Ok: true}
		resp.Update.Type = string(req.Method)
		resp.Update.Message, _ = json.Marshal(common.ResumeResponse{FileName: "resumed.zip"})
		respBytes, _ := json.Marshal(resp)
		_ = warpcli.WriteForTesting(c2, respBytes)
	}()

	opts := &nativehost.ResumeOptions{
		ForceParts:     true,
		MaxConnections: 4,
		MaxSegments:    8,
		Proxy:          "socks5://proxy:1080",
		Timeout:        60,
		SpeedLimit:     "500K",
	}

	result, err := adapter.Resume("test-123", opts)
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}
	if result.FileName != "resumed.zip" {
		t.Errorf("Expected FileName 'resumed.zip', got %s", result.FileName)
	}
}

// TestWarpcliAdapterResumeNilOpts tests Resume with nil options
func TestWarpcliAdapterResumeNilOpts(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	client := warpcli.NewClientForTesting(c1)
	adapter := &warpcliAdapter{Client: client}

	go func() {
		reqBytes, _ := warpcli.ReadForTesting(c2)
		var req struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(reqBytes, &req)

		resp := struct {
			Ok     bool `json:"ok"`
			Update struct {
				Type    string          `json:"type"`
				Message json.RawMessage `json:"message"`
			} `json:"update"`
		}{Ok: true}
		resp.Update.Type = string(req.Method)
		resp.Update.Message, _ = json.Marshal(common.ResumeResponse{FileName: "nil-opts.zip"})
		respBytes, _ := json.Marshal(resp)
		_ = warpcli.WriteForTesting(c2, respBytes)
	}()

	result, err := adapter.Resume("test-123", nil)
	if err != nil {
		t.Fatalf("Resume with nil opts failed: %v", err)
	}
	if result.FileName != "nil-opts.zip" {
		t.Errorf("Expected FileName 'nil-opts.zip', got %s", result.FileName)
	}
}

// TestWarpcliAdapterFlush tests the adapter's Flush method
func TestWarpcliAdapterFlush(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	client := warpcli.NewClientForTesting(c1)
	adapter := &warpcliAdapter{Client: client}

	go func() {
		reqBytes, _ := warpcli.ReadForTesting(c2)
		var req struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(reqBytes, &req)

		resp := struct {
			Ok     bool `json:"ok"`
			Update struct {
				Type    string          `json:"type"`
				Message json.RawMessage `json:"message"`
			} `json:"update"`
		}{Ok: true}
		resp.Update.Type = string(req.Method)
		resp.Update.Message = json.RawMessage(`{}`)
		respBytes, _ := json.Marshal(resp)
		_ = warpcli.WriteForTesting(c2, respBytes)
	}()

	result, err := adapter.Flush("test-123")
	if err != nil {
		t.Fatalf("Flush failed: %v", err)
	}
	if !result {
		t.Error("Expected Flush to return true")
	}
}

// TestWarpcliAdapterClose tests the adapter's Close method
func TestWarpcliAdapterClose(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c2.Close()

	client := warpcli.NewClientForTesting(c1)
	adapter := &warpcliAdapter{Client: client}

	err := adapter.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

// TestInstallChromeBrowser tests install for Chrome browser
func TestInstallChromeBrowser(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"

	set := flag.NewFlagSet("install", flag.ContinueOnError)
	set.String("browser", "chrome", "")
	set.String("chrome-extension-id", "testextension123", "")
	set.String("firefox-extension-id", "", "")
	_ = set.Parse(nil)
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: "install"}

	// This test will attempt to install but may fail if directory doesn't exist
	// We're testing that the browser switch case is executed
	captureOutput(func() {
		_ = install(ctx)
	})
	// Test passes if no panic occurs - browser case is covered
}

// TestInstallChromiumBrowser tests install for Chromium browser
func TestInstallChromiumBrowser(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"

	set := flag.NewFlagSet("install", flag.ContinueOnError)
	set.String("browser", "chromium", "")
	set.String("chrome-extension-id", "testextension123", "")
	set.String("firefox-extension-id", "", "")
	_ = set.Parse(nil)
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: "install"}

	captureOutput(func() {
		_ = install(ctx)
	})
}

// TestInstallEdgeBrowser tests install for Edge browser
func TestInstallEdgeBrowser(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"

	set := flag.NewFlagSet("install", flag.ContinueOnError)
	set.String("browser", "edge", "")
	set.String("chrome-extension-id", "testextension123", "")
	set.String("firefox-extension-id", "", "")
	_ = set.Parse(nil)
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: "install"}

	captureOutput(func() {
		_ = install(ctx)
	})
}

// TestInstallBraveBrowser tests install for Brave browser
func TestInstallBraveBrowser(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"

	set := flag.NewFlagSet("install", flag.ContinueOnError)
	set.String("browser", "brave", "")
	set.String("chrome-extension-id", "testextension123", "")
	set.String("firefox-extension-id", "", "")
	_ = set.Parse(nil)
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: "install"}

	captureOutput(func() {
		_ = install(ctx)
	})
}

// TestInstallFirefoxBrowser tests install for Firefox browser
func TestInstallFirefoxBrowser(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"

	set := flag.NewFlagSet("install", flag.ContinueOnError)
	set.String("browser", "firefox", "")
	set.String("chrome-extension-id", "", "")
	set.String("firefox-extension-id", "testextension@example.com", "")
	_ = set.Parse(nil)
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: "install"}

	captureOutput(func() {
		_ = install(ctx)
	})
}

// TestInstallAllBrowsers tests install for all browsers
func TestInstallAllBrowsers(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"

	set := flag.NewFlagSet("install", flag.ContinueOnError)
	set.String("browser", "all", "")
	set.String("chrome-extension-id", "testextension123", "")
	set.String("firefox-extension-id", "testextension@example.com", "")
	_ = set.Parse(nil)
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: "install"}

	captureOutput(func() {
		_ = install(ctx)
	})
}

// TestUninstallChromeBrowser tests uninstall for Chrome browser
func TestUninstallChromeBrowser(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"

	set := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	set.String("browser", "chrome", "")
	_ = set.Parse(nil)
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: "uninstall"}

	captureOutput(func() {
		_ = uninstall(ctx)
	})
}

// TestUninstallChromiumBrowser tests uninstall for Chromium browser
func TestUninstallChromiumBrowser(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"

	set := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	set.String("browser", "chromium", "")
	_ = set.Parse(nil)
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: "uninstall"}

	captureOutput(func() {
		_ = uninstall(ctx)
	})
}

// TestUninstallEdgeBrowser tests uninstall for Edge browser
func TestUninstallEdgeBrowser(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"

	set := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	set.String("browser", "edge", "")
	_ = set.Parse(nil)
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: "uninstall"}

	captureOutput(func() {
		_ = uninstall(ctx)
	})
}

// TestUninstallBraveBrowser tests uninstall for Brave browser
func TestUninstallBraveBrowser(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"

	set := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	set.String("browser", "brave", "")
	_ = set.Parse(nil)
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: "uninstall"}

	captureOutput(func() {
		_ = uninstall(ctx)
	})
}

// TestUninstallFirefoxBrowser tests uninstall for Firefox browser
func TestUninstallFirefoxBrowser(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"

	set := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	set.String("browser", "firefox", "")
	_ = set.Parse(nil)
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: "uninstall"}

	captureOutput(func() {
		_ = uninstall(ctx)
	})
}

// TestUninstallAllBrowsers tests uninstall for all browsers
func TestUninstallAllBrowsers(t *testing.T) {
	app := cli.NewApp()
	app.Name = "warpdl"

	set := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	set.String("browser", "all", "")
	_ = set.Parse(nil)
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: "uninstall"}

	captureOutput(func() {
		_ = uninstall(ctx)
	})
}

// TestUninstallFlagsRegistered tests that uninstall flags are registered
func TestUninstallFlagsRegistered(t *testing.T) {
	var uninstallCmd *cli.Command
	for _, cmd := range Commands {
		if cmd.Name == "uninstall" {
			uninstallCmd = &cmd
			break
		}
	}

	if uninstallCmd == nil {
		t.Fatal("Uninstall command not found")
	}

	found := false
	for _, f := range uninstallCmd.Flags {
		if sf, ok := f.(cli.StringFlag); ok && sf.Name == "browser" {
			found = true
			if sf.Value != "all" {
				t.Errorf("Expected default browser value 'all', got %s", sf.Value)
			}
			break
		}
	}
	if !found {
		t.Error("Flag 'browser' not found in uninstall command")
	}
}
