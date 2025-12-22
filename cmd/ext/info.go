package ext

import (
	"errors"
	"fmt"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
)

func info(ctx *cli.Context) (err error) {
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
		common.PrintRuntimeErr(ctx, "ext-info", "new_client", err)
		return nil
	}
	extInfo, err := client.GetExtension(id)
	if err != nil {
		common.PrintRuntimeErr(ctx, "ext-info", "get_extension", err)
		return nil
	}
	fmt.Printf(`Extension Info:
Name: %s
Version: %s
Description: %s`, extInfo.Name, extInfo.Version, extInfo.Description)
	return nil
}
