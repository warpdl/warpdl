package ext

import (
	"errors"
	"fmt"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/pkg/warpcli"
)

func Install(ctx *cli.Context) error {
	path := ctx.Args().First()
	if path == "" {
		return common.PrintErrWithCmdHelp(
			ctx,
			errors.New("no path provided"),
		)
	}
	client, err := warpcli.NewClient()
	if err != nil {
		common.PrintRuntimeErr(ctx, "ext-install", "new_client", err)
		return nil
	}
	ext, err := client.AddExtension(path)
	if err != nil {
		common.PrintRuntimeErr(ctx, "ext-install", "load-extension", err)
		return nil
	}
	fmt.Printf("Successfully installed extension:%s (%s)\n", ext.Name, ext.Version)
	return nil
}
