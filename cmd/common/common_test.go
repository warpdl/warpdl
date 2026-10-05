package common

import (
	"errors"
	"flag"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/urfave/cli"
	"github.com/vbauerster/mpb/v8"
)

func newTestContext() *cli.Context {
	app := cli.NewApp()
	app.Name = "warpdl"
	app.Version = "test"
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: "cmd"}
	return ctx
}

// captureStdout redirects os.Stdout for the duration of f and returns what was
// written. printErrWithCallback renders diagnostics through os.Stdout, so the
// tests need the real stream instead of a stubbed writer.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	f()
	_ = w.Close()
	os.Stdout = old

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	_ = r.Close()
	return string(out)
}

// assertOutputContains fails when output does not carry the expected text.
func assertOutputContains(t *testing.T, output, expected string) {
	t.Helper()
	if !strings.Contains(output, expected) {
		t.Errorf("expected output to contain %q, got:\n%s", expected, output)
	}
}

// assertExitCoder fails unless err carries the wanted process exit status.
func assertExitCoder(t *testing.T, err error, wantCode int) {
	t.Helper()
	var exitCoder cli.ExitCoder
	if !errors.As(err, &exitCoder) {
		t.Fatalf("expected cli.ExitCoder, got %T: %v", err, err)
	}
	if exitCoder.ExitCode() != wantCode {
		t.Fatalf("exit code = %d, want %d", exitCoder.ExitCode(), wantCode)
	}
}

func TestInitBars(t *testing.T) {
	p := mpb.New()
	dbar, cbar := InitBars(p, "", 100)
	if dbar == nil || cbar == nil {
		t.Fatalf("expected bars")
	}
}

// TestInitBarsWithProgress verifies that progress bars can be initialized
// with a non-zero starting position for resume scenarios.
// Note: mpb.Bar doesn't expose a public Current() getter, so we can only verify
// the function executes without error. The actual progress is validated via
// integration tests and manual testing.
func TestInitBarsWithProgress(t *testing.T) {
	p := mpb.New()
	initialProgress := int64(50)
	totalSize := int64(100)

	dbar, cbar := InitBarsWithProgress(p, "", totalSize, initialProgress)

	if dbar == nil || cbar == nil {
		t.Fatal("expected bars to be created")
	}
}

// TestInitBarsWithProgressZero ensures backward compatibility
// when initialProgress is 0 (fresh download scenario).
func TestInitBarsWithProgressZero(t *testing.T) {
	p := mpb.New()
	dbar, cbar := InitBarsWithProgress(p, "", 100, 0)

	if dbar == nil || cbar == nil {
		t.Fatal("expected bars to be created")
	}
}

func TestBeautAndReplic(t *testing.T) {
	if got := Beaut("hi", 4); got != " hi " {
		t.Fatalf("unexpected beaut output: %q", got)
	}
	vals := replic('x', 3)
	if len(vals) != 3 || vals[0] != 'x' {
		t.Fatalf("unexpected replic output: %v", vals)
	}
}

func TestBeautOddRemainder(t *testing.T) {
	if got := Beaut("hi", 5); got != " hi  " {
		t.Fatalf("unexpected beaut output for odd padding: %q", got)
	}
}

func TestBeautLongInputDoesNotPanic(t *testing.T) {
	const value = "schedule-value-longer-than-column"
	if got := Beaut(value, 14); got != value {
		t.Fatalf("expected long value unchanged, got %q", got)
	}
	if got := Beaut(value, 0); got != "" {
		t.Fatalf("expected empty output for non-positive width, got %q", got)
	}
}

func TestPrintRuntimeErr(t *testing.T) {
	if err := PrintRuntimeErr(nil, "cmd", "action", nil); err != nil {
		t.Fatalf("nil runtime error: %v", err)
	}
	err := PrintRuntimeErr(newTestContext(), "cmd", "action", errors.New("boom"))
	if err == nil {
		t.Fatal("expected non-zero runtime error")
	}
	exitCoder, ok := err.(cli.ExitCoder)
	if !ok || exitCoder.ExitCode() == 0 {
		t.Fatalf("expected non-zero cli.ExitCoder, got %T: %v", err, err)
	}
}

