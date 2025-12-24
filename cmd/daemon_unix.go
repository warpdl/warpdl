//go:build !windows

package cmd

import "github.com/urfave/cli"

// checkWindowsService is a no-op on non-Windows platforms.
// Returns false indicating not running as a service.
func checkWindowsService(ctx *cli.Context) (bool, error) {
	return false, nil
}
