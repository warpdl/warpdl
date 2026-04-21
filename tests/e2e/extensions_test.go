//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// runInDir executes a warpdl CLI command with the working directory set to dir.
// Returns combined stdout+stderr. Fails the test on non-zero exit.
//
// This is required for "ext install" because the daemon binary resolves the
// extension path as filepath.Join(os.Getwd(), givenPath), so callers must
// either run from the directory that contains the file or use a path that is
// relative to that directory.
func (e *testEnv) runInDir(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(e.BinaryPath, args...)
	cmd.Env = e.Env
	cmd.Dir = dir
	output, err := runCmdWithTimeout(cmd, commandTimeout)
	if err != nil {
		t.Fatalf("command %v (dir=%s) failed: %v\nOutput: %s", args, dir, err, output)
	}
	return output
}

// waitForExtension polls "ext list" until the named extension appears or the
// deadline elapses. Extensions are loaded asynchronously by the daemon after
// AddExtension returns, so a brief retry loop avoids brittle time.Sleep calls.
func waitForExtension(t *testing.T, e *testEnv, name string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		output, err := e.runMayFail("ext", "list")
		if err == nil && strings.Contains(output, name) {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("extension %q did not appear in list within %v", name, timeout)
}

// ---------------------------------------------------------------------------
// Extension lifecycle tests
// ---------------------------------------------------------------------------

// TestExt_InstallListUninstall verifies the full install → list → uninstall
// lifecycle:
//  1. Install a minimal JS extension.
//  2. Confirm it appears in the list output.
//  3. Uninstall it.
//  4. Confirm it no longer appears in the list.
func TestExt_InstallListUninstall(t *testing.T) {
	e := newTestEnv(t)
	e.startDaemon(t)

	extPath := createTestExtension(t, e.ConfigDir, "myext")
	// ext install takes a path relative to the process working directory.
	// Run the command from the directory that contains the file so we can
	// pass a short relative path.
	extDir := filepath.Dir(extPath)
	extFile := filepath.Base(extPath)

	// Install the extension.
	installOut := e.runInDir(t, extDir, "ext", "install", extFile)
	assertOutputContains(t, installOut, "Successfully installed extension")
	assertOutputContains(t, installOut, "myext")

	// Confirm it appears in the list.
	waitForExtension(t, e, "myext", 10*time.Second)
	listOut := e.run(t, "ext", "list")
	assertOutputContains(t, listOut, "myext")

	// Uninstall it.
	uninstallOut := e.run(t, "ext", "uninstall", "myext")
	assertOutputContains(t, uninstallOut, "Successfully uninstalled extension")
	assertOutputContains(t, uninstallOut, "myext")

	// Extension must no longer appear in the list.
	listAfter := e.run(t, "ext", "list")
	assertOutputNotContains(t, listAfter, "myext")
}

// TestExt_ActivateDeactivate verifies activate/deactivate state transitions:
//  1. Install an extension (active by default).
//  2. Deactivate it — list must show inactive.
//  3. Activate it again — list must show active.
func TestExt_ActivateDeactivate(t *testing.T) {
	e := newTestEnv(t)
	e.startDaemon(t)

	extPath := createTestExtension(t, e.ConfigDir, "toggleext")
	extDir := filepath.Dir(extPath)
	extFile := filepath.Base(extPath)

	// Install.
	e.runInDir(t, extDir, "ext", "install", extFile)
	waitForExtension(t, e, "toggleext", 10*time.Second)

	// Deactivate.
	deactivateOut := e.run(t, "ext", "deactivate", "toggleext")
	assertOutputContains(t, deactivateOut, "Successfully deactivated extension")
	assertOutputContains(t, deactivateOut, "toggleext")

	// List must show the extension marked as inactive (false).
	listAfterDeactivate := e.run(t, "ext", "list", "--show-all")
	assertOutputContains(t, listAfterDeactivate, "toggleext")
	assertOutputContains(t, listAfterDeactivate, "false")

	// Activate.
	activateOut := e.run(t, "ext", "activate", "toggleext")
	assertOutputContains(t, activateOut, "Successfully activated extension")
	assertOutputContains(t, activateOut, "toggleext")

	// List must show the extension marked as active (true).
	listAfterActivate := e.run(t, "ext", "list")
	assertOutputContains(t, listAfterActivate, "toggleext")
	assertOutputContains(t, listAfterActivate, "true")
}

// TestExt_InstallInvalidPath verifies that installing a non-existent file path
// returns a runtime error rather than silently succeeding.
func TestExt_InstallInvalidPath(t *testing.T) {
	e := newTestEnv(t)
	e.startDaemon(t)

	// The daemon should reject a path that does not resolve to a real file.
	// The CLI prints a runtime error and exits 0 (per the pattern in install.go),
	// so we use runMayFail and inspect the output instead of runExpectError.
	output, _ := e.runMayFail("ext", "install", "/does/not/exist/nope.js")

	// Expect some form of error message — either from the daemon or the CLI layer.
	if !strings.Contains(output, "error") &&
		!strings.Contains(output, "Error") &&
		!strings.Contains(output, "failed") &&
		!strings.Contains(output, "not found") &&
		!strings.Contains(output, "no such file") &&
		!strings.Contains(output, "invalid") {
		t.Fatalf("expected error output for non-existent path, got:\n%s", output)
	}
}

// TestExt_UninstallNotFound verifies that uninstalling a name that does not
// exist in the extension store produces an error message.
func TestExt_UninstallNotFound(t *testing.T) {
	e := newTestEnv(t)
	e.startDaemon(t)

	// Attempting to delete an extension that was never installed should fail.
	// The CLI prints the daemon error and exits 0, so inspect output directly.
	output, _ := e.runMayFail("ext", "uninstall", "nonexistent-extension-xyz")

	if !strings.Contains(output, "error") &&
		!strings.Contains(output, "Error") &&
		!strings.Contains(output, "failed") &&
		!strings.Contains(output, "not found") {
		t.Fatalf("expected error output when uninstalling nonexistent extension, got:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// Native host tests
// ---------------------------------------------------------------------------

// TestNativeHost_Status verifies that "native-host status" exits cleanly and
// prints the expected header and per-browser status lines. No daemon is
// required — the command only inspects filesystem paths.
func TestNativeHost_Status(t *testing.T) {
	// Ensure a real home directory is available; skip in environments where
	// os.UserHomeDir() would fail (e.g. containers with no /etc/passwd).
	if _, err := os.UserHomeDir(); err != nil {
		t.Skipf("no home directory available: %v", err)
	}

	e := newTestEnv(t)

	output := e.run(t, "native-host", "status")

	// Header must be present.
	assertOutputContains(t, output, "Native Messaging Host Status")

	// Each supported browser must appear in the output.
	for _, browser := range []string{"Chrome", "Firefox", "Chromium", "Edge", "Brave"} {
		if !strings.Contains(output, browser) {
			t.Errorf("expected browser %q to appear in status output, got:\n%s", browser, output)
		}
	}

	// Every browser line must report either "Installed" or "Not installed".
	hasStatus := strings.Contains(output, "Installed") || strings.Contains(output, "Not installed")
	if !hasStatus {
		t.Errorf("expected status lines with 'Installed' or 'Not installed', got:\n%s", output)
	}
}
