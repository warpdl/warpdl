package main

import (
	"errors"
	"fmt"

	"github.com/urfave/cli"
	"github.com/warpdl/warpdl/pkg/warplib"
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
	var headers warplib.Headers
	if userAgent != "" {
		headers = warplib.Headers{{
			Key: warplib.USER_AGENT_KEY, Value: getUserAgent(userAgent),
		}}
	}
	d, err := warplib.NewDownloader(
		getHTTPClient(),
		url,
		&warplib.DownloaderOpts{
			Headers:   headers,
			SkipSetup: true,
		},
	)
	if err != nil {
		printRuntimeErr(ctx, "info", "new_downloader", err)
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
		printRuntimeErr(ctx, "list", "init_manager", err)
		return nil
	}
	defer m.Close()
	var items []*warplib.Item
	switch {
	case showAll, showCompleted && showPending:
		items = m.GetItems()
	case showCompleted:
		items = m.GetCompletedItems()
	default:
		items = m.GetIncompleteItems()
	}
	fback := func() error {
		fmt.Println("warp: no downloads found")
		return nil
	}
	if len(items) == 0 {
		return fback()
	}
	txt := "Here are your downloads:"
	txt += "\n\n------------------------------------------------------"
	txt += "\n|Num|\t         Name         | Unique Hash | Status |"
	txt += "\n|---|-------------------------|-------------|--------|"
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
