package cmd

import (
	"errors"
	"fmt"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/pkg/warpcli"
	"github.com/warpdl/warpdl/pkg/warplib"
)

var (
	maxParts   int
	maxConns   int
	forceParts bool
	timeTaken  bool
	timeout    int
	maxRetries int
	retryDelay int

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
		cli.IntFlag{
			Name:        "timeout, t",
			Usage:       "per-request timeout in seconds (0 = no timeout)",
			Value:       DEF_TIMEOUT_SEC,
			EnvVar:      "WARPDL_TIMEOUT",
			Destination: &timeout,
		},
		cli.IntFlag{
			Name:        "max-retries",
			Usage:       "maximum retry attempts for transient errors (0 = unlimited)",
			Value:       DEF_MAX_RETRIES,
			EnvVar:      "WARPDL_MAX_RETRIES",
			Destination: &maxRetries,
		},
		cli.IntFlag{
			Name:        "retry-delay",
			Usage:       "base delay between retries in milliseconds",
			Value:       DEF_RETRY_DELAY,
			EnvVar:      "WARPDL_RETRY_DELAY",
			Destination: &retryDelay,
		},
	}
)

func resume(ctx *cli.Context) (err error) {
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
	var headers warplib.Headers
	if userAgent != "" {
		headers = warplib.Headers{{
			Key: warplib.USER_AGENT_KEY, Value: getUserAgent(userAgent),
		}}
	}
	client, err := warpcli.NewClient()
	if err != nil {
		common.PrintRuntimeErr(ctx, "resume", "new_client", err)
		return
	}
	defer client.Close()
	client.CheckVersionMismatch(currentBuildArgs.Version)
	fmt.Println(">> Initiating a WARP download << ")
	if proxyURL != "" {
		if _, err := warplib.ParseProxyURL(proxyURL); err != nil {
			common.PrintRuntimeErr(ctx, "resume", "invalid_proxy", err)
			return nil
		}
	}
	r, err := client.Resume(hash, &warpcli.ResumeOpts{
		ForceParts:     forceParts,
		MaxConnections: int32(maxConns),
		MaxSegments:    int32(maxParts),
		Headers:        headers,
		Proxy:          proxyURL,
		Timeout:        timeout,
		MaxRetries:     maxRetries,
		RetryDelay:     retryDelay,
	})
	if err != nil {
		common.PrintRuntimeErr(ctx, "resume", "client-resume", err)
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
		r.MaxConnections,
	)
	if r.MaxSegments != 0 {
		txt += fmt.Sprintf("Max Segments\t: %d\n", r.MaxSegments)
	}
	fmt.Println(txt)
	RegisterHandlersWithProgress(client, int64(r.ContentLength), int64(r.Downloaded))
	return client.Listen()
}
