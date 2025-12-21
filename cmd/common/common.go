package common

import (
	"fmt"
	"os"
	"strings"

	"github.com/urfave/cli"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

var VersionCmdStr string

var (
	showAppHelpAndExit = cli.ShowAppHelpAndExit
	showCommandHelp    = cli.ShowCommandHelp
)

func InitBars(p *mpb.Progress, prefix string, cLength int64) (dbar *mpb.Bar, cbar *mpb.Bar) {
	barStyle := mpb.BarStyle().Lbound("╢").Filler("█").Tip("█").Padding("░").Rbound("╟")

	name := prefix + "Downloading"

	dbar = p.New(0,
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
	dbar.SetTotal(cLength, false)
	dbar.EnableTriggerComplete()

	name = prefix + "Compiling"
	cbar = p.New(0,
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
	cbar.SetTotal(cLength, false)
	cbar.EnableTriggerComplete()
	return
}

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

func GetVersion(ctx *cli.Context) error {
	fmt.Println(VersionCmdStr)
	return nil
}

func PrintRuntimeErr(ctx *cli.Context, cmd, action string, err error) {
	if err == nil {
		fmt.Println("err is nil", "[", cmd, "|", action, "]")
		return
	}
	var name string
	if ctx != nil {
		name = ctx.App.HelpName
	} else {
		name = os.Args[0]
	}
	fmt.Printf("%s: %s[%s]: %s\n", name, cmd, action, err.Error())
}

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
	if strings.Contains(estr, "-version") ||
		strings.Contains(estr, "-v") {
		return GetVersion(ctx)
	}
	fmt.Printf("%s: %s\n\n", ctx.App.HelpName, err.Error())
	callback()
	return nil
}

func UsageErrorCallback(ctx *cli.Context, err error, _ bool) error {
	if ctx.Command.Name != "" {
		return PrintErrWithCmdHelp(ctx, err)
	}
	return PrintErrWithHelp(ctx, err)
}

func Beaut(s string, n int) (b string) {
	n1 := len(s)
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
