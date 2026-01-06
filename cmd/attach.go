package cmd

import (
	"errors"
	"fmt"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
)

func attach(ctx *cli.Context) (err error) {
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
	client, err := getClient()
	if err != nil {
		common.PrintRuntimeErr(ctx, "attach", "new_client", err)
		return nil
	}
	defer client.Close()
	client.CheckVersionMismatch(currentBuildArgs.Version)
	fmt.Println(">> Initiating a WARP download << ")
	d, err := client.AttachDownload(hash)
	if err != nil {
		common.PrintRuntimeErr(ctx, "attach", "client-attach", err)
		return nil
	}
	txt := fmt.Sprintf(`
Download Info
Name`+"\t\t"+`: %s
Size`+"\t\t"+`: %s
Save Location`+"\t"+`: %s/
`,
		d.FileName,
		d.ContentLength,
		d.DownloadDirectory,
	)
	fmt.Println(txt)
	RegisterHandlers(client, int64(d.ContentLength))
	return client.Listen()
}
