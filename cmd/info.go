package cmd

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/pkg/warplib"
)

var (
	userAgent string

	infoFlags = []cli.Flag{
		cli.StringFlag{
			Name:        "user-agent",
			Usage:       "HTTP user agent to use for downloding (default: warp)",
			Destination: &userAgent,
		},
	}
)

func info(ctx *cli.Context) error {
	url := ctx.Args().First()
	if url == "" {
		return printErrWithCmdHelp(
			ctx,
			errors.New("no url provided"),
		)
	} else if url == "help" {
		return cli.ShowCommandHelp(ctx, ctx.Command.Name)
	}
	fmt.Printf("%s: fetching details, please wait...\n", ctx.App.HelpName)
	var headers warplib.Headers
	if userAgent != "" {
		headers = warplib.Headers{{
			Key: warplib.USER_AGENT_KEY, Value: getUserAgent(userAgent),
		}}
	}
	d, err := warplib.NewDownloader(
		&http.Client{},
		url,
		&warplib.DownloaderOpts{
			Headers:   headers,
			SkipSetup: true,
		},
	)
	if err != nil {
		printRuntimeErr(ctx, "info", "new_downloader", err)
		return nil
	}
	fName := d.GetFileName()
	if fName == "" {
		fName = "not-defined"
	}
	fmt.Printf(`
File Info
Name`+"\t"+`: %s
Size`+"\t"+`: %s
`, fName, d.GetContentLengthAsString())
	return nil
}
