package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/urfave/cli"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
)

const HELP_TEMPL = `Usage: {{if .UsageText}}{{.UsageText}}{{else}}{{.HelpName}} {{if .VisibleFlags}}[global options]{{end}}{{if .Commands}} command [command options]{{end}} {{if .ArgsUsage}}{{.ArgsUsage}}{{else}}[arguments...]{{end}}{{end}}
{{.Description}}{{if .VisibleCommands}}
Commands:{{range .VisibleCategories}}{{if .Name}}

{{.Name}}:{{range .VisibleCommands}}
  {{join .Names ", "}}{{"\t"}}{{.Usage}}{{end}}{{else}}{{range .VisibleCommands}}
{{"\t"}}{{index .Names 0}}{{"\t:\t"}}{{.Usage}}{{end}}{{end}}{{end}}{{end}}{{if .VisibleFlags}}{{end}}

Use "{{.HelpName}} help <command>" for more information about any command.

`

const CMD_HELP_TEMPL = `{{if .Description}}{{.Description}}{{else}}{{.HelpName}} - {{.Usage}}

{{end}}Usage:
        {{.HelpName}} {{if .UsageText}}{{.UsageText}}{{else}}[arguments...]{{end}}{{if .VisibleFlags}}

Supported Flags:{{range .VisibleFlags}}
  {{.}}{{end}}{{end}}

`

var Description = `
Warp is a powerful and versatile cross-platform download manager. 
With its advanced technology, Warp has the ability to accelerate
your download speeds by up to 10 times, revolutionizing the way
you obtain files on any operating system.
`

