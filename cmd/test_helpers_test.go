package cmd

import (
	"bytes"
	"flag"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/urfave/cli"
)

// captureOutput captures stdout and stderr during function execution.
// It redirects os.Stdout and os.Stderr to pipes, runs the provided function,
// and returns the captured output as strings. This is useful for testing
// CLI output without modifying the command implementations.
func captureOutput(f func()) (stdout, stderr string) {
	// Save original file descriptors
	oldStdout := os.Stdout
	oldStderr := os.Stderr

	// Create pipes for capturing output
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	// Run the function
	f()

	// Close writers and restore original file descriptors
	wOut.Close()
	wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	// Read captured output
	var bufOut, bufErr bytes.Buffer
	io.Copy(&bufOut, rOut)
	io.Copy(&bufErr, rErr)
	rOut.Close()
	rErr.Close()

	return bufOut.String(), bufErr.String()
}

// assertContains checks if output contains the expected substring.
// It reports a test failure with the actual output if the substring is not found.
func assertContains(t *testing.T, output, expected string) {
	t.Helper()
	if !strings.Contains(output, expected) {
		t.Errorf("expected output to contain %q, got:\n%s", expected, output)
	}
}

// assertNotContains checks if output does NOT contain the specified substring.
// It reports a test failure if the substring is found in the output.
func assertNotContains(t *testing.T, output, notExpected string) {
	t.Helper()
	if strings.Contains(output, notExpected) {
		t.Errorf("expected output to NOT contain %q, got:\n%s", notExpected, output)
	}
}

// assertErrorFormat checks that error output follows the standard format:
// warpdl: cmd[action]: msg
// This validates that runtime errors are formatted consistently.
func assertErrorFormat(t *testing.T, output, cmd, action string) {
	t.Helper()
	pattern := "warpdl: " + cmd + "[" + action + "]:"
	if !strings.Contains(output, pattern) {
		t.Errorf("expected error format %q, got:\n%s", pattern, output)
	}
}

// assertContainsAll checks that output contains all expected substrings.
// It reports a failure for each missing substring.
func assertContainsAll(t *testing.T, output string, expected []string) {
	t.Helper()
	for _, exp := range expected {
		if !strings.Contains(output, exp) {
			t.Errorf("expected output to contain %q, got:\n%s", exp, output)
		}
	}
}

// assertLineCount checks that output has at least the expected number of lines.
func assertLineCount(t *testing.T, output string, minLines int) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < minLines {
		t.Errorf("expected at least %d lines, got %d:\n%s", minLines, len(lines), output)
	}
}

// newContext creates a CLI context for testing commands.
func newContext(app *cli.App, args []string, name string) *cli.Context {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	_ = set.Parse(args)
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: name}
	return ctx
}
