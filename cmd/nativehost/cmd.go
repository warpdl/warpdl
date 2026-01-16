// Package nativehost provides CLI commands for managing native messaging host integration.
package nativehost

import "github.com/urfave/cli"

// Commands contains all native-host related subcommands.
var Commands = []cli.Command{
	{
		Name:   "install",
		Action: install,
		Usage:  "install native messaging manifest for browsers",
		Flags:  installFlags,
	},
	{
		Name:   "uninstall",
		Action: uninstall,
		Usage:  "remove native messaging manifest from browsers",
		Flags:  uninstallFlags,
	},
	{
		Name:   "run",
		Action: run,
		Usage:  "run native messaging host (called by browser)",
		Hidden: true, // Hidden from help as it's called by browsers
	},
	{
		Name:   "status",
		Action: status,
		Usage:  "show installation status for all browsers",
	},
}

var installFlags = []cli.Flag{
	cli.StringFlag{
		Name:  "browser",
		Usage: "browser to install for (chrome, firefox, chromium, edge, brave, all)",
		Value: "all",
	},
	cli.StringFlag{
		Name:  "chrome-extension-id",
		Usage: "Chrome extension ID (required for Chrome-based browsers)",
	},
	cli.StringFlag{
		Name:  "firefox-extension-id",
		Usage: "Firefox extension ID (required for Firefox)",
	},
	cli.BoolFlag{
		Name:  "auto",
		Usage: "use default extension IDs (for package manager hooks)",
	},
}

var uninstallFlags = []cli.Flag{
	cli.StringFlag{
		Name:  "browser",
		Usage: "browser to uninstall from (chrome, firefox, chromium, edge, brave, all)",
		Value: "all",
	},
}