func initBars(p *mpb.Progress, prefix string, cLength int64) (dbar *mpb.Bar, cbar *mpb.Bar) {
	barStyle := mpb.BarStyle().Lbound("╢").Filler("█").Tip("█").Padding("░").Rbound("╟")

	name := prefix + "Downloading"

	dbar = p.New(0,
		barStyle,
		mpb.PrependDecorators(
			decor.Name(name, decor.WC{W: len(name) + 1, C: decor.DidentRight}),
			decor.OnComplete(
				decor.AverageETA(decor.ET_STYLE_GO, decor.WC{W: 4}), "Complete",
			),
		),
		mpb.AppendDecorators(
			decor.AverageSpeed(decor.SizeB1024(0), "% .2f"),
		),
	)
	dbar.SetTotal(cLength, false)
	dbar.EnableTriggerComplete()

	name = prefix + "Compiling"
	cbar = p.New(0,
		barStyle,
		mpb.BarQueueAfter(dbar),
		mpb.PrependDecorators(
			decor.Name(name, decor.WC{W: len(name) + 1, C: decor.DidentRight}),
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

func help(ctx *cli.Context) error {
	arg := ctx.Args().First()
	if arg == "" || arg == "help" {
		fmt.Printf("%s %s\n", ctx.App.Name, ctx.App.Version)
		cli.ShowAppHelpAndExit(ctx, 0)
		return nil
	}
	err := cli.ShowCommandHelp(ctx, arg)
	if err != nil {
		return err
	}
	err = printErrWithHelp(ctx, err)
	if err != nil {
		return err
	}
	return nil
}

func version(ctx *cli.Context) error {
	fmt.Printf(
		"%s %s (%s_%s)\n",
		ctx.App.Name,
		ctx.App.Version,
		runtime.GOOS,
		runtime.GOARCH,
	)
	return nil
}

func printRuntimeErr(ctx *cli.Context, cmd string, err error) {
	fmt.Printf("%s: %s: %s\n", ctx.App.HelpName, cmd, err.Error())
}

func printErrWithCmdHelp(ctx *cli.Context, err error) error {
	return printErrWithCallback(
		ctx,
		err,
		func() {
			err := cli.ShowCommandHelp(ctx, ctx.Command.Name)
			if err != nil {
				fmt.Println(err.Error())
			}
		},
	)
}

func printErrWithHelp(ctx *cli.Context, err error) error {
	return printErrWithCallback(
		ctx,
		err,
		func() {
			cli.ShowAppHelpAndExit(ctx, 1)
		},
	)
}

func printErrWithCallback(ctx *cli.Context, err error, callback func()) error {
	if err == nil {
		return nil
	}
	estr := strings.ToLower(err.Error())
	if estr == "flag: help requested" {
		return help(ctx)
	}
	if strings.Contains(estr, "-version") ||
		strings.Contains(estr, "-v") {
		return version(ctx)
	}
	fmt.Printf("%s: %s\n\n", ctx.App.HelpName, err.Error())
	callback()
	return nil
}

func usageErrorCallback(ctx *cli.Context, err error, _ bool) error {
	if ctx.Command.Name != "" {
		return printErrWithCmdHelp(ctx, err)
	}
	return printErrWithHelp(ctx, err)
}

var (
	maxParts   int
	maxConns   int
	dlPath     string
	fileName   string
	forceParts bool
	timeTaken  bool
)

var dlFlags = []cli.Flag{
	cli.IntFlag{
		Name:        "max-parts, s",
		Usage:       "to specify the number of maximum file segments",
		EnvVar:      "WARP_MAX_PARTS",
		Destination: &maxParts,
		Value:       200,
	},
	cli.IntFlag{
		Name:        "max-connection, x",
		Usage:       "specify the number of maximum parallel connection",
		EnvVar:      "WARP_MAX_CONN",
		Destination: &maxConns,
		Value:       24,
	},
	cli.StringFlag{
		Name:        "file-name, o",
		Usage:       "explicitly set the name of file (determined automatically if not specified)",
		Destination: &fileName,
	},
	cli.StringFlag{
		Name:        "download-path, l",
		Usage:       "set the path where downloaded file should be saved",
		Value:       ".",
		Destination: &dlPath,
	},
	cli.BoolTFlag{
		Name:        "force-parts, f",
		Usage:       "forceful file segmentation (default: true)",
		EnvVar:      "WARP_FORCE_SEGMENTS",
		Destination: &forceParts,
	},
	cli.BoolFlag{
		Name:        "time-taken, e",
		Destination: &timeTaken,
		Hidden:      true,
	},
}

var rsFlags = []cli.Flag{
	cli.IntFlag{
		Name:        "max-parts, s",
		Usage:       "to specify the number of maximum file segments",
		EnvVar:      "WARP_MAX_PARTS",
		Destination: &maxParts,
		Value:       200,
	},
	cli.IntFlag{
		Name:        "max-connection, x",
		Usage:       "specify the number of maximum parallel connection",
		EnvVar:      "WARP_MAX_CONN",
		Destination: &maxConns,
		Value:       24,
	},
	cli.BoolTFlag{
		Name:        "force-parts, f",
		Usage:       "forceful file segmentation (default: true)",
		EnvVar:      "WARP_FORCE_SEGMENTS",
		Destination: &forceParts,
	},
	cli.BoolFlag{
		Name:        "time-taken, e",
		Destination: &timeTaken,
		Hidden:      true,
	},
}

var (
	showHidden    bool
	showCompleted bool
	showPending   bool
	showAll       bool
)

var lsFlags = []cli.Flag{
	cli.BoolFlag{
		Name:        "show-completed, c",
		Usage:       "use this flag to list completed downloads (default: false)",
		Destination: &showCompleted,
	},
	cli.BoolTFlag{
		Name:        "show-pending, p",
		Usage:       "use this flag to include pending downloads (default: true)",
		Destination: &showPending,
	},
	cli.BoolFlag{
		Name:        "show-all, a",
		Usage:       "use this flag to list all downloads (default: false)",
		Destination: &showAll,
	},
	cli.BoolFlag{
		Name:        "show-hidden, g",
		Usage:       "use this flag to list hidden downloads (default: false)",
		Destination: &showHidden,
	},
}

const (
	ListDescription = `The list command displays a list of incomplete 
downloads along with their unique download hashes
which can be used to resume pending downloads.

Example:
        warpdl list

`
	InfoDescription = `The info command makes a GET request to the entered 
url and and tries to fetch the basic file info like 
name, size etc.

Example:
        warpdl info https://domain.com/file.zip

`
	DownloadDescription = `The download command lets you quickly fetch and save 
files from the internet. You can initiate the download
process and securely store the desired file on your 
local system.

Warp uses dynamic file segmentation technique by default
to download files fastly by utilizing the full alloted 
bandwidth 

Example:
        warpdl https://domain.com/file.zip
					OR
        warpdl download https://domain.com/file.zip

`
	ResumeDescription = `The resume command lets you resume an incomplete download
using its unique download hash which you can retrieve by 
using "warpdl list" command.

Example:
        warpdl resume <unique download hash>

`
)

func main() {
	app := cli.App{
		Name:                  "Warp",
		HelpName:              "warp",
		Usage:                 "An ultra fast download manager.",
		Version:               VERSION,
		UsageText:             "warp <command> [arguments...]",
		Description:           Description,
		CustomAppHelpTemplate: HELP_TEMPL,
		OnUsageError:          usageErrorCallback,
		Commands: []cli.Command{
			{
				Name:               "info",
				Aliases:            []string{"i"},
				Usage:              "shows info about a file",
				Action:             info,
				OnUsageError:       usageErrorCallback,
				CustomHelpTemplate: CMD_HELP_TEMPL,
				Description:        InfoDescription,
			},
			{
				Name:                   "download",
				Aliases:                []string{"d"},
				Usage:                  "fastly download a file ",
				CustomHelpTemplate:     CMD_HELP_TEMPL,
				OnUsageError:           usageErrorCallback,
				Action:                 download,
				Flags:                  dlFlags,
				UseShortOptionHandling: true,
				Description:            DownloadDescription,
			},
			{
				Name:                   "list",
				Aliases:                []string{"l"},
				Usage:                  "display downloads history",
				Action:                 list,
				OnUsageError:           usageErrorCallback,
				CustomHelpTemplate:     CMD_HELP_TEMPL,
				Description:            ListDescription,
				UseShortOptionHandling: true,
				Flags:                  lsFlags,
			},
			{
				Name:                   "resume",
				Aliases:                []string{"r"},
				Usage:                  "resume an incomplete download",
				Description:            ResumeDescription,
				OnUsageError:           usageErrorCallback,
				CustomHelpTemplate:     CMD_HELP_TEMPL,
				Action:                 resume,
				UseShortOptionHandling: true,
				Flags:                  rsFlags,
			},
			{
				Name:    "help",
				Aliases: []string{"h"},
				Usage:   "prints the help message",
				Action:  help,
			},
			{
				Name:               "version",
				Aliases:            []string{"v"},
				Usage:              "prints installed version of warp",
				UsageText:          " ",
				CustomHelpTemplate: CMD_HELP_TEMPL,
				Action:             version,
			},
		},
		Action:                 download,
		Flags:                  dlFlags,
		UseShortOptionHandling: true,
		HideHelp:               true,
		HideVersion:            true,
	}
	err := app.Run(os.Args)
	if err != nil {
		fmt.Printf("warp: %s\n", err.Error())
	}
}
