package cmd

import (
	"errors"
	"fmt"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/pkg/warpcli"
)

func stop(ctx *cli.Context) (err error) {
	hash := ctx.Args().First()
	if hash == "" {
		if ctx.Command.Name == "" {
			return help(ctx)
		}
		return printErrWithCmdHelp(
			ctx,
			errors.New("no hash provided"),
		)
	} else if hash == "help" {
		return cli.ShowCommandHelp(ctx, ctx.Command.Name)
	}
	client, err := warpcli.NewClient()
	if err != nil {
		printRuntimeErr(ctx, "stop", "new_client", err)
		return nil
	}
	_, err = client.StopDownload(hash)
	if err != nil {
		printRuntimeErr(ctx, "stop", "stop-download", err)
		return nil
	}
	fmt.Println("Downloading stopped.")
	return nil
}
