package ext

import (
	"errors"
	"fmt"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
)

func activate(ctx *cli.Context) error {
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
		common.PrintRuntimeErr(ctx, "ext-activate", "new_client", err)
		return nil
	}
	ext, err := client.ActivateExtension(id)
	if err != nil {
		common.PrintRuntimeErr(ctx, "ext-activate", "activate-extension", err)
		return nil
	}
	fmt.Printf("Successfully activated extension: %s (%s)\n", ext.Name, ext.Version)
	return nil
}
