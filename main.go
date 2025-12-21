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
	osExit    = os.Exit
)

func main() {
	osExit(runMain(os.Args, run))
}

func run(args []string) error {
	return cmd.Execute(args, cmd.BuildArgs{
		Version:   version,
		Commit:    commit,
		Date:      date,
		BuildType: buildType,
	})
}

func runMain(args []string, runFunc func([]string) error) int {
	if err := runFunc(args); err != nil {
		fmt.Printf("warpdl: %s\n", err.Error())
		return 1
	}
	return 0
}
