package ext

import (
	"errors"
	"fmt"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
)

func uninstall(ctx *cli.Context) error {
	if ctx.Args().First() == "help" {
		return cli.ShowCommandHelp(ctx, ctx.Command.Name)
	}
	id := ctx.Args().First()
	if id == "" {
		return common.PrintErrWithCmdHelp(
			ctx,
			errors.New("no extension id provided"),
		)
	}
	client, err := newClient()
	if err != nil {
		common.PrintRuntimeErr(ctx, "ext-uninstall", "new_client", err)
		return nil
	}
	ext, err := client.DeleteExtension(id)
	if err != nil {
		common.PrintRuntimeErr(ctx, "ext-uninstall", "delete-extension", err)
		return nil
	}
	fmt.Printf("Successfully uninstalled extension: %s\n", ext.Name)
	return nil
}
