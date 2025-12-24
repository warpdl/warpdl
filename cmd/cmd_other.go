//go:build !windows

package cmd

import "github.com/urfave/cli"

// getPlatformCommands returns platform-specific CLI commands.
// On non-Windows platforms, there are no additional commands.
func getPlatformCommands() []cli.Command {
	return nil
}
