package cmd

import (
	"errors"
	"fmt"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/pkg/warpcli"
	"github.com/warpdl/warpdl/pkg/warplib"
)

var (
	maxParts   int
	maxConns   int
	forceParts bool
	timeTaken  bool

	rsFlags = []cli.Flag{
		cli.IntFlag{
			Name:        "max-parts, s",
			Usage:       "to specify the number of maximum file segments",
			EnvVar:      "WARP_MAX_PARTS",
			Destination: &maxParts,
			Value:       DEF_MAX_PARTS,
		},
		cli.IntFlag{
			Name:        "max-connection, x",
			Usage:       "specify the number of maximum parallel connection",
			EnvVar:      "WARP_MAX_CONN",
			Destination: &maxConns,
			Value:       DEF_MAX_CONNS,
		},
		cli.BoolTFlag{
			Name:        "force-parts, f",
			Usage:       "forceful file segmentation (default: true)",
			EnvVar:      "WARP_FORCE_SEGMENTS",
			Destination: &forceParts,
		},
		cli.BoolFlag{
			Name:        "time-taken, e",
			Destination: &timeTaken,
			Hidden:      true,
		},
	}
)

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
