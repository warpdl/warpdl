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
	err = m.Flush()
	if err != nil {
		printRuntimeErr(ctx, "flush", "execute", err)
	}
	fmt.Println("Flushed all download history!")
	return nil
}
