package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/warpdl/warpdl/cmd"
)

// these variable are set at build time
var (
	version   string
	commit    string
	date      string
	buildType string = "unclassified"
	osExit           = os.Exit
)

func main() {
	osExit(runMain(os.Args, run))
}

// isNativeMessagingMode checks if warpdl is being invoked as a native messaging host.
// Chrome/Firefox pass the extension origin as the first argument (e.g., chrome-extension://id/).
func isNativeMessagingMode(args []string) bool {
	if len(args) < 2 {
		return false
	}
	arg := args[1]
	return strings.HasPrefix(arg, "chrome-extension://") ||
		strings.HasPrefix(arg, "moz-extension://")
}

func run(args []string) error {
	// Auto-detect native messaging mode when called by browser extensions.
	// Chrome/Firefox pass the extension origin as the first argument.
	if isNativeMessagingMode(args) {
		// Rewrite args to run native-host command
		args = []string{args[0], "native-host", "run"}
	}

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
