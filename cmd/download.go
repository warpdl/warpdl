package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/pkg/warpcli"
	"github.com/warpdl/warpdl/pkg/warplib"
)

var (
	dlPath   string
	fileName string

	dlFlags = []cli.Flag{
		cli.StringFlag{
			Name:        "file-name, o",
			Usage:       "explicitly set the name of file (determined automatically if not specified)",
			Destination: &fileName,
		},
		cli.StringFlag{
			Name:        "download-path, l",
			Usage:       "set the path where downloaded file should be saved",
			Value:       ".",
			Destination: &dlPath,
		},
	}
)

func download(ctx *cli.Context) (err error) {
	url := ctx.Args().First()
	if url == "" {
		if ctx.Command.Name == "" {
			return common.Help(ctx)
		}
		return common.PrintErrWithCmdHelp(
			ctx,
			errors.New("no url provided"),
		)
	} else if url == "help" {
		return cli.ShowCommandHelp(ctx, ctx.Command.Name)
	}
	client, err := warpcli.NewClient()
	if err != nil {
		common.PrintRuntimeErr(ctx, "download", "new_client", err)
		return
	}
	fmt.Println(">> Initiating a WARP download << ")
	url = strings.TrimSpace(url)

	var headers warplib.Headers
	if userAgent != "" {
		headers = warplib.Headers{{
			Key: warplib.USER_AGENT_KEY, Value: getUserAgent(userAgent),
		}}
	}
	d, err := client.Download(url, fileName, dlPath, &warpcli.DownloadOpts{
		ForceParts:     forceParts,
		MaxConnections: maxConns,
		MaxSegments:    maxParts,
		Headers:        headers,
	})
	if err != nil {
		common.PrintRuntimeErr(ctx, "info", "download", err)
		return nil
	}
	txt := fmt.Sprintf(`
Download Info
Name`+"\t\t"+`: %s
Size`+"\t\t"+`: %s
Save Location`+"\t"+`: %s/
Max Connections`+"\t"+`: %d
`,
		d.FileName,
		d.ContentLength.String(),
		d.DownloadDirectory,
		d.MaxConnections,
	)
	if d.MaxSegments != 0 {
		txt += fmt.Sprintf("Max Segments\t: %d\n", d.MaxSegments)
	}
	fmt.Println(txt)
	RegisterHandlers(client, int64(d.ContentLength))
	return client.Listen()
}
