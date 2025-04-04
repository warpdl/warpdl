package ext

import (
	"fmt"
	"strconv"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/pkg/warpcli"
)

var (
	showAll bool

	lsFlags = []cli.Flag{
		cli.BoolFlag{
			Name:        "show-all, a",
			Usage:       "use this flag to list all extensions (default: false)",
			Destination: &showAll,
		},
	}
)

func list(ctx *cli.Context) error {
	if ctx.Args().First() == "help" {
		return cli.ShowCommandHelp(ctx, ctx.Command.Name)
	}
	client, err := warpcli.NewClient()
	if err != nil {
		common.PrintRuntimeErr(ctx, "list-ext", "new_client", err)
		return nil
	}
	exts, err := client.ListExtension(showAll)
	if err != nil || exts == nil {
		common.PrintRuntimeErr(ctx, "list-ext", "new_client", err)
		return nil
	}
	txt := "-----------------------------------------------------------------"
	txt += "\n|Num|\t       Name           |     Unique Hash     | Activated |"
	txt += "\n|---|-------------------------|---------------------|-----------|"
	var i int
	for _, ext := range *exts {
		i++
		name := ext.Name
		n := len(name)
		switch {
		case n > 23:
			name = name[:20] + "..."
		case n < 23:
			name = common.Beaut(name, 23)
		}
		txt += fmt.Sprintf("\n| %d | %s |   %s  |   %s   |", i, name, ext.ExtensionId, common.Beaut(strconv.FormatBool(ext.Activated), 5))
	}
	txt += "\n-----------------------------------------------------------------"
	fmt.Println(txt)
	return nil
}
