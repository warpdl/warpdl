package ext

import "github.com/urfave/cli"

var Commands = []cli.Command{
	{
		Name:   "install",
		Action: install,
		Usage:  "install a warpdl extension",
	},
	{
		Name:   "uninstall",
		Action: uninstall,
		Usage:  "uninstall a warpdl extension",
	},
	{
		Name:   "info",
		Action: info,
		Usage:  "show info about a warpdl extension",
	},
	{
		Name:   "list",
		Action: list,
		Usage:  "list installed warpdl extensions",
		Flags:  lsFlags,
	},
	{
		Name:   "activate",
		Action: activate,
		Usage:  "activate an unactivated warpdl extension",
	},
	{
		Name:   "deactivate",
		Action: deactivate,
		Usage:  "deactivate a warpdl extension",
	},
}
