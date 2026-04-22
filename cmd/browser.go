package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/pkg/browser"
)

// openBrowser attempts to open the given URL in the user's default
// browser. If the WARP_NO_BROWSER environment variable is set or the
// browser open fails, the URL is printed to `out` instead. Returns nil
// on success (either actually opening or printing a fallback).
func openBrowser(out io.Writer, url string) error {
	if os.Getenv("WARP_NO_BROWSER") != "" {
		fmt.Fprintf(out, "Please open this URL in your browser:\n  %s\n", url)
		return nil
	}
	if err := browser.OpenURL(url); err != nil {
		fmt.Fprintf(out, "Could not open browser (%v).\nPlease open this URL:\n  %s\n", err, url)
		return nil
	}
	fmt.Fprintf(out, "Opening browser...\n  %s\n", url)
	return nil
}
