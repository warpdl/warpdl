package main

import (
	"fmt"

	"github.com/urfave/cli"
	"github.com/warpdl/warplib"
)

func flush(ctx *cli.Context) error {
	if !confirm(command("flush"), forceFlush) {
		return nil
	}
	m, err := warplib.InitManager()
	if err != nil {
		printRuntimeErr(ctx, "flush", "init_manager", err)
		return nil
	}
	defer m.Close()
	if hashToFlush != "" && hashToFlush != "all" {
		err = m.FlushOne(hashToFlush)
		if err != nil {
			printRuntimeErr(ctx, "flush", "hash", err)
			return nil
		}
		fmt.Println("Flushed that item!")
		return nil
	}
	err = m.Flush()
	if err != nil {
		printRuntimeErr(ctx, "flush", "execute", err)
	}
	fmt.Println("Flushed all download history!")
	return nil
}
