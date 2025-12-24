//go:build windows

package cmd

import "github.com/urfave/cli"

// getPlatformCommands returns Windows-specific CLI commands.
func getPlatformCommands() []cli.Command {
	return []cli.Command{
		serviceCommand(),
	}
}
