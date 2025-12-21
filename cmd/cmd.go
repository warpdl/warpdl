// Package cmd implements the command-line interface for WarpDL.
// It provides commands for downloading files, managing downloads,
// controlling the daemon, and handling extensions.
package cmd

import (
	"fmt"
	"runtime"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/cmd/ext"
)

// BuildArgs contains build-time information passed to the CLI application.
// These values are typically injected during the build process via ldflags
// and are used to display version and build information to users.
type BuildArgs struct {
	// Version is the semantic version of the application.
	Version string
	// BuildType indicates the build variant (e.g., "release", "debug", "snapshot").
	BuildType string
	// Date is the build timestamp in a human-readable format.
	Date string
	// Commit is the git commit hash from which the build was created.
	Commit string
}

// Execute initializes and runs the CLI application with the provided arguments.
// It configures all available commands (download, resume, list, daemon, ext, etc.)
// and their respective flags, then executes the appropriate command based on
// user input. The default action is the download command when no subcommand
// is specified. It returns any error encountered during command execution.
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
				Name:        "ext",
				Usage:       "manage warpdl extensions",
				Subcommands: ext.Commands,
			},
			{
				Name:   "daemon",
				Action: daemon,
				Usage:  "start the warpdl daemon",
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
				Usage:  "stop a running download",
			},
			{
				Name:   "attach",
				Action: attach,
				Usage:  "attach console to a running download",
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
