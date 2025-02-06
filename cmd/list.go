package cmd

import (
	"fmt"
	"sort"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/cmd/common"
	"github.com/warpdl/warpdl/pkg/warpcli"
	"github.com/warpdl/warpdl/pkg/warplib"
)

var (
	showHidden    bool
	showCompleted bool
	showPending   bool
	showAll       bool

	lsFlags = []cli.Flag{
		cli.BoolFlag{
			Name:        "show-completed, c",
			Usage:       "use this flag to list completed downloads (default: false)",
			Destination: &showCompleted,
		},
		cli.BoolTFlag{
			Name:        "show-pending, p",
			Usage:       "use this flag to include pending downloads (default: true)",
			Destination: &showPending,
		},
		cli.BoolFlag{
			Name:        "show-all, a",
			Usage:       "use this flag to list all downloads (default: false)",
			Destination: &showAll,
		},
		cli.BoolFlag{
			Name:        "show-hidden, g",
			Usage:       "use this flag to list hidden downloads (default: false)",
			Destination: &showHidden,
		},
	}
)

func list(ctx *cli.Context) error {
	if ctx.Args().First() == "help" {
		return cli.ShowCommandHelp(ctx, ctx.Command.Name)
	}
	client, err := warpcli.NewClient()
	if err != nil {
		common.PrintRuntimeErr(ctx, "list", "new_client", err)
		return nil
	}
	l, err := client.List(&warpcli.ListOpts{
		ShowCompleted: showCompleted || showAll,
		ShowPending:   showPending || showAll,
	})
	if err != nil {
		common.PrintRuntimeErr(ctx, "list", "get_list", err)
		return nil
	}
	fback := func() error {
		fmt.Println("warp: no downloads found")
		return nil
	}
	if len(l.Items) == 0 {
		return fback()
	}
	txt := "Here are your downloads:"
	txt += "\n\n------------------------------------------------------"
	txt += "\n|Num|\t         Name         | Unique Hash | Status |"
	txt += "\n|---|-------------------------|-------------|--------|"
	var i int
	sort.Sort(warplib.ItemSlice(l.Items))
	for _, item := range l.Items {
		if !showHidden && (item.Hidden || item.Children) {
			continue
		}
		i++
		name := item.Name
		n := len(name)
		switch {
		case n > 23:
			name = name[:20] + "..."
		case n < 23:
			name = beaut(name, 23)
		}
		perc := fmt.Sprintf(`%d%%`, item.GetPercentage())
		txt += fmt.Sprintf("\n| %d | %s |   %s  |  %s  |", i, name, item.Hash, beaut(perc, 4))
	}
	if i == 0 {
		return fback()
	}
	txt += "\n------------------------------------------------------"
	fmt.Println(txt)
	return nil
}

func beaut(s string, n int) (b string) {
	n1 := len(s)
	x := n - n1
	x1 := x / 2
	w := string(
		replic(' ', x1),
	)
	b = w
	b += s
	b += w
	if x%2 != 0 {
		b += " "
	}
	return
}

func replic[aT any](v aT, n int) []aT {
	a := make([]aT, n)
	for i := range a {
		a[i] = v
	}
	return a
}
