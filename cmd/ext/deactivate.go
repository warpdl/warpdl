package ext

import (
	"errors"
	"fmt"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
)

func deactivate(ctx *cli.Context) error {
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
		return common.PrintRuntimeErr(ctx, "ext-deactivate", "new_client", err)
	}
	defer client.Close()
	id, err = resolveExtensionID(client, id)
	if err != nil {
		return common.PrintRuntimeErr(ctx, "ext-deactivate", "resolve-extension", err)
	}
	ext, err := client.DeactivateExtension(id)
	if err != nil {
		return common.PrintRuntimeErr(ctx, "ext-deactivate", "delete-extension", err)
	}
	fmt.Printf("Successfully deactivated extension: %s\n", ext.Name)
	return nil
}
