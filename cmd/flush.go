package cmd

import (
	"errors"
	"fmt"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
)

var (
	forceFlush  bool
	hashToFlush string

	flsFlags = []cli.Flag{
		cli.BoolFlag{
			Name:        "force, f",
			Usage:       "use this flag to force flush (default: false)",
			Destination: &forceFlush,
		},
		cli.StringFlag{
			Name:        "item-hash, i",
			Usage:       "use this flag to flush a particular item (default: all)",
			Destination: &hashToFlush,
		},
	}
)

func flush(ctx *cli.Context) error {
	args := ctx.Args()
	if len(args) == 1 {
		hashToFlush = args[0]
	} else if len(args) > 1 {
		return common.PrintErrWithCmdHelp(
			ctx,
			errors.New("invalid amount of arguments"),
		)
	}
	if !confirm(command("flush"), forceFlush) {
		return nil
	}
	client, err := getClient()
	if err != nil {
		common.PrintRuntimeErr(ctx, "flush", "new_client", err)
		return nil
	}
	defer client.Close()
	_, err = client.Flush(hashToFlush)
	if err != nil {
		common.PrintRuntimeErr(ctx, "flush", "flush", err)
		return nil
	}
	if hashToFlush == "" {
		fmt.Println("Flushed all download history!")
	} else {
		fmt.Printf("Flushed %s\n", hashToFlush)
	}
	return nil
}
