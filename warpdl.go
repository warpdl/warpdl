package main

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"strings"

	"github.com/urfave/cli"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
	"github.com/warpdl/warpdl/internal/server"
	"github.com/warpdl/warpdl/internal/service"
)

func initBars(p *mpb.Progress, prefix string, cLength int64) (dbar *mpb.Bar, cbar *mpb.Bar) {
	barStyle := mpb.BarStyle().Lbound("╢").Filler("█").Tip("█").Padding("░").Rbound("╟")

	name := prefix + "Downloading"

	dbar = p.New(0,
		barStyle,
		mpb.PrependDecorators(
			decor.Name(name, decor.WC{W: len(name) + 1, C: decor.DidentRight}),
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

func getVersion(ctx *cli.Context) error {
	fmt.Printf(
		"%s %s (%s_%s)\nBuild: %s=%s\n",
		ctx.App.Name,
		ctx.App.Version,
		runtime.GOOS,
		runtime.GOARCH,
		date, commit,
	)
	return nil
}

func printRuntimeErr(ctx *cli.Context, cmd, action string, err error) {
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
		return getVersion(ctx)
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

func main() {
	app := cli.App{
		Name:                  "Warp",
		HelpName:              "warp",
		Usage:                 "An ultra fast download manager.",
		Version:               fmt.Sprintf("%s-%s", version, BuildType),
		UsageText:             "warp <command> [arguments...]",
		Description:           DESCRIPTION,
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
				Flags:              infoFlags,
			},
			{
				Name: "daemon",
				Action: func(ctx *cli.Context) error {
					s, err := service.NewService(log.Default())
					if err != nil {
						panic(err)
					}
					serv := server.NewServer(log.Default())
					s.RegisterHandlers(serv)
					return serv.Start()
				},
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
				Name:                   "flush",
				Aliases:                []string{"c"},
				Usage:                  "flush the user download history",
				Description:            FlushDescription,
				OnUsageError:           usageErrorCallback,
				CustomHelpTemplate:     CMD_HELP_TEMPL,
				Action:                 flush,
				UseShortOptionHandling: true,
				Flags:                  flsFlags,
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
				Action:             getVersion,
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
