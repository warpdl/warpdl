package ext

import (
	"errors"
	"fmt"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/pkg/warpcli"
)

func Info(ctx *cli.Context) (err error) {
	id := ctx.Args().First()
	if id == "" {
		return common.PrintErrWithCmdHelp(
			ctx,
			errors.New("no extension id provided"),
		)
	}
	client, err := warpcli.NewClient()
	if err != nil {
		common.PrintRuntimeErr(ctx, "ext-install", "new_client", err)
		return nil
	}
	extInfo, err := client.GetExtension(id)
	if err != nil {
		common.PrintRuntimeErr(ctx, "ext-install", "get_extension", err)
		return nil
	}
	fmt.Printf(`Extension Info:\n
Name: %s
Version: %s
Description: %s`, extInfo.Name, extInfo.Version, extInfo.Description)
	return nil
}
