package cmd

import (
	"errors"
	"fmt"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/pkg/warpcli"
)

var stopFlags = []cli.Flag{
	cli.StringFlag{
		Name:        "daemon-uri",
		Usage:       "daemon URI to connect to (e.g., tcp://localhost:9090, unix:///tmp/warpdl.sock, or /path/to/socket)",
		Destination: &daemonURI,
		EnvVar:      "WARPDL_DAEMON_URI",
	},
}

func stop(ctx *cli.Context) (err error) {
	hash := ctx.Args().First()
	if hash == "" {
		if ctx.Command.Name == "" {
			return common.Help(ctx)
		}
		return common.PrintErrWithCmdHelp(
			ctx,
			errors.New("no hash provided"),
		)
	} else if hash == "help" {
		return cli.ShowCommandHelp(ctx, ctx.Command.Name)
	}
	client, err := warpcli.NewClientWithURI(daemonURI)
	if err != nil {
		common.PrintRuntimeErr(ctx, "stop", "new_client", err)
		return nil
	}
	_, err = client.StopDownload(hash)
	if err != nil {
		common.PrintRuntimeErr(ctx, "stop", "stop-download", err)
		return nil
	}
	fmt.Println("Downloading stopped.")
	return nil
}
