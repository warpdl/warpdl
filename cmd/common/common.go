// Package common provides shared utilities and helper functions for CLI commands.
// It includes progress bar initialization, error handling, help display,
// and text formatting utilities used across the WarpDL command-line interface.
package common

import (
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

// VersionCmdStr holds the formatted version string displayed by the version command.
// It is populated at runtime by the Execute function with build-time information
// including version, platform, build date, and commit hash.
var VersionCmdStr string

var (
	showAppHelpAndExit = cli.ShowAppHelpAndExit
	showCommandHelp    = cli.ShowCommandHelp
)

// SetShowAppHelpAndExit sets the function used to show app help and exit.
// It returns the previous function, allowing for restoration after testing.
// This is primarily used for testing to avoid os.Exit calls.
func SetShowAppHelpAndExit(fn func(*cli.Context, int)) func(*cli.Context, int) {
	prev := showAppHelpAndExit
	showAppHelpAndExit = fn
	return prev
}

// SetShowCommandHelp sets the function used to show command help.
// It returns the previous function, allowing for restoration after testing.
// This is primarily used for testing.
func SetShowCommandHelp(fn func(*cli.Context, string) error) func(*cli.Context, string) error {
	prev := showCommandHelp
	showCommandHelp = fn
	return prev
}

// InitBars creates and configures download and compile progress bars.
// It returns two progress bars: dbar for tracking download progress and
// cbar for tracking file compilation/assembly progress. The prefix parameter
// is prepended to bar labels, and cLength specifies the total content length
// for progress calculation. Both bars use a visual block-style display.
//
// The total is passed at construction time: since mpb v8.12.0 a bar
// created with BarQueueAfter has no serve goroutine until it is dequeued,
// so post-construction calls like SetTotal or EnableTriggerComplete on the
// queued bar block forever. A positive construction total also enables the
// complete trigger, so this works identically on v8.11.x. Note that mpb is
// pinned to v8.11.3 in go.mod because the download handlers in cmd/client.go
// also operate on the queued bar while it is still queued, which deadlocks
// under v8.12.x semantics.
func InitBars(p *mpb.Progress, prefix string, cLength int64) (dbar *mpb.Bar, cbar *mpb.Bar) {
	barStyle := mpb.BarStyle().Lbound("╢").Filler("█").Tip("█").Padding("░").Rbound("╟")

	name := prefix + "Downloading"

	dbar = p.New(cLength,
		barStyle,
		mpb.PrependDecorators(
			decor.Name(name, decor.WC{W: len(name) + 1, C: decor.DindentRight}),
			decor.OnComplete(
				decor.EwmaETA(decor.ET_STYLE_GO, 30, decor.WC{W: 4}), "Complete",
			),
		),
		mpb.AppendDecorators(
			decor.EwmaSpeed(decor.SizeB1024(0), "% .2f", 30),
		),
	)

	name = prefix + "Compiling"
	cbar = p.New(cLength,
		barStyle,
		mpb.BarQueueAfter(dbar),
		mpb.PrependDecorators(
			decor.Name(name, decor.WC{W: len(name) + 1, C: decor.DindentRight}),
			decor.OnComplete(
				decor.AverageETA(decor.ET_STYLE_GO, decor.WC{W: 4}), "Complete",
			),
		),
		mpb.AppendDecorators(
			decor.AverageSpeed(decor.SizeB1024(0), "% .2f"),
		),
	)
	return
}

// InitBarsWithProgress creates progress bars with an initial progress value.
// This is used when resuming downloads where some bytes are already downloaded.
// The initialProgress parameter sets the starting position of the download bar.
func InitBarsWithProgress(p *mpb.Progress, prefix string, cLength int64, initialProgress int64) (dbar *mpb.Bar, cbar *mpb.Bar) {
	dbar, cbar = InitBars(p, prefix, cLength)
	if initialProgress > 0 {
		dbar.SetCurrent(initialProgress)
	}
	return
}

// Help displays help information for the application or a specific command.
// If no argument is provided or the argument is "help", it displays the
// application-level help and exits. Otherwise, it shows help for the
// specified command name.
func Help(ctx *cli.Context) error {
	arg := ctx.Args().First()
	if arg == "" || arg == "help" {
		fmt.Printf("%s %s\n", ctx.App.Name, ctx.App.Version)
		showAppHelpAndExit(ctx, 0)
		return nil
	}
	err := showCommandHelp(ctx, arg)
	if err != nil {
		return err
	}
	err = PrintErrWithHelp(ctx, err)
	if err != nil {
		return err
	}
	return nil
}

// GetVersion prints the version string to stdout and returns nil.
// The version string includes the application name, version, platform,
// build date, and commit hash as configured in VersionCmdStr.
func GetVersion(ctx *cli.Context) error {
	fmt.Println(VersionCmdStr)
	return nil
}

// PrintRuntimeErr formats and prints a runtime error message to stderr and
// returns an ExitCoder so callers cannot accidentally report success.
// It includes the application name, command name, action identifier, and
// the error message. If err is nil, it prints a diagnostic message indicating
// no error was present. The ctx parameter may be nil, in which case the
// application name is derived from os.Args[0].
func PrintRuntimeErr(ctx *cli.Context, cmd, action string, err error) error {
	if err == nil {
		return nil
	}
	var name string
	if ctx != nil {
		name = ctx.App.HelpName
	} else {
		name = os.Args[0]
	}
	fmt.Fprintf(os.Stderr, "%s: %s[%s]: %s\n", name, cmd, action, err.Error())
	// The diagnostic has already been rendered above. Returning an empty
	// ExitError preserves the non-zero status without printing it twice.
	return cli.NewExitError("", 1)
}

// PrintErrWithCmdHelp prints the error message followed by the current
// command's help text. It is used for errors that occur in the context
// of a specific subcommand.
func PrintErrWithCmdHelp(ctx *cli.Context, err error) error {
	return printErrWithCallback(
		ctx,
		err,
		func() {
			err := showCommandHelp(ctx, ctx.Command.Name)
			if err != nil {
				fmt.Println(err.Error())
			}
		},
	)
}

// PrintErrWithHelp prints the error message followed by the application-level
// help text and exits with status code 1. It is used for errors that occur
// at the application level rather than within a specific command.
func PrintErrWithHelp(ctx *cli.Context, err error) error {
	return printErrWithCallback(
		ctx,
		err,
		func() {
			showAppHelpAndExit(ctx, 1)
		},
	)
}

func printErrWithCallback(ctx *cli.Context, err error, callback func()) error {
	if err == nil {
		return nil
	}
	estr := strings.ToLower(err.Error())
	if estr == "flag: help requested" {
		return Help(ctx)
	}
	if estr == "flag provided but not defined: -version" ||
		estr == "flag provided but not defined: -v" {
		return GetVersion(ctx)
	}
	fmt.Printf("%s: %s\n\n", ctx.App.HelpName, err.Error())
	callback()
	return cli.NewExitError("", 1)
}

// UsageErrorCallback handles usage errors from the CLI framework.
// It determines whether the error occurred at the command level or
// application level and displays the appropriate help text along with
// the error message. This function is designed to be used as the
// OnUsageError callback for cli.App and cli.Command.
func UsageErrorCallback(ctx *cli.Context, err error, _ bool) error {
	if ctx.Command.Name != "" {
		return PrintErrWithCmdHelp(ctx, err)
	}
	return PrintErrWithHelp(ctx, err)
}

// Beaut centers a string within a field of width n by padding with spaces.
// If the string length is less than n, spaces are added equally on both sides.
// If n minus the string length is odd, an extra space is appended at the end.
// This is useful for creating centered text in fixed-width displays.
func Beaut(s string, n int) (b string) {
	if n <= 0 {
		return ""
	}
	n1 := len(s)
	if n1 >= n {
		return s
	}
	x := n - n1
	x1 := x / 2
	w := string(
		replic(' ', x1),
	)
	b = w
	b += s
	b += w
	if x%2 != 0 {
		b += " "
	}
	return
}

func replic[aT any](v aT, n int) []aT {
	a := make([]aT, n)
	for i := range a {
		a[i] = v
	}
	return a
}