func TestPrintErrWithHelp(t *testing.T) {
	ctx := newTestContext()
	called := false
	orig := showAppHelpAndExit
	showAppHelpAndExit = func(*cli.Context, int) {
		called = true
	}
	defer func() { showAppHelpAndExit = orig }()

	_ = PrintErrWithHelp(ctx, errors.New("oops"))
	if !called {
		t.Fatalf("expected help to be called")
	}
}

func TestPrintErrWithHelpFlagHelpRequested(t *testing.T) {
	ctx := newTestContext()
	called := false
	orig := showAppHelpAndExit
	showAppHelpAndExit = func(*cli.Context, int) {
		called = true
	}
	defer func() { showAppHelpAndExit = orig }()

	if err := PrintErrWithHelp(ctx, errors.New("flag: help requested")); err != nil {
		t.Fatalf("PrintErrWithHelp: %v", err)
	}
	// When error is "flag: help requested", it calls Help() which calls showAppHelpAndExit
	if !called {
		t.Fatalf("expected help to be called")
	}
}

func TestPrintErrWithCmdHelp(t *testing.T) {
	ctx := newTestContext()
	called := false
	orig := showCommandHelp
	showCommandHelp = func(*cli.Context, string) error {
		called = true
		return nil
	}
	defer func() { showCommandHelp = orig }()

	_ = PrintErrWithCmdHelp(ctx, errors.New("oops"))
	if !called {
		t.Fatalf("expected command help to be called")
	}
}

// TestPrintErrWithCmdHelp_ShowCommandHelpError pins the failure path of
// PrintErrWithCmdHelp: when the help renderer itself fails, its message is
// printed after the original error and the command still reports a non-zero
// exit status instead of silently succeeding.
func TestPrintErrWithCmdHelp_ShowCommandHelpError(t *testing.T) {
	ctx := newTestContext()
	orig := showCommandHelp
	showCommandHelp = func(*cli.Context, string) error {
		return errors.New("help render failed")
	}
	defer func() { showCommandHelp = orig }()

	var err error
	out := captureStdout(t, func() {
		err = PrintErrWithCmdHelp(ctx, errors.New("oops"))
	})

	assertOutputContains(t, out, "oops")
	assertOutputContains(t, out, "help render failed")
	assertExitCoder(t, err, 1)
}

// TestUsageErrorCallback pins command-level routing: with a command in scope the
// usage error must print the error, request help for that command (not the app
// help), and return a non-zero exit status.
func TestUsageErrorCallback(t *testing.T) {
	ctx := newTestContext() // ctx.Command.Name == "cmd"

	var helpedCommand string
	origCmdHelp := showCommandHelp
	showCommandHelp = func(_ *cli.Context, name string) error {
		helpedCommand = name
		return nil
	}
	defer func() { showCommandHelp = origCmdHelp }()

	appHelpCalls := 0
	origAppHelp := showAppHelpAndExit
	showAppHelpAndExit = func(*cli.Context, int) { appHelpCalls++ }
	defer func() { showAppHelpAndExit = origAppHelp }()

	var err error
	out := captureStdout(t, func() {
		err = UsageErrorCallback(ctx, errors.New("bad flag"), false)
	})

	if helpedCommand != "cmd" {
		t.Fatalf("expected command help for %q, got %q", "cmd", helpedCommand)
	}
	if appHelpCalls != 0 {
		t.Fatalf("command-scoped usage error must not show app help (%d calls)", appHelpCalls)
	}
	assertOutputContains(t, out, "bad flag")
	assertExitCoder(t, err, 1)
}

func TestHelp(t *testing.T) {
	ctx := newTestContext()
	called := false
	orig := showAppHelpAndExit
	showAppHelpAndExit = func(*cli.Context, int) {
		called = true
	}
	defer func() { showAppHelpAndExit = orig }()

	if err := Help(ctx); err != nil {
		t.Fatalf("Help: %v", err)
	}
	if !called {
		t.Fatalf("expected help to be called")
	}
}

