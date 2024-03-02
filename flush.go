package main

import (
	"fmt"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/pkg/warpcli"
)

func flush(ctx *cli.Context) error {
	if !confirm(command("flush"), forceFlush) {
		return nil
	}
	client, err := warpcli.NewClient()
	if err != nil {
		printRuntimeErr(ctx, "flush", "new_client", err)
	}
	_, err = client.Flush(hashToFlush)
	if err != nil {
		printRuntimeErr(ctx, "flush", "flush", err)
		return nil
	}
	if hashToFlush == "" {
		fmt.Println("Flushed all download history!")
	} else {
		fmt.Printf("Flushed %s\n", hashToFlush)
	}
	return nil
}
