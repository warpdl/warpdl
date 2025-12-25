//go:build !windows

package cmd

import "github.com/urfave/cli"

// getDaemonAction returns the platform-specific daemon action.
// On Unix platforms (Linux, macOS), this is the standard daemon function.
func getDaemonAction() cli.ActionFunc {
	return daemon
}
