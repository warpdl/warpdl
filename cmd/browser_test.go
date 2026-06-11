package cmd

import (
	"bytes"
	"runtime"
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

// TestOpenBrowserOpenFails covers the browser.OpenURL failure branch
// without launching any process. On Linux the browser package shells out
// to xdg-open / x-www-browser / www-browser, resolved via exec.LookPath
// against PATH. By pointing PATH at an empty directory, none of the
// providers resolve, so OpenURL returns exec.ErrNotFound and openBrowser
// takes the "Could not open browser" fallback — printing the URL and
// returning nil. WARP_NO_BROWSER is explicitly cleared so we reach the
// OpenURL call rather than the env-guard early return.
//
// On non-Linux platforms the dispatch differs (open/rundll32), so we
// restrict this to Linux where the empty-PATH trick is reliable and
// hermetic.
func TestOpenBrowserOpenFails(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("empty-PATH browser-failure trick is Linux-specific")
	}
	t.Setenv("WARP_NO_BROWSER", "")
	t.Setenv("PATH", t.TempDir()) // no browser providers resolvable here

	var buf bytes.Buffer
	if err := openBrowser(&buf, "https://example.com/auth"); err != nil {
		t.Fatalf("openBrowser must swallow open errors, got: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Could not open browser") {
		t.Fatalf("expected fallback message, got: %q", out)
	}
	if !strings.Contains(out, "https://example.com/auth") {
		t.Fatalf("expected URL in fallback output, got: %q", out)
	}
}
