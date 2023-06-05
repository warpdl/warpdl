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
	}
	fmt.Printf("%s: fetching details, please wait...", ctx.App.HelpName)
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
	m, err := warplib.InitManager()
	if err != nil {
		printRuntimeErr(ctx, "list", err)
		return nil
	}
	items := m.GetIncompleteItems()
	txt := "Here is a list of incomplete items:"
	for _, item := range items {
		if item.Children {
			continue
		}
		txt += fmt.Sprintf("\n- %s :: %d%%", item.Hash, item.GetPercentage())
	}
	fmt.Println(txt)
	return nil
}
