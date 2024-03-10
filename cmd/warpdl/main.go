package main

import (
	"fmt"
	"log"
	"os"

	"github.com/urfave/cli"
	api "github.com/warpdl/warpdl/internal/api"
	"github.com/warpdl/warpdl/internal/server"
)

func Execute(args []string) error {
	app := cli.App{
		Name:                  "WarpDL",
		HelpName:              "warpdl",
		Usage:                 "An ultra fast download manager.",
		Version:               fmt.Sprintf("%s-%s", version, BuildType),
		UsageText:             "warpdl <command> [arguments...]",
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
					s, err := api.NewApi(log.Default())
					if err != nil {
						panic(err)
					}
					serv := server.NewServer(log.Default())
					s.RegisterHandlers(serv)
					return serv.Start()
				},
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
	return app.Run(args)
}

func main() {
	err := Execute(os.Args)
	if err != nil {
		fmt.Printf("warpdl: %s\n", err.Error())
		os.Exit(1)
	}
}
