// debug/extl is a cli tool to debug the extl engine of warpdl.
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/warpdl/warpdl/internal/extl"
)

const HELP = `debug/extl is a cli tool to debug the extl engine of warpdl.

Usage:
  debug/extl [command]
  
Commands:
  help    Show this help message and exit.
  extract Extract a url using the loaded extensions.
  list    List all the loaded extensions.
  load    Load an extension.
  unload  Unload an extension.
  reload  Reload an extension.
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		println(HELP)
		return nil
	}

	extEng, err := extl.NewEngine(log.Default(), nil, true)
	if err != nil {
		return err
	}
	switch args[0] {
	case "extract":
		if len(args) < 2 {
			return fmt.Errorf("extract: missing url")
		}
		url := args[1]
		eUrl, err := extEng.Extract(url)
		if err != nil {
			return fmt.Errorf("extract: %w", err)
		}
		log.Println("Extracted URL:", eUrl)
	case "load":
		if len(args) < 2 {
			return fmt.Errorf("load: missing extension path")
		}
		extPath := args[1]
		mod, err := extEng.AddModule(extPath)
		if err != nil {
			return fmt.Errorf("load: %w", err)
		}
		log.Println("Loaded Module:", mod.Name)
	case "list", "unload", "reload":
		log.Println("Not Implemented Yet")
	}
	return nil
}
