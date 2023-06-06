package main

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/urfave/cli"
	"github.com/warpdl/warplib"
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
	d, err := warplib.NewDownloader(
		&http.Client{},
		url,
		nil,
	)
	if err != nil {
		printRuntimeErr(ctx, "info", err)
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

func list(ctx *cli.Context) error {
	if ctx.Args().First() == "help" {
		return cli.ShowCommandHelp(ctx, ctx.Command.Name)
	}
	m, err := warplib.InitManager()
	if err != nil {
		printRuntimeErr(ctx, "list", err)
		return nil
	}
	var items []*warplib.Item
	switch {
	case showAll, showCompleted && showPending:
		items = m.GetItems()
	case showCompleted:
		items = m.GetCompletedItems()
	default:
		items = m.GetIncompleteItems()
	}
	if len(items) == 0 {
		fmt.Println("warp: no downloads found")
		return nil
	}
	txt := "Here are your downloads:"
	txt += "\n\n------------------------------------------------------"
	txt += "\n|Num|\t         Name         | Unique Hash | Status |"
	var i int
	for _, item := range items {
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
