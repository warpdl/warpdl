package ext

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
)

func install(ctx *cli.Context) error {
	if ctx.Args().First() == "help" {
		return cli.ShowCommandHelp(ctx, ctx.Command.Name)
	}
	path := ctx.Args().First()
	if path == "" {
		return common.PrintErrWithCmdHelp(
			ctx,
			errors.New("no path provided"),
		)
	}
	cwd, err := os.Getwd()
	if err != nil {
		common.PrintRuntimeErr(ctx, "ext-install", "getwd", err)
		return nil
	}
	client, err := newClient()
	if err != nil {
		common.PrintRuntimeErr(ctx, "ext-install", "new_client", err)
		return nil
	}
	ext, err := client.AddExtension(filepath.Join(cwd, path))
	if err != nil {
		common.PrintRuntimeErr(ctx, "ext-install", "load-extension", err)
		return nil
	}
	fmt.Printf("Successfully installed extension: %s (%s)\n", ext.Name, ext.Version)
	return nil
}
