package main

import (
	"fmt"
	"os"

	"github.com/warpdl/warpdl/cmd"
)

func main() {
	err := cmd.Execute(os.Args)
	if err != nil {
		fmt.Printf("warpdl: %s\n", err.Error())
		os.Exit(1)
	}
}
