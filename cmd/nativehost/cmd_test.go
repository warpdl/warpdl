package nativehost

import (
	"bytes"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/internal/nativehost"
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
