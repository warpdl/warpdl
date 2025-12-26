package common

import (
	"errors"
	"flag"
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

func TestInitBars(t *testing.T) {
	p := mpb.New()
	dbar, cbar := InitBars(p, "", 100)
	if dbar == nil || cbar == nil {
		t.Fatalf("expected bars")
	}
}

// TestInitBarsWithProgress verifies that progress bars can be initialized
// with a non-zero starting position for resume scenarios.
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

func TestPrintRuntimeErr(t *testing.T) {
	PrintRuntimeErr(nil, "cmd", "action", nil)
	PrintRuntimeErr(newTestContext(), "cmd", "action", errors.New("boom"))
}

func TestPrintErrWithHelp(t *testing.T) {
	ctx := newTestContext()
	called := false
	orig := showAppHelpAndExit
	showAppHelpAndExit = func(*cli.Context, int) {
		called = true
	}
	defer func() { showAppHelpAndExit = orig }()

	if err := PrintErrWithHelp(ctx, errors.New("oops")); err != nil {
		t.Fatalf("PrintErrWithHelp: %v", err)
	}
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

	if err := PrintErrWithCmdHelp(ctx, errors.New("oops")); err != nil {
		t.Fatalf("PrintErrWithCmdHelp: %v", err)
	}
	if !called {
		t.Fatalf("expected command help to be called")
	}
}

func TestPrintErrWithCmdHelp_ShowCommandHelpError(t *testing.T) {
	ctx := newTestContext()
	orig := showCommandHelp
	showCommandHelp = func(*cli.Context, string) error {
		return errors.New("boom")
	}
	defer func() { showCommandHelp = orig }()

	if err := PrintErrWithCmdHelp(ctx, errors.New("oops")); err != nil {
		t.Fatalf("PrintErrWithCmdHelp: %v", err)
	}
}

func TestUsageErrorCallback(t *testing.T) {
	ctx := newTestContext()
	orig := showCommandHelp
	showCommandHelp = func(*cli.Context, string) error { return nil }
	defer func() { showCommandHelp = orig }()

	if err := UsageErrorCallback(ctx, errors.New("oops"), false); err != nil {
		t.Fatalf("UsageErrorCallback: %v", err)
	}
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

	if err := PrintErrWithHelp(newTestContext(), errors.New("bad -v")); err != nil {
		t.Fatalf("PrintErrWithHelp: %v", err)
	}
}

func TestUsageErrorCallbackNoCommand(t *testing.T) {
	app := cli.NewApp()
	set := flag.NewFlagSet("test", flag.ContinueOnError)
	ctx := cli.NewContext(app, set, nil)
	ctx.Command = cli.Command{Name: ""}
	orig := showAppHelpAndExit
	showAppHelpAndExit = func(*cli.Context, int) {}
	defer func() { showAppHelpAndExit = orig }()

	if err := UsageErrorCallback(ctx, errors.New("oops"), false); err != nil {
		t.Fatalf("UsageErrorCallback: %v", err)
	}
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