func TestHelpWithCommandArg(t *testing.T) {
	app := cli.NewApp()
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	_ = set.Parse([]string{"list"})
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: "help"}
	called := false
	orig := showCommandHelp
	showCommandHelp = func(*cli.Context, string) error {
		called = true
		return nil
	}
	defer func() { showCommandHelp = orig }()

	if err := Help(ctx); err != nil {
		t.Fatalf("Help: %v", err)
	}
	if !called {
		t.Fatalf("expected command help to be called")
	}
}

func TestHelpWithCommandError(t *testing.T) {
	app := cli.NewApp()
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	_ = set.Parse([]string{"list"})
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: "help"}
	orig := showCommandHelp
	showCommandHelp = func(*cli.Context, string) error {
		return errors.New("boom")
	}
	defer func() { showCommandHelp = orig }()

	if err := Help(ctx); err == nil {
		t.Fatalf("expected error from Help")
	}
}

func TestGetVersion(t *testing.T) {
	old := VersionCmdStr
	VersionCmdStr = "v1.2.3"
	defer func() { VersionCmdStr = old }()

	if err := GetVersion(newTestContext()); err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
}

func TestPrintErrWithHelpVersion(t *testing.T) {
	old := VersionCmdStr
	VersionCmdStr = "v0"
	defer func() { VersionCmdStr = old }()

	if err := PrintErrWithHelp(newTestContext(), errors.New("flag provided but not defined: -v")); err != nil {
		t.Fatalf("PrintErrWithHelp: %v", err)
	}
}

func TestPrintErrWithHelpDoesNotTreatSubstringAsVersion(t *testing.T) {
	ctx := newTestContext()
	called := false
	orig := showAppHelpAndExit
	showAppHelpAndExit = func(*cli.Context, int) { called = true }
	defer func() { showAppHelpAndExit = orig }()

	_ = PrintErrWithHelp(ctx, errors.New("invalid value"))
	if !called {
		t.Fatal("expected ordinary invalid-value error to show help")
	}
}

// TestUsageErrorCallbackNoCommand pins app-level routing: without a command in
// scope the usage error must print the diagnostic, fall through to application
// help with exit status 1, and never ask for command help.
func TestUsageErrorCallbackNoCommand(t *testing.T) {
	ctx := newTestContext()
	ctx.Command = cli.Command{Name: ""}

	commandHelpCalls := 0
	origCmdHelp := showCommandHelp
	showCommandHelp = func(*cli.Context, string) error {
		commandHelpCalls++
		return nil
	}
	defer func() { showCommandHelp = origCmdHelp }()

	appHelpExit := -1
	origAppHelp := showAppHelpAndExit
	showAppHelpAndExit = func(_ *cli.Context, exitCode int) { appHelpExit = exitCode }
	defer func() { showAppHelpAndExit = origAppHelp }()

	var err error
	out := captureStdout(t, func() {
		err = UsageErrorCallback(ctx, errors.New("oops"), false)
	})

	if commandHelpCalls != 0 {
		t.Fatalf("app-level usage error must not show command help (%d calls)", commandHelpCalls)
	}
	if appHelpExit != 1 {
		t.Fatalf("app help exit code = %d, want 1", appHelpExit)
	}
	assertOutputContains(t, out, "oops")
	assertExitCoder(t, err, 1)
}

func TestSetShowAppHelpAndExit(t *testing.T) {
	wasCalled := false
	customFn := func(*cli.Context, int) { wasCalled = true }
	prev := SetShowAppHelpAndExit(customFn)
	if prev == nil {
		t.Fatal("expected previous function to be returned")
	}
	// Test that the new function is now set
	showAppHelpAndExit(nil, 0)
	if !wasCalled {
		t.Fatal("expected custom function to be called")
	}
	SetShowAppHelpAndExit(prev) // restore
}

func TestSetShowCommandHelp(t *testing.T) {
	wasCalled := false
	customFn := func(*cli.Context, string) error {
		wasCalled = true
		return nil
	}
	prev := SetShowCommandHelp(customFn)
	if prev == nil {
		t.Fatal("expected previous function to be returned")
	}
	// Test that the new function is now set
	_ = showCommandHelp(nil, "")
	if !wasCalled {
		t.Fatal("expected custom function to be called")
	}
	SetShowCommandHelp(prev) // restore
}
