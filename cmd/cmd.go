// Package cmd implements the command-line interface for WarpDL.
// It provides commands for downloading files, managing downloads,
// controlling the daemon, and handling extensions.
package cmd

import (
	"fmt"
	"os"
	"runtime"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/cmd/ext"
	"github.com/warpdl/warpdl/cmd/nativehost"
	sharedcommon "github.com/warpdl/warpdl/common"
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

// currentBuildArgs stores the build arguments for use by daemon and other commands.
var currentBuildArgs BuildArgs

// globalFlags are flags that apply to daemon-connecting commands
var globalFlags = []cli.Flag{
	cli.StringFlag{
		Name:        "daemon-uri",
		Usage:       "daemon URI (unix:///path, tcp://host:port, pipe://name)",
		EnvVar:      "WARPDL_DAEMON_URI",
		Destination: &daemonURI,
	},
	cli.StringSliceFlag{
		Name:  "cookie",
		Usage: "HTTP cookie(s) for authentication (format: 'name=value', can be specified multiple times)",
	},
	cli.BoolFlag{
		Name:   "debug, d",
		Usage:  "enable debug logging for troubleshooting",
		EnvVar: sharedcommon.DebugEnv,
	},
}

// GetApp returns the configured CLI application for documentation generation
// and other programmatic uses. It builds the complete command structure with
// all flags, descriptions, and help templates. This function is useful when
// you need access to the app structure without running it (e.g., for generating
// documentation or inspecting available commands).
func GetApp(bArgs BuildArgs) *cli.App {
	// Build the base commands
	commands := []cli.Command{
		{
			Name:        "ext",
			Usage:       "manage warpdl extensions",
			Subcommands: ext.Commands,
		},
		{
			Name:        "native-host",
			Usage:       "manage native messaging host for browser extensions",
			Subcommands: nativehost.Commands,
		},
		{
			Name:   "daemon",
			Action: getDaemonAction(),
			Usage:  "start the warpdl daemon",
			Flags: []cli.Flag{
				cli.IntFlag{
					Name:   "max-concurrent",
					Usage:  "maximum number of concurrent downloads (0 = unlimited)",
					Value:  3,
					EnvVar: "WARPDL_MAX_CONCURRENT",
				},
			},
		},
		{
			Name:   "stop-daemon",
			Action: stopDaemon,
			Usage:  "stop the running daemon gracefully",
			Flags:  globalFlags,
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
			Flags:  globalFlags,
		},
		{
			Name:   "attach",
			Action: attach,
			Usage:  "attach console to a running download",
			Flags:  globalFlags,
		},
		{
			Name:                   "download",
			Aliases:                []string{"d"},
			Usage:                  "fastly download a file ",
			CustomHelpTemplate:     CMD_HELP_TEMPL,
			OnUsageError:           common.UsageErrorCallback,
			Action:                 download,
			Flags:                  append(dlFlags, globalFlags...),
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
			Flags:                  append(lsFlags, globalFlags...),
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
			Flags:                  append(rsFlags, globalFlags...),
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
			Flags:                  append(flsFlags, globalFlags...),
		},
		queueCmd,
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
	}

	// Add platform-specific commands
	platformCommands := getPlatformCommands()
	if len(platformCommands) > 0 {
		commands = append(commands, platformCommands...)
	}

	return &cli.App{
		Name:                   "warpdl",
		HelpName:               "warpdl",
		Usage:                  "An ultra fast download manager.",
		Version:                fmt.Sprintf("%s-%s", bArgs.Version, bArgs.BuildType),
		UsageText:              "warpdl <command> [arguments...]",
		Description:            DESCRIPTION,
		CustomAppHelpTemplate:  HELP_TEMPL,
		OnUsageError:           common.UsageErrorCallback,
		Commands:               commands,
		Action:                 download,
		Flags:                  append(dlFlags, globalFlags...),
		UseShortOptionHandling: true,
		HideHelp:               true,
		HideVersion:            true,
		Before: func(ctx *cli.Context) error {
			// Set WARPDL_DEBUG=1 if --debug/-d flag is set
			// This enables debug logging throughout the application
			if ctx.GlobalBool("debug") {
				_ = os.Setenv(sharedcommon.DebugEnv, "1")
			}
			return nil
		},
	}
}

// Execute initializes and runs the CLI application with the provided arguments.
// It configures all available commands (download, resume, list, daemon, ext, etc.)
// and their respective flags, then executes the appropriate command based on
// user input. The default action is the download command when no subcommand
// is specified. It returns any error encountered during command execution.
func Execute(args []string, bArgs BuildArgs) error {
	// Store build args for use by daemon and other commands
	currentBuildArgs = bArgs

	// Get the configured CLI application
	app := GetApp(bArgs)

	// Set the version command string for global use
	common.VersionCmdStr = fmt.Sprintf("%s %s (%s_%s)\nBuild: %s=%s\n",
		app.Name,
		app.Version,
		runtime.GOOS,
		runtime.GOARCH,
		bArgs.Date, bArgs.Commit,
	)

	return app.Run(args)
}
