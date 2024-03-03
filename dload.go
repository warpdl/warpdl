package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/pkg/warpcli"
	"github.com/warpdl/warpdl/pkg/warplib"
)

func download(ctx *cli.Context) (err error) {
	url := ctx.Args().First()
	if url == "" {
		if ctx.Command.Name == "" {
			return help(ctx)
		}
		return printErrWithCmdHelp(
			ctx,
			errors.New("no url provided"),
		)
	} else if url == "help" {
		return cli.ShowCommandHelp(ctx, ctx.Command.Name)
	}
	fmt.Println(">> Initiating a WARP download << ")
	url = strings.TrimSpace(url)

	var headers warplib.Headers
	if userAgent != "" {
		headers = warplib.Headers{{
			Key: warplib.USER_AGENT_KEY, Value: getUserAgent(userAgent),
		}}
	}
	if fileName == "" {
		printRuntimeErr(ctx, "info", "get_file_name", errors.New("file name cannot be empty"))
		return
	}
	client, err := warpcli.NewClient()
	if err != nil {
		printRuntimeErr(ctx, "info", "new_client", err)
		return
	}
	d, err := client.Download(url, fileName, dlPath, &warpcli.DownloadOpts{
		ForceParts:     forceParts,
		MaxConnections: maxConns,
		MaxSegments:    maxParts,
		Headers:        headers,
	})
	if err != nil {
		printRuntimeErr(ctx, "info", "create_downloader", err)
		return nil
	}
	txt := fmt.Sprintf(`
Download Info
Name`+"\t\t"+`: %s
Size`+"\t\t"+`: %s
Save Location`+"\t"+`: %s/
Max Connections`+"\t"+`: %d
`,
		fileName,
		d.ContentLength.String(),
		d.DownloadDirectory,
		maxConns,
	)
	if maxParts != 0 {
		txt += fmt.Sprintf("Max Segments\t: %d\n", maxParts)
	}
	fmt.Println(txt)
	return client.Listen()
}

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

func resume(ctx *cli.Context) (err error) {
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
	var headers warplib.Headers
	if userAgent != "" {
		headers = warplib.Headers{{
			Key: warplib.USER_AGENT_KEY, Value: getUserAgent(userAgent),
		}}
	}
	client, err := warpcli.NewClient()
	if err != nil {
		printRuntimeErr(ctx, "resume", "new_client", err)
		return
	}
	fmt.Println(">> Initiating a WARP download << ")
	r, err := client.Resume(hash, &warpcli.ResumeOpts{
		ForceParts:     forceParts,
		MaxConnections: maxConns,
		MaxSegments:    maxParts,
		Headers:        headers,
	})
	if err != nil {
		printRuntimeErr(ctx, "resume", "client-resume", err)
		return nil
	}

	txt := fmt.Sprintf(`
Download Info
Name`+"\t\t"+`: %s
Size`+"\t\t"+`: %s
Save Location`+"\t"+`: %s/
Max Connections`+"\t"+`: %d
`,
		r.FileName,
		r.ContentLength,
		func() string {
			loc := r.AbsoluteLocation
			if loc != "" {
				return loc
			}
			return r.DownloadDirectory
		}(),
		maxConns,
	)
	if maxParts != 0 {
		txt += fmt.Sprintf("Max Segments\t: %d\n", maxParts)
	}
	fmt.Println(txt)
	return client.Listen()
}
