package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestOpenBrowserPrintsURLWhenDisabled(t *testing.T) {
	t.Setenv("WARP_NO_BROWSER", "1")
	var buf bytes.Buffer
	if err := openBrowser(&buf, "https://example.com/x"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "https://example.com/x") {
		t.Fatalf("URL not printed to fallback output: %q", buf.String())
	}
}

func TestOpenBrowserNoEmptyURL(t *testing.T) {
	t.Setenv("WARP_NO_BROWSER", "1")
	var buf bytes.Buffer
	_ = openBrowser(&buf, "https://a.b/c")
	// Just smoke — must not panic.
}
