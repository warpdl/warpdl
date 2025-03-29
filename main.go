package main

import (
	"fmt"
	"os"

	"github.com/warpdl/warpdl/cmd"
)

// these variable are set at build time
var (
	version   string
	commit    string
	date      string
	buildType string = "unclassified"
)

func main() {
	err := cmd.Execute(os.Args, cmd.BuildArgs{
		Version:   version,
		Commit:    commit,
		Date:      date,
		BuildType: buildType,
	})
	if err != nil {
		fmt.Printf("warpdl: %s\n", err.Error())
		os.Exit(1)
	}
}
