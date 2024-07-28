// debug/extl is a cli tool to debug the extl engine of warpdl.
package main

import (
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
	args := os.Args[1:]
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		println(HELP)
		return
	}

	extEng, err := extl.NewEngine(log.Default(), nil, true)
	if err != nil {
		log.Fatal(err)
	}
	switch args[0] {
	case "extract":
		if len(args) < 2 {
			log.Fatal("extract: missing url")
		}
		url := args[1]
		eUrl, err := extEng.Extract(url)
		if err != nil {
			log.Fatal("extract:", err)
		}
		log.Println("Extracted URL:", eUrl)

	case "load":
		if len(args) < 2 {
			log.Fatal("load: missing extension path")
		}
		extPath := args[1]
		mod, err := extEng.AddModule(extPath)
		if err != nil {
			log.Fatal("load:", err)
		}
		log.Println("Loaded Module:", mod.Name)
	case "list", "unload", "reload":
		log.Println("Not Implemented Yet")
	}
}
