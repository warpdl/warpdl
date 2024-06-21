package cmd

import (
	"fmt"
	"runtime"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/cmd/ext"
)

type BuildArgs struct {
	Version   string
	BuildType string
	Date      string
	Commit    string
}

func Execute(args []string, bArgs BuildArgs) error {
	app := cli.App{
		Name:                  "warpdl",
		HelpName:              "warpdl",
		Usage:                 "An ultra fast download manager.",
		Version:               fmt.Sprintf("%s-%s", bArgs.Version, bArgs.BuildType),
		UsageText:             "warpdl <command> [arguments...]",
		Description:           DESCRIPTION,
		CustomAppHelpTemplate: HELP_TEMPL,
		OnUsageError:          common.UsageErrorCallback,
		Commands: []cli.Command{
			{
				Name: "ext",
				Subcommands: []cli.Command{
					{
						Name:   "install",
						Action: ext.Install,
					},
					{
						Name:   "info",
						Action: ext.Info,
					},
				},
			},
			{
				Name:   "daemon",
				Action: daemon,
			},
			{
				Name:               "info",
				Aliases:            []string{"i"},
				Usage:              "shows info about a file",
				Action:             info,
				OnUsageError:       common.UsageErrorCallback,
				CustomHelpTemplate: CMD_HELP_TEMPL,
				Description:        InfoDescription,
				Flags:              infoFlags,
			},
			{
				Name:   "stop",
				Action: stop,
			},
			{
				Name:   "attach",
				Action: attach,
			},
			{
				Name:                   "download",
				Aliases:                []string{"d"},
				Usage:                  "fastly download a file ",
				CustomHelpTemplate:     CMD_HELP_TEMPL,
				OnUsageError:           common.UsageErrorCallback,
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
				OnUsageError:           common.UsageErrorCallback,
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
				OnUsageError:           common.UsageErrorCallback,
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
				OnUsageError:           common.UsageErrorCallback,
				CustomHelpTemplate:     CMD_HELP_TEMPL,
				Action:                 flush,
				UseShortOptionHandling: true,
				Flags:                  flsFlags,
			},
			{
				Name:    "help",
				Aliases: []string{"h"},
				Usage:   "prints the help message",
				Action:  common.Help,
			},
			{
				Name:               "version",
				Aliases:            []string{"v"},
				Usage:              "prints installed version of warp",
				UsageText:          " ",
				CustomHelpTemplate: CMD_HELP_TEMPL,
				Action:             common.GetVersion,
			},
		},
		Action:                 download,
		Flags:                  dlFlags,
		UseShortOptionHandling: true,
		HideHelp:               true,
		HideVersion:            true,
	}
	common.VersionCmdStr = fmt.Sprintf("%s %s (%s_%s)\nBuild: %s=%s\n",
		app.Name,
		app.Version,
		runtime.GOOS,
		runtime.GOARCH,
		bArgs.Date, bArgs.Commit,
	)
	return app.Run(args)
}
