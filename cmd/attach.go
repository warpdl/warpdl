package cmd

import (
	"errors"
	"fmt"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/pkg/warpcli"
)

func attach(ctx *cli.Context) (err error) {
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
		printRuntimeErr(ctx, "attach", "new_client", err)
	}
	fmt.Println(">> Initiating a WARP download << ")
	d, err := client.AttachDownload(hash)
	if err != nil {
		printRuntimeErr(ctx, "attach", "client-attach", err)
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
	return client.Listen()
}